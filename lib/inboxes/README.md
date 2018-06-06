# Inboxes

In Transit, the inbox is the central piece of the puzzle that keeps track of and delivers incoming items to subscribers.

An `Inbox` is created for each subscribed prefix (topic) and group.

When a published `Entry` is added using the `Inboxes` `Add` method, the entry's `Topic:Instance` is compared against
each subscription prefix, and added to each group's `Inbox` for delivery.

Transit comes with a single type of inbox built in, that of the `MemoryInbox`. However you can feel free to build your
own derivative application and provide your own `Inbox` implementation, for example to incorporate file or database
backed inboxes.

## Usage

```go
package main

import (
	"context"
	
	"github.com/sirupsen/logrus"
	
	"github.com/nedscode/transit/lib/inboxes"
	"github.com/nedscode/transit/proto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	logger := logrus.New()
	
	boxes := inboxes.New(ctx, logger, nil, inboxes.SyncNone)
	
	prefix := "foo.bar"
	group := "my-group"
	inbox := boxes.Inbox(prefix, group, nil)
	boxes.Add(inboxes.Wrap(&transit.Entry{
		Topic: "foo.bar.baz", 
		Identity: "123",
	}))
	
	entry, changed := inbox.Next(inboxes.AcceptAny)
	if entry != nil {
		inbox.Ack(&transit.Acknowledgement{
			Sub: &transit.Sub{
				Prefix: prefix,
				Group:  group,
				ID:     entry.ID,
			},
			Ack: true,
		})
		
		select {
		case <-changed:
			logger.Info("Entry was ACKed")
		default:
			logger.Warn("Entry should have been changed by ACK, but wasn't")
		}
	}
}
```
