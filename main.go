//go:generate proto/proto.sh
package main

import (
	"context"
	"flag"
	"net/http"

	"github.com/norganna/formatrus"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/nedscode/transit/lib/cli"
	_ "github.com/nedscode/transit/statik"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logrus.New()
	formatter := formatrus.New()
	logger.Formatter = formatter
	formatter.
		Order(100, "req").
		Order(90, "id").
		Order(-100, "ret", "res")
	formatter.ParagraphBlock = true

	c := cli.New(ctx, logger)
	c.AddFlags()
	c.Parse()

	grpc.EnableTracing = true
	go func() {
		http.ListenAndServe(":9000", nil)
	}()

	var command string
	args := flag.Args()
	if len(args) == 0 {
		command = "start-server"
	} else {
		command = args[0]
		args = args[1:]
	}

	err := c.Exec(command, args)
	if err != nil {
		logger.WithError(err).Fatal("Failed to execute command")
	}
}
