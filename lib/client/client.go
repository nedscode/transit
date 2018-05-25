package transit

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"google.golang.org/grpc"

	"github.com/nedscode/transit/lib/connect"
	pb "github.com/nedscode/transit/proto"
)

// Client is a connection to the Transit server cluster.
type Client struct {
	ctx    context.Context
	cancel context.CancelFunc
	conn   *grpc.ClientConn
	client pb.TransitClient

	connectTimeout time.Duration
	readTimeout    time.Duration

	tlsMode     int
	cert        tls.Certificate
	masterToken string
	peers       []string
}

var (
	// ErrNotMasterToken is an error returned when a master token is expected, but not received.
	ErrNotMasterToken = errors.New("supplied token is not a master token")

	// ErrNotMarshalable is an error returned when sending a message that can't be marshalled.
	ErrNotMarshalable = errors.New("message is not marshalable and can't be sent")

	// ErrNotUnmarshalable is an error returned when receiving a message that can't be unmarshalled.
	ErrNotUnmarshalable = errors.New("message is not unmarshalable and can't be decoded")

	// ErrNoPeers is an error returned when there are no peers supplied to connect to.
	ErrNoPeers = connect.ErrNoPeers

	// ErrNoPeer is an error returned when there is no peer found.
	ErrNoPeer = connect.ErrNoPeer

	// ErrNoLeader is an error returned when there is no leader found.
	ErrNoLeader = connect.ErrNoLeader
)

// Connect establishes the connection to the master server.
func Connect(ctx context.Context, opts ...ConnectOption) (*Client, error) {
	ctx2, cancel := context.WithCancel(ctx)

	client := &Client{
		ctx:    ctx2,
		cancel: cancel,

		connectTimeout: 500 * time.Millisecond,
		readTimeout:    500 * time.Millisecond,

		tlsMode: 1,
	}

	var err error

	for _, opt := range opts {
		err = opt(client)
		if err != nil {
			return nil, err
		}
	}

	if len(client.peers) == 0 {
		return nil, ErrNoPeers
	}

	err = client.peerConnect(true, 3)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Close closes this client connection.
func (c *Client) Close() {
	c.conn.Close()
	c.cancel()
}

// Publish causes an entry to be published to Transit.
func (c *Client) Publish(e *Entry) uint64 {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	p, err := c.client.Publish(ctx, e)
	if err != nil {
		return 0
	}

	return p.ID
}

func (c *Client) ack(s *Sub) bool {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	a, err := c.client.Ack(ctx, s)
	return err == nil && a != nil && a.Success
}

func (c *Client) connectContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, c.connectTimeout)
}

func (c *Client) readContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, c.readTimeout)
}
