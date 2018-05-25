package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/proto"
)

// Backend is a TransitServer handler
type Backend struct {
	mu     *sync.RWMutex
	logger logrus.FieldLogger
	store  *raft.Store
	otp    *otpStore
}

var _ transit.TransitServer = (*Backend)(nil)

var (
	unauthenticatedError = status.Error(codes.Unauthenticated, "Token not supplied for authenticated call")
	permissionError      = status.Error(codes.PermissionDenied, "Did not supply a token with sufficient access")
	invalidTokenError    = status.Error(codes.PermissionDenied, "Invalid or expired token provided")
	notLeaderError       = status.Error(codes.Canceled, "This server is not the cluster leader")
)

// New creates a new Backend for use as a transit handler
func New(logger logrus.FieldLogger, store *raft.Store) *Backend {
	return &Backend{
		mu:     &sync.RWMutex{},
		logger: logger,
		store:  store,
		otp:    &otpStore{times: map[string]*otp{}},
	}
}

// Ping tests server is alive.
func (b *Backend) Ping(ctx context.Context, ping *transit.Pong) (*transit.Pong, error) {
	logger := b.logger.WithField("rpc", "ping")

	tokenName, err := b.requireRoles(ctx)
	if err != nil {
		return nil, err
	}

	ret := &transit.Pong{
		ID:      ping.ID,
		Leader:  b.store.Leader(),
		Leading: b.store.Leading(),
	}

	logger = logger.WithField("tokenName", tokenName)
	logger = logger.WithField("ping", ping)

	logger.WithField("ret", ret).Info("Sending pong")
	return ret, nil
}

// Publish takes a message entry and returns the published message id.
func (b *Backend) Publish(ctx context.Context, e *transit.Entry) (*transit.Publication, error) {
	if !b.store.Leading() {
		return nil, notLeaderError
	}

	logger := b.logger.WithField("rpc", "publish")

	tokenName, err := b.requireRoles(ctx, "owner", "publisher")
	if err != nil {
		return nil, err
	}
	logger = logger.WithField("tokenName", tokenName)
	logger = logger.WithField("entry", e)

	ret := &transit.Publication{
		ID: 1, // TODO actual pub ID
	}
	logger.WithField("ret", ret).Info("Publishing entry")
	return ret, nil
}

// Subscribe takes topic and group details and returns a subscription stream.
func (b *Backend) Subscribe(d *transit.Subscription, s transit.Transit_SubscribeServer) error {
	if !b.store.Leading() {
		return notLeaderError
	}

	logger := b.logger.WithField("rpc", "subscribe")

	tokenName, err := b.requireRoles(s.Context(), "owner", "publisher", "subscriber")
	if err != nil {
		return err
	}
	logger = logger.WithField("tokenName", tokenName)

	logger.Info("New subscriber")

	return nil
}

// Ack acknowledges the successful receipt and processing of a message id.
// Acknowledging a message allows you to receive a new message.
func (b *Backend) Ack(ctx context.Context, p *transit.Sub) (*transit.Acked, error) {
	if !b.store.Leading() {
		return nil, notLeaderError
	}

	logger := b.logger.WithField("rpc", "ack")

	tokenName, err := b.requireRoles(ctx, "owner", "publisher", "subscriber")
	if err != nil {
		return nil, err
	}
	logger = logger.WithField("tokenName", tokenName)

	logger = logger.WithField("sub", p)

	ret := &transit.Acked{
		Success: true,
	}

	logger.WithField("ret", ret).Info("Acking entry")
	return ret, nil
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
		errs = append(errs, err.Error())
	}

	ret := &transit.Success{}
	if len(errs) > 0 {
		ret.Error = fmt.Sprintf("Errors applying commands: %s", strings.Join(errs, ", "))
	} else {
		ret.Succeed = true
	}

	logger.WithField("ret", ret).Info("Applying commands")
	return ret, nil
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
	ret := &transit.StringMap{
		Values: m,
	}

	logger.WithField("ret", ret).Info("Getting keys")
	return ret, nil
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
	ret := &transit.StringMap{
		Values: list,
	}

	logger.WithField("ret", ret).Info("Listing keys")
	return ret, nil
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
	ret := &transit.Success{}
	if err != nil {
		ret.Error = err.Error()
	} else {
		ret.Succeed = true
	}

	logger.WithField("ret", ret).Info("Joining to cluster")
	return ret, nil
}

// ClusterLeader returns the address of the current cluster leader.
func (b *Backend) ClusterLeader(ctx context.Context, v *transit.Void) (*transit.String, error) {
	logger := b.logger.WithField("rpc", "cluster-leader")

	err := b.requireCluster(ctx)
	if err != nil {
		logger.WithError(err).Info("Not permitted")
		return nil, err
	}

	ret := &transit.String{
		Value: b.store.Leader(),
	}

	logger.WithField("ret", ret).Info("Returning leader")
	return ret, nil
}
