# Transit

A specialised message bus coordinator.

Allows specific routing, distribution and delivery strategies to be followed when queueing messages.

## Summary

Unlike a general purpose message bus, which has queues and groups, Transit allows message routing, delivery conditions and filtering.

For example you can establish a group, and specify that you only want to receive one message of a particular kind at a time. Or if a second item comes in while one is waiting, then you want the second one to be dropped, or maybe you want the original dropped, or maybe you don't care.

You can tell it that you don't want any messages that haven't been waiting for at least ***n*** milliseconds, or that you only want to receive messages in a particular allotments of items.

You can specify not before and expiry times on messages. Additionally delay and max ages on subscriptions.

It can even automatically assign allotments to certain subscribers to smartly distribute item load.

## What's possible...

While the usages are nearly limitless, the following may serve as an example of a few of the possibilities:

##### Prioritising messages:

You can have messages inserted with allotments of "high priority" and "normal priority", and have 2 subscriptions, one which selects the high priority only messages, and one which selects all priorities.

When a high priority message comes through, you will be able to process them first.

##### Debouncing noisy messages:

Subscribing with a delay on your subscription and a deliver strategy of "ignore" will allow you to filter out noisy messages until they quiet down.

For example if you have a lot of updates happening for messages with a given identity, thus causing a lot of load on your processing services, you may wish to debounce them.

To do this you can set a delay of 500ms on your subscriptions and set a delivery strategy of "ignore".

When a message comes in it will sit in the queue for a minimum of 500ms before delivery is attempted.

If another message arrives with the same identity into the queue within the same time, it will remove the existing delayed message and put itself at the back of the queue.

You won't get alerted until it's been waiting for more than 500ms without a duplicate arriving.

##### Preventing race conditions:

Say you have a process where processing items simultaneously can be dangerous and cause race conditions.

If you created a queue with a delivery policy of Serial, and make sure that the identity for conflicting messages is set the same, then you can ensure that not more than one message of a type can be processed at a time, while allowing other message of different types to be processed in parallel.

An example of this is you could set the identity for a message to be the client id, thus enforcing that only one message per client is processed at once.

##### Load balancing messages:

Say you have a bunch of processes each of which can process arbitrary messages, but requires it to load a large set of data for a particular event. When you are processing subsequent messages, you would like future messages for the given event to go back to the service that already has the data loaded as it will be faster at processing it.

You can give the message an Allotment of the event id, and then turn on Assigned distribution for the subscription. New messages that arrive with a new allotment will be assigned to a queue based on queue load levels, and then directed towards that server. However messages with existing allotments will go to the currently assigned server(s).

Taking this a step further, you may have a particularly large event coming up, and want to spin up multiple services dedicated to processing just this one event. You can connect them to the queue with a subscription specifying just the allotment they wish to receive and specify the Requested distribution mode, which means they will solely process things assigned to that allotment and no other processes will receive messages for that event.

## Operation

The Transit server is a streaming gRPC server.

Clients connect directly via a client library and communicate via exchanged gRPC (protobuf) messages.

Multiple services run in a cluster with a single raft elected master service. If the master service fails, a new service will take over the leadership and begin processing messages.

New groups are created upon first client subscription, and will be kept alive for up to 3 hours after the last client disconnects. Incoming messages for the group will arrive in a dedicate inbox for that group and assigned to subscribers depending upon the delivery strategy and distribution requirements of each subscriber.

A client will connect to us, and subscribe to one or more topics with a group name.

The service maintains durability for a queue with a given subscription subject prefix and group name for 3 hours after the last subscriber disconnects. After that the queue is dropped along with all messages.

Transit is:
 - **Durable** - Message queueing is maintained even if your application dies / restarts.
 - **Resilient** - If one Transit dies, another will take over the job.
 - **Gracefully persistent** - 
     Upon a scheduled shutdown the master server will step down, and attempt to gracefully migrate it's queues to a new leader server.

Transit is NOT:
 - **Fully persistent** -
     If your transit server crashes, loses connectivity etc, the queues, messages and states on it are lost.

There will possibly (dependant upon demand/resources available) be work done in the future to allow (selectably) fully and mostly persistent modes of operation.

These theoretical modes however will require additional processing, network communications, and by definition slow down the queue processing speed (hence the selectability) due to sync locking.

## Example code

Usually when I look at something new, I want to see what kind of code I will have to write, this is it (full example in example/ directory):

```go
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
			fmt.Printf("Hey! I just got a %s #%d (%s): %#v\n", e.Topic, e.ID, message)

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
```

