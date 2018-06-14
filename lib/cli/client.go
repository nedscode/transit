package cli

import (
	"errors"
	"strings"

	"github.com/norganna/style"
	"google.golang.org/grpc"

	"github.com/nedscode/transit/lib/connect"
	"github.com/nedscode/transit/proto"
)

func (c *Config) withLocal(key string, cb afterFunc) afterFunc {
	return func() (err error) {
		_, c.node, _, _, err = c.dialNode(style.Sprintf(":%d", c.GRPCPort), key)
		if err != nil {
			return
		}
		cb()
		return nil
	}
}

func (c *Config) withNode(host, key string, cb afterFunc) afterFunc {
	return func() (err error) {
		_, c.node, _, _, err = c.dialNode(host, key)
		if err != nil {
			return
		}
		cb()
		return nil
	}
}

func (c *Config) withAnyPeer(cb afterFunc) afterFunc {
	return func() (err error) {
		peers, key := c.getPeersKey()
		_, c.node, err = c.connectNode(peers, key, false)
		if err != nil {
			return
		}
		cb()
		return nil
	}
}

func (c *Config) withLeader(cb afterFunc) afterFunc {
	return func() (err error) {
		peers, key := c.getPeersKey()
		_, c.node, err = c.connectNode(peers, key, true)
		if err != nil {
			return
		}
		cb()
		return nil
	}
}

func (c *Config) connectNode(peers []string, key string, wantLeader bool) (conn *grpc.ClientConn, client transit.TransitClient, err error) {
	if len(peers) == 0 {
		return nil, nil, errors.New("no peers supplied")
	}

	if len(key) != 48 {
		return nil, nil, errors.New("invalid key supplied")
	}

	// Joining the cluster as a peer requires a connection to the leader.
	i := 0
	n := len(peers)
	peer := peers[i]
	tried := map[string]bool{}

	// Try connecting to all supplied peers
	for i < n {
		if !tried[peer] {
			var leading bool
			var leader string

			_, client, leading, leader, err = c.dialNode(peer, key)
			// If we have connected without error
			if err == nil {
				if !wantLeader {
					// Request indicated that leadership was not necessary
					c.logger.Info("Found a live node")
					return
				}
				if leading {
					// If this node thinks it's the leader, then we've arrived
					if err == nil {
						c.logger.Info("Found a leader node")
						return
					}
					c.logger.WithError(err).Info("Error joining cluster")
				} else if leader != "" {
					if !tried[peer] {
						// Try this peer next
						c.logger.Info("Trying leader node next")
						continue
					} else {
						c.logger.Info("Supposed leader already tried", peer)
					}
				}
			} else {
				c.logger.WithError(err).Info("Error with node")
			}
		} else {
			c.logger.Info("Already tried this node")
		}

		// Mark the peer as tried
		tried[peer] = true

		// Get the next peer
		i++
		if i < n {
			c.logger.Info("Proceeding to next node")
			peer = peers[i]
		}
	}

	return
}

func (c *Config) getPeersKey() (peers []string, key string) {
	params, err := connect.ParseURI(c.ClusterURI, nil)
	if err != nil {
		return
	}

	key = params.Token
	if len(key) != 48 {
		key = ""
		return
	}

	peers = params.Peers
	if len(peers) == 0 {
		key = ""
	}
	return
}

func (c *Config) dialNode(host, key string) (conn *grpc.ClientConn, client transit.TransitClient, leading bool, leader string, err error) {
	ctx, cancel := c.timeout()
	defer cancel()

	if strings.HasPrefix(host, c.Address+":") {
		host = "127.0.0.1" + host[len(c.Address):]
	}

	c.logger.WithField("host", host).WithField("key", key).Info("Dialling node")
	// We always dial our local context

	opts := []grpc.DialOption{
		grpc.WithBlock(),
	}

	if c.TLSMode == "full" {
		opts = append(opts, grpc.WithTransportCredentials(c.certs.ClientCredentials()))
	} else if c.TLSMode == "off" {
		opts = append(opts, grpc.WithInsecure())
	} else if c.TLSMode == "anon" {
		opts = append(opts, grpc.WithTransportCredentials(c.certs.AnonClient()))
	} else {
		c.logger.WithField("tls", c.TLSMode).Fatal("Unrecognised TLS mode flag")
	}

	conn, err = grpc.DialContext(ctx, host, opts...)

	if err != nil {
		return
	}

	c.logger.Info("Pinging node")
	pingCtx, pingCancel := c.timeout()
	defer pingCancel()

	client = transit.NewTransitClient(conn)
	pong, err := client.Ping(
		pingCtx,
		&transit.Pong{
			ID: 1,
		},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)
	if err != nil {
		c.logger.WithError(err).Fatal("Error calling server ping")
	}

	if pong.ID != 1 {
		c.logger.Fatal("Wrong ID back from pong")
	}
	leading = pong.Leading
	leader = pong.Leader

	return
}
