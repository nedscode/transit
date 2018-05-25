package cli

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/gogo/gateway"
	"github.com/grpc-ecosystem/go-grpc-middleware/validator"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/lib/server"
	"github.com/nedscode/transit/proto"
)

func (c *Config) startServerCommand([]string) (cb afterFunc, err error) {
	cb = c.startServer
	return
}

func (c *Config) startServer() error {
	if c.Address == "" {
		c.logger.Fatal("Address must be specified")
	}

	// Our clusterID will be our "peer accessible" IP and gRPC (not raft) port.
	c.clusterID = fmt.Sprintf("%s:%d", c.Address, c.GRPCPort)
	bootstrap := c.Cluster == ""
	c.raft = raft.New(c.clusterID, fmt.Sprintf(":%d", c.ClusterPort), c.DataDir, bootstrap, c.logger)
	c.raft.SeedKey = c.ClusterKey
	c.logger.Info("Serving raft protocol on ", c.ClusterPort)
	c.raft.Start(c.ctx, c.Persist)

	// Listen on all interfaces at the gRPC port.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", c.GRPCPort))
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

			//TODO implement new connect.Establish() call instead
			_, client, cErr := c.connectNode(peers, key, true)
			if cErr != nil {
				c.logger.WithError(cErr).Fatal("Cannot connect to leader node")
			}

			ctx, cancel := c.timeout()
			defer cancel()

			ret, jErr := client.ClusterJoin(
				ctx,
				&transit.Server{
					ID:      c.clusterID,
					Address: fmt.Sprintf("%s:%d", c.Address, c.ClusterPort),
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
	transit.RegisterTransitServer(rpcServer, server.New(c.logger, c.raft))

	// Serve gRPC Server
	c.logger.Infof("Serving gRPC on %s://%s:%d", proto, c.Address, c.GRPCPort)
	go func() {
		// rpcServer.Serve() will run forever or until a fatal error
		c.logger.WithError(rpcServer.Serve(listener)).Fatal("Listener failed")
	}()

	conn, _, _, _, err := c.dialNode(fmt.Sprintf("%s:%d", c.Address, c.GRPCPort), c.raft.Key())
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
	c.Cluster = fmt.Sprintf("transit://%s/%s", strings.Join(c.raft.PeerAddresses(), ","), key)
	c.logger.Infof("Boot additional servers with `-cluster %s`", c.Cluster)
	ioutil.WriteFile(path.Join(c.DataDir, "/cluster"), []byte(c.Cluster), 0600)

	gatewayAddr := fmt.Sprintf("%s:%d", c.Address, c.GatewayPort)
	c.logger.Infof("Serving gRPC gateway on %s://%s", proto, gatewayAddr)
	c.logger.Infof("Serving admin management on %s://%s%s", proto, gatewayAddr, "/admin/")
	c.logger.Infof("Serving API documentation on %s://%s%s", proto, gatewayAddr, "/doc/")

	gatewayAddr = fmt.Sprintf(":%d", c.GatewayPort)
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

	c.logger.WithError(gwServer.ListenAndServeTLS(certFile, keyFile)).Fatal("Gateway server failed")

	return nil
}

func (c *Config) joinCluster(client transit.TransitClient, key string) error {
	ctx, cancel := c.timeout()
	defer cancel()

	c.logger.WithField("key", key).Info("joining raft")
	join, err := client.ClusterJoin(
		ctx,
		&transit.Server{
			ID:      c.clusterID,
			Address: fmt.Sprintf("%s:%d", c.Address, c.ClusterPort),
		},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)

	if err == nil && !join.Succeed {
		err = fmt.Errorf("failed to join cluster: %s", join.Error)
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
		err = fmt.Errorf("failed to get leader, empty")
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
