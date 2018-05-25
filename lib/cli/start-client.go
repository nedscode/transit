package cli

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"

	"github.com/nedscode/transit/proto"
)

func (c *Config) withLocal(key string, cb afterFunc) afterFunc {
	return func() (err error) {
		_, c.node, _, _, err = c.dialNode(fmt.Sprintf(":%d", c.GRPCPort), key)
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
					c.logger.Info("found a live node")
					return
				}
				if leading {
					// If the node thinks it's the leader...
					err = c.joinCluster(client, key)
					if err == nil {
						c.logger.Info("found a leader node")
						return
					}
					c.logger.WithError(err).Info("error joining cluster")
				} else if leader != "" {
					if !tried[peer] {
						// Try this peer next
						c.logger.Info("trying leader node next")
						continue
					} else {
						c.logger.Info("supposed leader already tried", peer)
					}
				}
			} else {
				c.logger.WithError(err).Info("error with node")
			}
		} else {
			c.logger.Info("already tried this node")
		}

		// Mark the peer as tried
		tried[peer] = true

		// Get the next peer
		i++
		if i < n {
			c.logger.Info("proceeding to next node")
			peer = peers[i]
		}
	}

	return
}

func (c *Config) getPeersKey() (peers []string, key string) {
	cc := strings.Split(c.Cluster, "/")
	if cc[0] != "transit:" || len(cc) < 4 {
		return
	}

	key = cc[3]
	if len(key) != 48 {
		key = ""
		return
	}

	peers = strings.Split(cc[2], ",")
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

	c.logger.WithField("host", host).WithField("key", key).Info("dialling node")
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

	c.logger.Info("pinging node")
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
