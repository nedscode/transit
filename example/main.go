package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/golang/protobuf/ptypes"

	"github.com/nedscode/transit/lib/client"
	"github.com/nedscode/transit/lib/connect"

	// This contains the Bar proto message which is used as in the example below.
	"github.com/nedscode/transit/example/bar"
)

func main() {
	var uri string
	if data, err := ioutil.ReadFile("data/cluster"); err == nil {
		uri = string(data)
	}
	flag.StringVar(&uri, "cluster", uri, "The cluster URI (defaults to contents of data/cluster)")
	flag.Parse()

	if uri == "" {
		fmt.Println("Supply a connection uri with `-cluster`.")
		os.Exit(1)
	}

	tc, err := transit.Connect(
		context.Background(),
		transit.URI(uri),
	)
	if err != nil {
		if err == connect.ErrIncorrectTokenType {
			fmt.Println("You have provided a cluster token to connect as a client, please use a master token.")
			os.Exit(1)
		}

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

	notBefore := time.Now().Add(10 * time.Second)
	notAfter := time.Now().Add(30 * time.Second)

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

	p := tc.Publish(
		"foo.bar.baz",
		"123",
		message,
		tc.Lot("abc"),
		tc.NotBefore(notBefore),
		tc.NotAfter(notAfter),
		tc.Concern(transit.ProcessedConcern),
		tc.Timeout(100*time.Millisecond),
	)

	p.Done(true)
	if err = p.Err(); err != nil {
		panic(err)
	}

	fmt.Printf("Published my entry, got ID %d, concern %d, err %#v\n", p.ID(), p.Concern(), p.Err())

	// The handler will take a single message from the queue, acking it when the handler finishes.
	// If the handler returns an error, the process stops and the error is returned to the main
	// process. Once the main process deals with the error, it may re-call Handle to resume.
	// If the client dies or disconnects while processing a message, without acking it, the
	// message will be returned back to the queue on the Transit server for another process
	// to handle.
	err = sub.Handle(func(e *transit.Entry) (err error) {
		dyn := &ptypes.DynamicAny{}
		err = ptypes.UnmarshalAny(e.Message, dyn)
		if err == nil {
			// Process message
			fmt.Printf("Hey! I just got a %s #%d\n", e.Topic, e.ID)

			if v, ok := dyn.Message.(*bar.Bar); ok {
				fmt.Printf("My %s arrived with a baz of %s\n", e.Topic, v.Baz)
			}
		}
		return transit.ErrShuttingDown
	})
	if err != nil && err != transit.ErrShuttingDown {
		panic(err)
	}
}
