package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nedscode/transit/lib/inboxes"
	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/proto"
)

// Backend is a TransitServer handler
type Backend struct {
	mu     *sync.RWMutex
	logger logrus.FieldLogger
	store  *raft.Store
	inbox  *inboxes.Inboxes
	otp    *otpStore
	ctx    context.Context
}

var _ transit.TransitServer = (*Backend)(nil)

var (
	unauthenticatedError = status.Error(codes.Unauthenticated, "Token not supplied for authenticated call")
	permissionError      = status.Error(codes.PermissionDenied, "Did not supply a token with sufficient access")
	invalidTokenError    = status.Error(codes.PermissionDenied, "Invalid or expired token provided")
	notLeaderError       = status.Error(codes.Canceled, "This server is not the cluster leader")
)

// New creates a new Backend for use as a transit handler
func New(ctx context.Context, logger logrus.FieldLogger, store *raft.Store, mode inboxes.SyncMode) *Backend {
	logger = logger.WithField("prefix", "handler")

	return &Backend{
		mu:     &sync.RWMutex{},
		logger: logger,
		store:  store,
		inbox:  inboxes.New(ctx, logger, nil, mode),
		otp:    &otpStore{times: map[string]*otp{}},
		ctx:    ctx,
	}
}

// Ping tests server is alive.
func (b *Backend) Ping(ctx context.Context, ping *transit.Pong) (*transit.Pong, error) {
	logger := b.logger.WithField("rpc", "ping")

	tokenName, _, err := b.requireRoles(ctx)
	if err != nil {
		return nil, err
	}

	res := &transit.Pong{
		ID:      ping.ID,
		Leader:  b.store.Leader(),
		Leading: b.store.Leading(),
	}

	logger = logger.WithField("user", tokenName)
	logger = logger.WithField("req", ping)

	logger.WithField("res", res).Debug("Sending pong")
	return res, nil
}

// Publish takes a message entry and returns the published message id.
func (b *Backend) Publish(ctx context.Context, p *transit.Publication) (*transit.Published, error) {
	if !b.store.Leading() {
		return nil, notLeaderError
	}
	wrap := inboxes.Wrap(p.Entry)

	logger := b.logger.WithField("rpc", "publish")

	tokenName, _, err := b.requireRoles(ctx, RoleOwner, RolePublisher)
	if err != nil {
		return nil, err
	}
	logger = logger.WithField("user", tokenName)
	logger = logger.WithField("req", p)

	var done chan *struct{}

	if p.Concern > transit.Concern_None {
		done = make(chan *struct{})
		monitor := make(chan transit.Concern, 10)
		wrap.Updated = monitor

		go func() {
			var waitCtx context.Context
			var cancel context.CancelFunc

			if p.Timeout > 0 {
				waitCtx, cancel = context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Millisecond)
			} else {
				waitCtx, cancel = context.WithCancel(ctx)
			}
			defer cancel()

		loop:
			for {
				select {
				case <-waitCtx.Done():
					break loop
				case concern := <-monitor:
					if concern >= p.Concern {
						break loop
					}
				}
			}

			done <- nil
		}()
	}

	b.inbox.Add(wrap)

	if done != nil {
		<-done
	}

	res := &transit.Published{
		ID:      wrap.ID,
		Concern: wrap.Concern,
	}
	logger.WithField("res", res).Debug("Published entry")
	return res, nil
}

func drain(changed <-chan bool) {
	select {
	case <-changed:
	default:
	}
}

// Subscribe takes topic and group details and returns a subscription stream.
func (b *Backend) Subscribe(d *transit.Subscription, s transit.Transit_SubscribeServer) error {
	if !b.store.Leading() {
		return notLeaderError
	}

	ctx := s.Context()

	logger := b.logger.WithField("rpc", "subscribe")

	tokenName, _, err := b.requireRoles(ctx, RoleOwner, RolePublisher, RoleSubscriber)
	if err != nil {
		return err
	}
	logger = logger.WithField("user", tokenName)

	logger.Debug("New subscriber")

	subscriber := newSubscriber(d.Allotments)
	subscriber.delay = d.Delay
	subscriber.maxAge = d.MaxAge

	box := b.inbox.Inbox(d.Prefix, d.Group, nil)
	logger = logger.WithFields(logrus.Fields{
		"prefix": d.Prefix,
		"group":  d.Group,
	})

	// Shift the bits right to normalise the group distribution field
	groupDistribution := d.Distribution >> 6 & 0x3f
	if strategy, changed := box.Strategy(&inboxes.Strategy{
		Delivery:     d.Delivery,
		Distribution: groupDistribution,
	}); changed {
		logger.WithField("strategy", strategy.String()).Info("Inbox strategy changed by subscriber")
	}

	running := true
	for running {
		var sub *transit.Sub
		wrap, changed := box.Next(ctx, subscriber)

		if wrap != nil {
			drain(changed)

			logger.WithField("entry", wrap.ID).Debug("Sending entry to subscriber")
			sub = &transit.Sub{
				Prefix: d.Prefix,
				Group:  d.Group,
				ID:     wrap.ID,
			}
			s.Send(&transit.Notification{
				Sub:   sub,
				Entry: wrap.Entry,
			})
		}

		// Wait for change notification before sending a new entry
		select {
		case <-ctx.Done():
			if sub != nil {
				box.Ack(&transit.Acknowledgement{
					Sub:   sub,
					Ack:   false,
					Close: true,
				})
				drain(changed)
			}

			running = false
		case running = <-changed:
		}
	}

	return nil
}

