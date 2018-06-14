package inboxes

// State is an internal representation of the state of an Entry within an Inbox.
type State uint8

const (
	// Ready indicates the item is ready to be delivered to a recipient.
	Ready State = 1 << iota
	// Sent indicates the item has been sent and is waiting to be acked.
	Sent
	// Acked indicates that the item has been processed and can be removed.
	Acked
)

func (s State) String() string {
	switch s {
	case Ready:
		return "Ready"
	case Sent:
		return "Sent"
	case Acked:
		return "Acked"
	}
	return "Novel"
}