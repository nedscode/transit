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

	// ErrIncorrectTokenType is an error returned when a cluster URI is provided for a gRPC connection or vice-versa.
	ErrIncorrectTokenType = errors.New("token type is incorrect for this kind of connection")

	// ErrInvalidURI is an error returned when a URI is provided that is not parseable.
	ErrInvalidURI = errors.New("invalid connection uri provided")

	// ErrInvalidToken is an error returned when a URI is provided that doesn't have a valid token.
	ErrInvalidToken = errors.New("invalid token in connection uri")

	// ErrInvalidTLSMode is an error returned when an unknown TLS mode parameter is provided in the uri.
	ErrInvalidTLSMode = errors.New("invalid TLS Mode specified")

	// ErrInvalidKeyFile is an error returned when a key file is not readable.
	ErrInvalidKeyFile = errors.New("error reading provided key file")

	// ErrInvalidCertFile is an error returned when a cert file is not readable.
	ErrInvalidCertFile = errors.New("error reading provided certificate file")

	// ErrInvalidCert is an error returned when a key+cert pair is not able to be processed into a connection cert.
	ErrInvalidCert = errors.New("unable to parse key and certificate into credentials")

	// ErrInvalidDuration is an error returned when a timeout value is not parseable.
	ErrInvalidDuration = errors.New("unable to parse duration value")

	// ErrInvalidRetries is an error returned when a retry count is not parseable.
	ErrInvalidRetries = errors.New("unable to parse retries value")
)

const (
	defaultConnectTimeout = 500 * time.Millisecond
	defaultReadTimeout    = 1000 * time.Millisecond
	defaultRetries        = 3
)

// EstablishGRPC connects to the given GRPC peers and optionally connects to the leader of the cluster.
func EstablishGRPC(ctx context.Context, p *Parameters) (conn *grpc.ClientConn, client transit.TransitClient, err error) {
	if p.RequireType != "" && p.TokenType != p.RequireType {
		err = ErrIncorrectTokenType
		return
	}

	var opts []grpc.DialOption

	switch p.TLSMode {
	case 0:
		opts = append(opts, grpc.WithInsecure())
	case 1:
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})))
	case 2:
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{p.Certificate},
		})))
	}

	if p.Token != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(&transit.TokenCredentials{
			Token: p.Token,
		}))
	}

	var tried map[string]bool
	var connect func(peer string) bool
	var peerFound bool

	connect = func(peer string) bool {
		if tried[peer] {
			return false
		}

		cCtx, cancel := context.WithTimeout(ctx, p.ConnectTimeout)
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

		rCtx, cancel := context.WithTimeout(ctx, p.ReadTimeout)
		defer cancel()

		var pong *transit.Pong
		pong, err = client.Ping(rCtx, &transit.Pong{})
		if err != nil {
			return false
		}

		peerFound = true

		if !p.LeaderOnly || pong.Leading {
			return true
		}

		if pong.Leader != "" {
			return connect(pong.Leader)
		}

		return false
	}

	// Create a copy of peers
	peers := make([]string, len(p.Peers))
	copy(peers, p.Peers)

	// Shuffle peers copy, so we get a random connect order
	n := len(peers)
	for i := 0; i < n-2; i++ {
		j := rand.Int31n(int32(n - 1))
		peers[i], peers[j] = peers[j], peers[i]
	}

	// Repeat up to RetryCount times
	for retry := 0; retry < p.RetryCount; retry++ {
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
