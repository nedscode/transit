package inboxes

// Subscriber is an interface that defines a wildSubscriber that can accept an entry.
type Subscriber interface {
	CanAccept(entry *EntryWrap) bool
}
