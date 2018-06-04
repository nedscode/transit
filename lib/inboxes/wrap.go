package inboxes

import "github.com/nedscode/transit/proto"

// EntryWrap wraps an Entry for extra internal fields.
type EntryWrap struct {
	*transit.Entry

	CountInboxes uint64
	CountAcked   uint64
	Notified     bool
	Concern      transit.Concern
	Updated      chan transit.Concern
}

// SetConcern updates the current concern level of an EntryWrap and publishes an update notification if necessary.
func (e *EntryWrap) SetConcern(c transit.Concern) {
	if e.Concern == c {
		return
	}

	e.Concern = c
	if e.Updated != nil {
		select {
		case e.Updated <- c:
		default:
		}
	}
}

// Wrap takes an Entry and wraps it into an EntryWrap.
func Wrap(entry *transit.Entry) *EntryWrap {
	return &EntryWrap{
		Entry: entry,
	}
}