The first time this is called, the server will create a new inbox called "transactions.usvc/foo.bar.*" which will receive any message with a subject matching the prefix `foo.bar.`, according to the Entry rules.

The messages will then be served out to any connected clients in order of receipt, except where messages are not ready to be transmitted yet, in which case they will be transmitted as soon as they are able.

#### Identity/Delivery - destination and parallel delivery options:

The Identity field allows you to specify a key to use to determine if the item already exists, and what action to take when another item arrives.
 
The Strategy you select when subscribing determines the action that will occur, and can be one of:
- `Drop` -
  If there's currently one waiting, don't bother with the new one and forget it.
  This can be useful if the processing of the current one will fulfil the requirements.
- `Replace` -
  If there's currently one waiting, replace it with the new one instead updating the content.
  This will preserve the items position in the queue, yet have the content of the new item.
- `Ignore` -
  If there's currently one waiting, delete it and place this one at the end of the queue.
  This acts like a debounce, and will cause the processing to delay until the item reaches the start of the queue again.
- `Serial` -
  Multiple items with the same `Identity` are able to co-exist in the group, but only one may be delivered to subscribers at a time.
- `Concurrent` (default) -
  There are no restrictions placed on the queue, items are delivered in order of arrival as soon as possible.

Note: `Drop`, `Replace` and `Ignore` act like `Serial` in that if one item is currently being processed, identical messages will wait for that item to be acknowledged before being delivered to a subscriber.
      
#### Lot/Distribution - destination distribution allotment:

- `Requested` -
  Only send us items from the lots we have asked for. If nothing's there, don't send anything else.
- `Assigned` -
  Take items from lots we have asked for first, and auto assign more lots to us if we've got nothing to do.
- `Arbitrary` (default) -
  Take items from lots we have asked for first, then just send us anything left over that's not assigned.
 
## Installing

Transit requires `vgo`, if you don't have it, get it installed first:
```bash
go get golang.org/x/vgo
```

Use `vgo` to get and build the transit binary:
```bash
vgo get github.com/nedscode/transit
```

## Compiling protobuf

The code contains the latest compiled protobuf. If you make changes to it and need to recompile it, you will need to install gogoproto's gogoslick binary and then regenerate the go proto file. 

```bash
go get github.com/gogo/protobuf/proto
go get github.com/gogo/protobuf/protoc-gen-gogo
go get github.com/gogo/protobuf/gogoproto

cd $GOPATH/github.com/nedscode/transit
vgo generate
```

## To TLS or not to TLS

Transit takes security seriously, but gives you the choice.

Even if you opt for non-TLS mode, all API keys issued have a time-limited one-time derivative ("auth") token which is generated for each request and is used for per-request authorisation.

The auth key is generated just prior to your request and you are protected against out of sequence or replay attacks.

Transit supports 3 different TLS modes:

- `-tls full` (default)
- `-tls anon`
- `-tls off`

In full mode, TLS is enabled, and all gRPC clients must use a client cert derived from the server certificate.
REST clients may connect without a client certificate. While Raft communications happen in the clear, connections can only be requested by gRPC.

In anon mode, TLS is enabled, but any client may connect with or without a client certificate. The client certificate if provided is not checked, but all data is still encrypted.

In off mode, TLS is disabled. All communications happen in plain HTTP/2, including REST.

## REST Queries

It may seem a little complicated to generate a request token when you're using third party libraries.

#### Generating for yourself

Fortunately it's relatively simple to generate an auth token in pretty much any language.

To prove this, here's an example in bash (be sure to set TOKEN to be your master token):

```bash
MASTER="kkRGaorSQWD3-2K3aicKpHXobkksLxmBD"
NONCE=1
PUBLIC="${MASTER%%-*}"
PRIVATE="${MASTER##*-}"
ENTROPY=`printf "%x-%x" $(date +%s) $NONCE`
CHECKSUM=`md5 -qs "$PRIVATE-$ENTROPY"`
AUTH="$PUBLIC-$ENTROPY-$CHECKSUM"

curl -vvvk -H "Authorization: Token $AUTH" https://localhost:9105/api/v1/ping/1
```

#### The easy way

Alternatively you can generate the key using the transit binary itself, if it's authorised to connect to the cluster:

```bash
AUTH=`transit gen-auth kkRGaorSQWD3`
curl -vvvk -H "Authorization: Token $AUTH" https://localhost:9105/api/v1/ping/1
```

***Note:***

To authorise a transit binary to connect to the cluster, run it once with the <code>-cluster transit://***PEERS***/***KEY***</code> option.

You can get the connection URI from the `data/cluster` file on any running node.

It will connect and save the cluster details for that and any subsequent connections.
