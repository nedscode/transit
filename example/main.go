package main

import (
	"context"
	"fmt"
	"time"

	// The following file is contained in the example/bar folder and contains the Bar struct used below.
	"github.com/nedscode/transit/example/bar"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/nedscode/transit/lib/client"
)

func main() {
	tc, err := transit.Connect(
		context.Background(),
		transit.Peers("192.168.1.1:9105", "192.168.1.2:9105"),
		transit.MasterToken("kkRGaorSQWD3-2K3aicKpHXobkksLxmBD"),
		transit.AnonTLS(),
	)
	if err != nil {
		panic(err)
	}

	// In this example, we are subscribing to any message prefixed by `foo.bar` into a persistent
	// inbox "foo.bar/transactions".
	// Only the requested lots "abc" and "def" will be delivered to us.
	// We're going to set the delivery policy of the entire queue to "Ignore" which will cause new
	// duplicate items to remove existing items and go to the back of the queue.
	// Since there's a 500ms delay on items, and duplicate items will be replaced at the back of the
	// queue, this implies a message needs to be unique within.
	sub, err := tc.Subscribe(
		"foo.bar",                               // What we want to subscribe to (foo.bar.*)
		"transactions",                          // Our group name, group queues are persisted for 3 hours after last disconnect
		transit.Allot("abc", "def"),             // Lets the server know we want these lots delivered to us
		transit.Delay(500*time.Millisecond),     // Don't send us any items until they're this old (in queue for 500ms)
		transit.MaxAge(5*time.Minute),           // Don't allow items to be in this queue longer than specified duration
		transit.Distribution(transit.Requested), // Sets our distribution mode (only what we've requested)
		transit.Delivery(transit.Ignore),        // Sets (or changes) the global delivery strategy for the queue
	)
	if err != nil {
		panic(err)
	}

	message, err := ptypes.MarshalAny(&bar.Bar{
		Baz: "xyz",
	})
	if err != nil {
		panic(err)
	}

	notBefore := time.Now().Add(10*time.Second).Unix() / int64(time.Millisecond)
	notAfter := time.Now().Add(30*time.Second).UnixNano() / int64(time.Millisecond)

	e := &transit.Entry{
		Topic:     "foo.bar.baz",
		Lot:       "abc",             // Used to determine distribution allotment
		Identity:  "123",             // Used to determine replacement and parallel delivery strategy
		Message:   message,           // The Any that is the message
		NotBefore: uint64(notBefore), // Message must wait for this long in the queue
		NotAfter:  uint64(notAfter),  // Message must be processed before this time has elapsed
	}

	// This will publish a `&Bar` object into the `foo.bar`/`transactions` queue with a unique
	// `Identity` of "123" and an allotment of `abc`.
	// Items will go into the queue in insert order but only one "foo.bar.baz:123" can be processed
	// at one time, with any existing duplicate non-processing items being removed from the queue
	// and the new message being placed at the end.
	// The message also has a not before and after time, which will combine with the Delay and
	// MaxAge of the queue it is inserted into to find the minimal time window that the item is
	// available for.
	// It may seem unreasonable to both have an entry validity time and a queue availability time,
	// however an entry may have multiple queues it needs to be delivered to, each with different
	// requirements.
	id := tc.Publish(e)
	fmt.Printf("Published my entry, got ID %d\n", id)

	// The handler will take a single message from the queue, acking it when the handler finishes.
	// If the handler returns an error, the process stops and the error is returned to the main
	// process. Once the main process deals with the error, it may re-call Handle to resume.
	// If the client dies or disconnects while processing a message, without acking it, the
	// message will be returned back to the queue on the Transit server for another process
	// to handle.
	err = sub.Handle(func(e *transit.Entry) (err error) {
		var message proto.Message
		err = ptypes.UnmarshalAny(e.Message, message)
		if err != nil {
			// Process message
			fmt.Printf("Hey! I just got a %s #%d: %#v\n", e.Topic, e.ID, message)

			if v, ok := message.(*bar.Bar); ok && e.Topic == "foo.bar" {
				fmt.Printf("My foo bar arrived with a %s", v.Baz)
			}
		}
		return
	})
	if err != nil {
		panic(err)
	}
}
