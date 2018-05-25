package connect

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/nedscode/transit/proto"
)

var (
	// ErrNoPeers is an error returned when there are no peers supplied to connect to.
	ErrNoPeers = errors.New("no peers supplied to connect to")

	// ErrNoPeer is an error returned when there is no peer found.
	ErrNoPeer = errors.New("no peers available")

	// ErrNoLeader is an error returned when there is no leader found.
	ErrNoLeader = errors.New("no leader available")
)

// Establish connects to the given peers and optionally connects to the leader of the cluster.
func Establish(
	ctx context.Context,
	timeout time.Duration,
	retries int,
	tlsMode int,
	masterToken string,
	cert tls.Certificate,
	wantLeader bool,
	peers []string,
) (
	conn *grpc.ClientConn,
	client transit.TransitClient,
	err error,
) {
	var opts []grpc.DialOption

	switch tlsMode {
	case 0:
		opts = append(opts, grpc.WithInsecure())
	case 1:
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})))
	case 2:
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
		})))
	}

	if masterToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(&transit.TokenCredentials{
			Token: masterToken,
		}))
	}

	var tried map[string]bool
	var connect func(peer string) bool
	var peerFound bool

	connect = func(peer string) bool {
		if tried[peer] {
			return false
		}

		cCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		conn, err = grpc.DialContext(
			cCtx,
			peer,
			opts...,
		)
		if err != nil {
			return false
		}

		client = transit.NewTransitClient(conn)

		rCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var pong *transit.Pong
		pong, err = client.Ping(rCtx, &transit.Pong{})
		if err != nil {
			return false
		}

		peerFound = true

		if !wantLeader || pong.Leading {
			return true
		}

		if pong.Leader != "" {
			return connect(pong.Leader)
		}

		return false
	}

	// Create a copy of peers
	x := make([]string, len(peers))
	copy(x, peers)
	peers = x

	// Shuffle peers copy, so we get a random connect order
	n := len(peers)
	for i := 0; i < n-2; i++ {
		j := rand.Int31n(int32(n - 1))
		peers[i], peers[j] = peers[j], peers[i]
	}

	// Repeat up to retries times
	for retry := 0; retry < retries; retry++ {
		// Try all our peers and their suggested leader once per retry
		tried = map[string]bool{}

		// For each peer in our list
		for _, peer := range peers {
			if ok := connect(peer); ok {
				err = nil
				return
			}
		}
	}

	if peerFound {
		err = ErrNoLeader
	}
	err = ErrNoPeer
	return
}
