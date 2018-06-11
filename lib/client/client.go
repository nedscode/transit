package transit

import (
	"context"
	"errors"
	"time"

	"github.com/norganna/logeric"
	"google.golang.org/grpc"

	"github.com/nedscode/transit/lib/connect"
	pb "github.com/nedscode/transit/proto"
)

const (
	defaultPoolSize = 1000
	maxPoolSize     = 1000000
)

// Client is a connection to the Transit server cluster.
type Client struct {
	ctx    context.Context
	cancel context.CancelFunc
	conn   *grpc.ClientConn
	client pb.TransitClient

	logger *logeric.Log

	poolSem   chan bool
	poolItems chan *Pub

	params *connect.Parameters
}

var (
	// ErrNotToken is an error returned when a token is expected, but not received.
	ErrNotToken = errors.New("supplied token is not a valid token")

	// ErrNotMarshalable is an error returned when sending a message that can't be marshalled.
	ErrNotMarshalable = errors.New("message is not marshalable and can't be sent")

	// ErrNotUnmarshalable is an error returned when receiving a message that can't be unmarshalled.
	ErrNotUnmarshalable = errors.New("message is not unmarshalable and can't be decoded")

	// ErrInvalidURI is an error returned when a cluster URI is provided that is not parseable.
	ErrInvalidURI = connect.ErrInvalidURI

	// ErrInvalidToken is an error returned when a URI is provided that doesn't have a valid token.
	ErrInvalidToken = connect.ErrInvalidToken

	// ErrInvalidTLSMode is an error returned when an unknown TLS mode parameter is provided in the uri.
	ErrInvalidTLSMode = connect.ErrInvalidTLSMode

	// ErrInvalidKeyFile is an error returned when a key file is not readable.
	ErrInvalidKeyFile = connect.ErrInvalidKeyFile

	// ErrInvalidCertFile is an error returned when a cert file is not readable.
	ErrInvalidCertFile = connect.ErrInvalidCertFile

	// ErrInvalidCert is an error returned when a key+cert pair is not able to be processed into a connection cert.
	ErrInvalidCert = connect.ErrInvalidCert

	// ErrInvalidDuration is an error returned when a timeout value is not parseable.
	ErrInvalidDuration = connect.ErrInvalidDuration

	// ErrInvalidRetries is an error returned when a retry count is not parseable.
	ErrInvalidRetries = connect.ErrInvalidRetries

	// ErrNoPeers is an error returned when there are no peers supplied to connect to.
	ErrNoPeers = connect.ErrNoPeers

	// ErrNoPeer is an error returned when there is no reachable peer found.
	ErrNoPeer = connect.ErrNoPeer

	// ErrNoLeader is an error returned when there is no reachable leader found.
	ErrNoLeader = connect.ErrNoLeader
)

// Connect establishes the connection to the master server.
func Connect(ctx context.Context, opts ...ConnectOption) (*Client, error) {
	ctx2, cancel := context.WithCancel(ctx)

	client := &Client{
		ctx:    ctx2,
		cancel: cancel,

		params: &connect.Parameters{
			TLSMode:        1,
			LeaderOnly:     true,
			ConnectTimeout: 500 * time.Millisecond,
			ReadTimeout:    500 * time.Millisecond,
			RetryCount:     3,
			PoolSize:       defaultPoolSize,
		},

		poolItems: make(chan *Pub, 10000),
	}

	var err error

	for _, opt := range opts {
		err = opt(client)
		if err != nil {
			return nil, err
		}
	}

	if len(client.params.Peers) == 0 {
		return nil, ErrNoPeers
	}

	if client.logger == nil {
		client.logger, err = logeric.New(nil)
		if err != nil {
			return nil, err
		}
	}

	err = client.peerConnect()
	if err != nil {
		return nil, err
	}

	client.startPool()

	return client, nil
}

// Close closes this client connection.
func (c *Client) Close() {
	c.conn.Close()
	c.cancel()
}

// Publish causes an entry to be published to Transit.
// Provide the desired WriteConcern and a timeout in milliseconds.
// This function is asynchronous and will return immediately before sending in the background.
// If you wish to wait until the write concern has been reached, call `.Done(true)` on the result of this `Publish`.

func (c *Client) startPool() {
	if c.params.PoolSize == 0 {
		// If there's no PoolSize then we just send direct.
		return
	}

	sem := make(chan bool, c.params.PoolSize)
	c.poolSem = sem
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				// Shutting down
				return
			case p := <-c.poolItems:
				// Gain a semaphore
				sem <- true
				go func() {
					// Publish the item
					c.publish(p)

					// Release semaphore
					<-sem
				}()
			}
		}
	}()
}

func (c *Client) enqueue(p *Publication) *Pub {
	pub := &Pub{
		p:    p,
		done: make(chan struct{}),
	}

	if c.params.PoolSize == 0 {
		c.publish(pub)
	} else {
		c.poolItems <- pub
	}
	return pub
}

func (c *Client) publish(p *Pub) {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(p.p.Timeout)*time.Millisecond+5*time.Second)
	defer cancel()

	pub, err := c.client.Publish(ctx, p.p)
	if err == nil {
		p.id = pub.ID
		p.concern = pub.Concern
	}

	p.err = err
	close(p.done)
}

func (c *Client) ack(s *Sub, ack, close bool) bool {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	a, err := c.client.Ack(ctx, &pb.Acknowledgement{
		Sub:   s,
		Ack:   ack,
		Close: close,
	})
	return err == nil && a != nil && a.Success
}

func (c *Client) connectContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, c.params.ConnectTimeout)
}

func (c *Client) readContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, c.params.ReadTimeout)
}
