package transit

import (
	"crypto/tls"
	"time"

	"github.com/nedscode/transit/lib/connect"
)

// ConnectOption is a function that can modify connection options within the client.
type ConnectOption func(c *Client) error

// Peers is a connection option that takes a list of peers to connect to.
func Peers(peers ...string) ConnectOption {
	return func(c *Client) error {
		c.peers = peers
		return nil
	}
}

// MasterToken is a connection option that takes a master token to be used for the connection.
func MasterToken(token string) ConnectOption {
	return func(c *Client) error {
		if len(token) != 33 || token[12] != '-' {
			return ErrNotMasterToken
		}
		c.masterToken = token
		return nil
	}
}

// ClientTLS is a connection option that takes a set of certificates for client authentication over TLS.
// Provide the raw PEM encoded certificate bytes (typically loaded directly from the .pem files).
func ClientTLS(cert, key []byte) ConnectOption {
	return func(c *Client) (err error) {
		c.tlsMode = 2
		c.cert, err = tls.X509KeyPair(cert, key)
		return
	}
}

// AnonTLS is a connection option that causes anonymous client connection over TLS (the default).
func AnonTLS() ConnectOption {
	return func(c *Client) error {
		c.tlsMode = 1
		return nil
	}
}

// NoTLS is a connection option that causes client connection in the clear, unencrypted.
func NoTLS() ConnectOption {
	return func(c *Client) error {
		c.tlsMode = 0
		return nil
	}
}

// ConnectTimeout is a connection option that sets the limit on the connection timeout.
func ConnectTimeout(duration time.Duration) ConnectOption {
	return func(c *Client) error {
		c.connectTimeout = duration
		return nil
	}
}

// ReadTimeout is a connection option that sets the limit on the read timeout.
func ReadTimeout(duration time.Duration) ConnectOption {
	return func(c *Client) error {
		c.readTimeout = duration
		return nil
	}
}

func (c *Client) peerConnect(wantLeader bool, retries int) (err error) {
	c.conn, c.client, err = connect.Establish(
		c.ctx, c.connectTimeout, retries, c.tlsMode, c.masterToken, c.cert, wantLeader, c.peers,
	)
	return
}
