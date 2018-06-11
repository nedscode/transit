package transit

import (
	"crypto/tls"
	"time"

	"github.com/norganna/logeric"

	"github.com/nedscode/transit/lib/connect"
)

// ConnectOption is a function that can modify connection options within the client.
type ConnectOption func(c *Client) error

// Peers is a connection option that takes a list of peers to connect to.
func Peers(peers ...string) ConnectOption {
	return func(c *Client) error {
		c.params.Peers = peers
		return nil
	}
}

// Token is a connection option that takes a token to be used for the connection.
func Token(token string) ConnectOption {
	return func(c *Client) error {
		if len(token) == 33 && token[12] == '-' {
			c.params.TokenType = "master"
		} else if len(token) == 48 {
			c.params.TokenType = "cluster"
		} else {
			return ErrNotToken
		}
		c.params.Token = token
		return nil
	}
}

// ClientTLS is a connection option that takes a set of certificates for client authentication over TLS.
// Provide the raw PEM encoded certificate bytes (typically loaded directly from the .pem files).
func ClientTLS(cert, key []byte) ConnectOption {
	return func(c *Client) (err error) {
		c.params.TLSMode = 2
		c.params.Certificate, err = tls.X509KeyPair(cert, key)
		return
	}
}

// AnonTLS is a connection option that causes anonymous client connection over TLS (the default).
func AnonTLS() ConnectOption {
	return func(c *Client) error {
		c.params.TLSMode = 1
		return nil
	}
}

// NoTLS is a connection option that causes client connection in the clear, unencrypted.
func NoTLS() ConnectOption {
	return func(c *Client) error {
		c.params.TLSMode = 0
		return nil
	}
}

// ConnectTimeout is a connection option that sets the limit on the connection timeout.
func ConnectTimeout(duration time.Duration) ConnectOption {
	return func(c *Client) error {
		c.params.ConnectTimeout = duration
		return nil
	}
}

// ReadTimeout is a connection option that sets the limit on the read timeout.
func ReadTimeout(duration time.Duration) ConnectOption {
	return func(c *Client) error {
		c.params.ReadTimeout = duration
		return nil
	}
}

// RequireClusterToken allows the requirement for a cluster token to be provided instead of a master token.
func RequireClusterToken() ConnectOption {
	return func(c *Client) error {
		c.params.RequireType = "cluster"
		return nil
	}
}

// URI allows the parsing of a connection URI to set options.
func URI(uri string) ConnectOption {
	return func(c *Client) error {
		_, err := connect.ParseURI(uri, c.params)
		return err
	}
}

// Logger allows specification of a generic logger.
func Logger(logger interface{}) ConnectOption {
	return func(c *Client) (err error) {
		c.logger, err = logeric.New(logger)
		return err
	}
}

// PoolSize allows setting the background publisher pool size.
// If you use lots of `Processed` write concerns, or are really cranking out those publications, you'll probably want
// to bump this up a notch, otherwise the default value should be more than enough.
func PoolSize(size uint) ConnectOption {
	return func(c *Client) error {
		if size > maxPoolSize {
			size = maxPoolSize
		}
		c.params.PoolSize = int(size)
		return nil
	}
}

func (c *Client) peerConnect() (err error) {
	c.conn, c.client, err = connect.EstablishGRPC(c.ctx, c.params)
	return
}
