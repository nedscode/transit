# Raft Store

This lib instantiates a raft instance and manages it to provide leader elections and synchronisation of a clustered key-value store across the cluster.

## Example usage:

This example will start up a standalone raft server:

```go
package main

import (
	"context"
	
	"github.com/nedscode/transit/lib/raft"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	raft := raft.New("ID1", ":9001", "data/", true, logger)
	err := raft.Start(context.Background(), false)
	if err != nil {
		panic(err)
	}
	
	// To join another server to us: 
	err = raft.Join("ID2", "127.0.0.1:9002")
	if err != nil {
		panic(err)
	}
	
	// To set values: 
	err = raft.Set("key", "value", "", "")
	if err != nil {
		panic(err)
	}
}
```