package cli

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/rakyll/statik/fs"
	"github.com/sirupsen/logrus"

	"github.com/nedscode/transit/lib/certs"
	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/proto"
)

type execFunc func([]string) (afterFunc, error)
type afterFunc func() error

// Config represents a CLI configuration.
type Config struct {
	DataDir     string
	Address     string
	Cluster     string
	ClusterKey  string
	GRPCPort    int
	GatewayPort int
	ClusterPort int
	TLSMode     string
	Persist     bool
	CertNames   string

	ctx       context.Context
	clusterID string
	logger    logrus.FieldLogger
	raft      *raft.Store
	certs     *certs.Store
	node      transit.TransitClient
}

// New will create a new CLI configuration.
func New(ctx context.Context, logger logrus.FieldLogger) *Config {
	return &Config{
		ctx:    ctx,
		logger: logger,
	}
}

// AddFlags allows this CLI configuration to parse CLI flags.
func (c *Config) AddFlags() {
	flag.StringVar(&c.DataDir, "data-dir", "data", "Directory to store data in")
	flag.StringVar(&c.Address, "address", "", "Our accessible IP address (first time)")
	flag.StringVar(&c.Cluster, "cluster", "", "The cluster URI (or \"boot\" to create new cluster)")
	flag.StringVar(&c.ClusterKey, "cluster-key", "", "If cluster doesn't have a key, will use this instead of random key")

	flag.IntVar(&c.GRPCPort, "grpc-port", 9105, "The gRPC server port")
	flag.IntVar(&c.GatewayPort, "gateway-port", 9106, "The gRPC-Gateway server port")
	flag.IntVar(&c.ClusterPort, "cluster-port", 9107, "The cluster communications port")

	flag.StringVar(&c.TLSMode, "tls", "full", "Set TLS mode (\"full\", \"anon\", \"off\")")

	flag.StringVar(&c.CertNames, "cert-names", "localhost,0.0.0.0,127.0.0.1", "The host names to use in certificate")

	flag.BoolVar(&c.Persist, "raft-persist", false, "Make raft state persistent")
}

// Parse causes the CLI flags to be parsed and checked.
// Add your own flags between calling `AddFlags` and `Parse` if you need.
func (c *Config) Parse() {
	flag.Parse()

	err := os.MkdirAll(c.DataDir, 0755)
	if err != nil {
		c.logger.WithError(err).Fatal("Failed to create data directory")
	}

	if c.Address == "" {
		data, err := ioutil.ReadFile(path.Join(c.DataDir, "/address"))
		if err == nil {
			c.Address = string(data)
		}
	} else {
		ioutil.WriteFile(path.Join(c.DataDir, "/address"), []byte(c.Address), 0644)
	}

	if c.Cluster == "" {
		data, err := ioutil.ReadFile(path.Join(c.DataDir, "/cluster"))
		if err == nil {
			c.Cluster = string(data)
		}
	} else {
		ioutil.WriteFile(path.Join(c.DataDir, "/cluster"), []byte(c.Cluster), 0600)
	}

	if c.Cluster == "boot" {
		c.Cluster = ""
	}

	if c.Address == "" {
		c.logger.Fatal("The address flag has not been specified")
	}
}

// Exec allows execution of a command.
func (c *Config) Exec(command string, args []string) (err error) {
	var exec execFunc
	var after afterFunc

	certFile := path.Join(c.DataDir, "cert.pem")
	keyFile := path.Join(c.DataDir, "key.pem")

	c.certs = certs.New(c.logger)
	c.certs.CheckCreateTLS(keyFile, certFile, c.CertNames)
	c.certs.LoadTLS(keyFile, certFile)

	switch command {
	case "list-tokens":
		exec = c.listTokensCommand

	case "add-token":
		exec = c.addTokenCommand

	case "gen-auth":
		exec = c.genAuthCommand

	case "start-server":
		exec = c.startServerCommand

	default:
		exec = c.helpCommand
	}

	if exec == nil {
		return fmt.Errorf("command not found: %s", command)
	}

	after, err = exec(args)
	if err != nil {
		return err
	}

	err = after()
	return
}

func (c *Config) timeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, 1*time.Second)
}

func serveAdmin(mux *http.ServeMux) error {
	statikFS, err := fs.New()
	if err != nil {
		return err
	}

	fileServer := http.FileServer(statikFS)
	prefix := "/admin/"
	mux.Handle(prefix, fileServer)
	return nil
}

// serveOpenAPI serves an OpenAPI UI on /doc/
// Adapted from https://github.com/philips/grpc-gateway-example/blob/a269bcb5931ca92be0ceae6130ac27ae89582ecc/cmd/serve.go#L63
func serveAPI(mux *http.ServeMux) error {
	mime.AddExtensionType(".svg", "image/svg+xml")

	statikFS, err := fs.New()
	if err != nil {
		return err
	}

	fileServer := http.FileServer(statikFS)
	prefix := "/doc/"
	mux.Handle(prefix, fileServer)
	return nil
}
