package inboxes

// Subscriber is an interface that defines a wildSubscriber that can accept an entry.
type Subscriber interface {
	CanAccept(entry *EntryWrap) bool
}

type acceptAnySubscriber struct {
}

func (a *acceptAnySubscriber) CanAccept(*EntryWrap) bool {
	return true
}

// AcceptAny is a simple subscriber that will accept any entry.
var AcceptAny Subscriber = (*acceptAnySubscriber)(nil)
