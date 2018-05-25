package inboxes

import "github.com/nedscode/transit/proto"

// Subscriber is an interface that defines a wildSubscriber that can accept an entry.
type Subscriber interface {
	CanAccept(entry *transit.Entry) bool
}