// Ack acknowledges the successful receipt and processing of a message id.
// Acknowledging a message allows you to receive a new message.
func (b *Backend) Ack(ctx context.Context, p *transit.Acknowledgement) (*transit.Acked, error) {
	if !b.store.Leading() {
		return nil, notLeaderError
	}

	logger := b.logger.WithField("rpc", "ack")

	tokenName, _, err := b.requireRoles(ctx, RoleOwner, RolePublisher, RoleSubscriber)
	if err != nil {
		return nil, err
	}
	logger = logger.WithField("user", tokenName)

	logger = logger.WithField("req", p)

	res := &transit.Acked{
		Success: true,
	}

	logger.WithField("res", res).Debug("Acking entry")
	return res, nil
}

// ClusterApply is for applying a set of transformation commands to the cluster's state.
func (b *Backend) ClusterApply(ctx context.Context, a *transit.ApplyCommands) (*transit.Success, error) {
	logger := b.logger.WithField("rpc", "cluster-apply")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	if !b.store.Leading() {
		logger.Info("Requires leader")
		return nil, notLeaderError
	}

	var errs []string
	for _, c := range a.Commands {
		err := b.store.Operate(&raft.Command{
			Operation: c.Operation,
			Key:       c.Key,
			Value:     c.Value,
			Compare:   c.Compare,
			Versus:    c.Versus,
		})
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	res := &transit.Success{}
	if len(errs) > 0 {
		res.Error = fmt.Sprintf("Errors applying commands: %s", strings.Join(errs, ", "))
	} else {
		res.Succeed = true
	}

	logger.WithField("res", res).Debug("Applying commands")
	return res, nil
}

// ClusterGetKeys returns the state values for a given set of cluster keys.
func (b *Backend) ClusterGetKeys(ctx context.Context, s *transit.Strings) (*transit.StringMap, error) {
	logger := b.logger.WithField("rpc", "cluster-get-keys")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	m := map[string]string{}
	for _, k := range s.Values {
		v := b.store.Get(k)
		m[k] = v
	}
	res := &transit.StringMap{
		Values: m,
	}

	logger.WithField("res", res).Debug("Getting keys")
	return res, nil
}

// ClusterList returns a list of keys and values, with the provided prefix from the cluster.
func (b *Backend) ClusterList(ctx context.Context, s *transit.String) (*transit.StringMap, error) {
	logger := b.logger.WithField("rpc", "cluster-list")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	list := b.store.List(s.Value)
	res := &transit.StringMap{
		Values: list,
	}

	logger.WithField("res", res).Debug("Listing keys")
	return res, nil
}

// ClusterJoin makes the server perform a join with the given server.
func (b *Backend) ClusterJoin(ctx context.Context, s *transit.Server) (*transit.Success, error) {
	logger := b.logger.WithField("rpc", "cluster-join")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	if !b.store.Leading() {
		logger.Info("Requires leader")
		return nil, notLeaderError
	}

	err = b.store.Join(s.ID, s.Address)
	res := &transit.Success{}
	if err != nil {
		res.Error = err.Error()
	} else {
		res.Succeed = true
	}

	logger.WithField("res", res).Debug("Joining to cluster")
	return res, nil
}

// ClusterLeader returns the address of the current cluster leader.
func (b *Backend) ClusterLeader(ctx context.Context, v *transit.Void) (*transit.String, error) {
	logger := b.logger.WithField("rpc", "cluster-leader")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	res := &transit.String{
		Value: b.store.Leader(),
	}

	logger.WithField("res", res).Debug("Returning leader")
	return res, nil
}
