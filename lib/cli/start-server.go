package cli

import (
	"context"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/gogo/gateway"
	"github.com/grpc-ecosystem/go-grpc-middleware/validator"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"github.com/norganna/style"

	"github.com/nedscode/transit/lib/connect"
	"github.com/nedscode/transit/lib/inboxes"
	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/lib/server"
	"github.com/nedscode/transit/proto"
)

func init() {
	addHelp(serverGroup, "start-server", "Start up a server (default command)")
}

func (c *Config) startServerCommand([]string) (cb afterFunc, err error) {
	cb = c.startServer
	return
}

func (c *Config) startServer() error {
	if c.Address == "" {
		c.logger.Fatal("Address must be specified")
	}

	// Our clusterID will be our "peer accessible" IP and gRPC (not raft) port.
	c.clusterID = style.Sprintf("%s:%d", c.Address, c.GRPCPort)
	bootstrap := c.ClusterURI == ""
	c.raft = raft.New(c.clusterID, style.Sprintf(":%d", c.ClusterPort), c.DataDir, bootstrap, c.logger)
	c.raft.SeedKey = c.ClusterSeed
	c.logger.Info("Serving raft protocol on ", c.ClusterPort)
	c.raft.Start(c.ctx, c.Persist)

	// Listen on all interfaces at the gRPC port.
	listener, err := net.Listen("tcp", style.Sprintf(":%d", c.GRPCPort))
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to listen")
	}

	// If we haven't bootstrapped (first server) then we connect to existing cluster peers and ask them to include us.
	var key string
	if !bootstrap {
		var peers []string
		peers, key = c.getPeersKey()

		if addr := c.raft.Leader(); addr != "" {
			c.logger.WithField("leader", addr).Info("Connected to raft nodes")
		} else {
			if len(peers) == 0 {
				c.logger.Fatal("Cluster URI is malformed")
			}

			c.params, err = connect.ParseURI(c.ClusterURI, nil)
			if err != nil {
				c.logger.WithError(err).WithField("uri", c.ClusterURI).Fatal("Failed to parse connection URI")
			}

			if c.params.TokenType != "cluster" {
				c.logger.Fatalf("Cannot connect to cluster with a %s token (needs cluster token)", c.params.TokenType)
			}

			c.params.RequireType = "cluster"

			_, client, cErr := connect.EstablishGRPC(c.ctx, c.params)
			if cErr != nil {
				c.logger.WithError(cErr).Fatal("Cannot connect to leader node")
			}

			ctx, cancel := c.timeout()
			defer cancel()

			ret, jErr := client.ClusterJoin(
				ctx,
				&transit.Server{
					ID:      c.clusterID,
					Address: style.Sprintf("%s:%d", c.Address, c.ClusterPort),
				},
				grpc.PerRPCCredentials(&transit.TokenCredentials{
					Token: key,
				}),
			)

			if jErr != nil {
				c.logger.WithError(jErr).Fatal("Failed to connect to existing cluster, specify a current cluster URI")
			}

			if !ret.Succeed {
				c.logger.WithField("err", ret.Error).Fatal("Failed to join existing cluster")
			}
		}
	} else {
		key = c.raft.Key()
	}

	// Create a new gRPC server.
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc_validator.UnaryServerInterceptor()),
		grpc.StreamInterceptor(grpc_validator.StreamServerInterceptor()),
	}

	proto := "http"
	if c.TLSMode != "off" {
		proto += "s"
		opts = append(opts, grpc.Creds(c.certs.ServerCredentials(c.TLSMode == "anon")))
	}

	rpcServer := grpc.NewServer(opts...)

	// Register a new server as the gRPC handler.
	transit.RegisterTransitServer(rpcServer, server.New(c.ctx, c.logger, c.raft, inboxes.SyncNone))

	// Serve gRPC Server
	c.logger.Infof("Serving gRPC on %s://%s:%d", proto, c.Address, c.GRPCPort)
	go func() {
		// rpcServer.Serve() will run forever or until a fatal error
		c.logger.WithError(rpcServer.Serve(listener)).Fatal("Listener failed")
	}()

	conn, _, _, _, err := c.dialNode(style.Sprintf("%s:%d", c.Address, c.GRPCPort), c.raft.Key())
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to start local client")
	}

	mux := http.NewServeMux()

	err = c.registerGateway(mux, conn)
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to serve gRPC gateway")
	}

	err = serveAdmin(mux)
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to serve Admin UI")
	}

	err = serveAPI(mux)
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to serve API UI")
	}

	// Save the current cluster connection string
	c.ClusterURI = style.Sprintf("transit://%s/%s", strings.Join(c.raft.PeerAddresses(), ","), key)
	c.logger.Infof("Boot additional servers with `-cluster %s`", c.ClusterURI)
	ioutil.WriteFile(path.Join(c.DataDir, "cluster"), []byte(c.ClusterURI), 0600)

	gatewayAddr := style.Sprintf("%s:%d", c.Address, c.GatewayPort)
	c.logger.Infof("Serving gRPC gateway on %s://%s", proto, gatewayAddr)
	c.logger.Infof("Serving admin management on %s://%s%s", proto, gatewayAddr, "/admin/")
	c.logger.Infof("Serving API documentation on %s://%s%s", proto, gatewayAddr, "/doc/")

	gatewayAddr = style.Sprintf(":%d", c.GatewayPort)
	gwServer := http.Server{
		Addr:      gatewayAddr,
		TLSConfig: c.certs.ServerConfig(c.TLSMode == "anon"),
		Handler:   mux,
	}

	if c.TLSMode == "off" {
		c.logger.WithError(gwServer.ListenAndServe()).Fatal("Gateway server failed")
	}

	certFile := path.Join(c.DataDir, "cert.pem")
	keyFile := path.Join(c.DataDir, "key.pem")

	style.Println("‹hc:Server is running›")
	c.logger.WithError(gwServer.ListenAndServeTLS(certFile, keyFile)).Fatal("Gateway server failed")

	return nil
}

func (c *Config) joinCluster(client transit.TransitClient, key string) error {
	ctx, cancel := c.timeout()
	defer cancel()

	c.logger.WithField("key", key).Info("Joining raft")
	join, err := client.ClusterJoin(
		ctx,
		&transit.Server{
			ID:      c.clusterID,
			Address: style.Sprintf("%s:%d", c.Address, c.ClusterPort),
		},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)

	if err == nil && !join.Succeed {
		err = style.Errorf("failed to join cluster: %s", join.Error)
	}

	return err
}

func (c *Config) getLeader(client transit.TransitClient, key string) (string, error) {
	ctx, cancel := c.timeout()
	defer cancel()

	leader, err := client.ClusterLeader(
		ctx,
		&transit.Void{},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)

	if err == nil && leader.Value == "" {
		err = style.Errorf("failed to get leader, empty")
	}

	return leader.Value, err
}

func (c *Config) registerGateway(mux *http.ServeMux, conn *grpc.ClientConn) (err error) {
	jsonpb := &gateway.JSONPb{
		EmitDefaults: true,
		Indent:       "  ",
		OrigName:     true,
	}

	gwmux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, jsonpb),
		// This is necessary to get error details properly
		// marshalled in unary requests.
		runtime.WithProtoErrorHandler(runtime.DefaultHTTPProtoErrorHandler),
	)

	err = transit.RegisterTransitHandler(context.Background(), gwmux, conn)
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to register gateway")
	}

	mux.Handle("/", gwmux)
	return
}
