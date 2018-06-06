# Transit server

This implements the handler functions that provide the gRPC and REST endpoints for the Transit server to communicate with clients via.

## Example

```go
package main

import (
	"context"
	"fmt"
	"net"
	
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	
	"github.com/nedscode/transit/lib/inboxes"
	"github.com/nedscode/transit/lib/raft"
	"github.com/nedscode/transit/lib/server"
	"github.com/nedscode/transit/proto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	logger := logrus.New()
	
	// Start a new standalone raft server. 
	store := raft.New("ID1", ":9001", "data/", true, logger)
	err := store.Start(context.Background(), false)
	if err != nil {
		panic(err)
	}
	
	// Create a new handler
	handler := server.New(ctx, logger, store, inboxes.SyncNone)
	
	// Open a listener locally on the given port. 
	listener, err := net.Listen("tcp", ":9005")
	if err != nil {
		panic(err)
	}
	
	// Boot up the gRPC server listening on the listener port,
	// and using our handler to handle requests.
	server := grpc.NewServer()
	transit.RegisterTransitServer(server, handler)
	panic(server.Serve(listener))
}
```
