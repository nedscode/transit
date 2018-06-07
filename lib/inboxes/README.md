# Inboxes

In Transit, the inbox is the central piece of the puzzle that keeps track of and delivers incoming items to subscribers.

An `Inbox` is created for each subscribed prefix (topic) and group.

When a published `Entry` is added using the `Inboxes` `Add` method, the entry's `Topic:Instance` is compared against
each subscription prefix, and added to each group's `Inbox` for delivery.

Transit comes with a single type of inbox built in, that of the `MemoryInbox`. However you can feel free to build your
own derivative application and provide your own `Inbox` implementation, for example to incorporate file or database
backed inboxes.

## Selection

Inboxes are designed to be a best effort FIFO queue, except when delivery scheduling prohibits delivery of a given item to a given subscriber.

When asked for the next item, a selector is passed which defines if an item is deliverable to a given client, such a selector will have a CanAccept method, which given an entry will determine if it's acceptable or not for the subscriber.

![Delivery model](https://github.com/nedscode/transit/raw/master/static/images/inboxes.jpeg)

This can result in slightly non sequential output as pictured in this diagram.

You can see that Entries (A), (B), (C), (D) and (E) are placed into an inbox by publishers. Items (A), (C) and (D) have the same color (`Identity`), and the queue has been defined with a `Serial` delivery strategy.

When the Next call is made at step 1, the first-in item (A) is retrieved from the bottom of the Inbox.

At step 2, the second item (B) is retrieved from the Inbox.

At step 3, an item (A) with the same Identity as the third item (C) is still being processed by the first subscriber, so it's not acceptable and the third subscriber is given the forth item instead.

Now the first subscriber acknowledges that the first item (A) has been processed, at step 4, so that the first subscriber can now get (C) at step 5.

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
