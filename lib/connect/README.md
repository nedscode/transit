# Connecting to gRPC

In transit, the connect lib is responsible for connecting to the gRPC server component, and returns a client that has access methods to make calls with.

Generally though you can use this lib to make a connection you will probably want to use the higher level client insead. (The high level client lib is available at [github.com/nedscode/transit/client](https://github.com/nedscode/transit/client))

### Example usage:

This is an example, using the low level connect library.

```go
package main

import (
	"context"
	"fmt"

	"github.com/nedscode/transit/lib/connect"
	"github.com/nedscode/transit/proto"
)

func main() {
    params, err := connect.ParseURI("transit://127.0.0.1:9105/myKey", nil)
    if err != nil {
        panic(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    _, client, err := connect.EstablishGRPC(ctx, params)
    if err != nil {
        panic(err)
    }

    pong, err := client.Ping(ctx, &transit.Pong{})
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Got pong", pong)
}
```
