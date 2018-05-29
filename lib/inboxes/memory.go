package inboxes

import (
	"fmt"
	"sync"

	"github.com/nedscode/transit/proto"
)

const (
	// DefaultMaxInboxCapacity is used if a maximum capacity isn't specified.
	DefaultMaxInboxCapacity = 10000000
)

// MemoryInboxFactory is an in-memory inbox generator function.
func MemoryInboxFactory(capacity, max uint64) InboxFactory {
	if max == 0 {
		max = DefaultMaxInboxCapacity
	}
	if max < capacity {
		max = capacity
	}
	return func() Inbox {
		return NewMemoryInbox(capacity, max)
	}
}

type inboxState uint8

const (
	// Ready indicates the item is ready to be delivered to a recipient.
	Ready inboxState = 1 << iota
	// Sent indicates the item has been sent and is waiting to be acked.
	Sent
	// Acked indicates that the item has been processed and can be removed.
	Acked
)

// NewMemoryInbox creates a new MemoryInbox.
func NewMemoryInbox(capacity, max uint64) *MemoryInbox {
	return &MemoryInbox{
		items:    make([]*transit.Entry, capacity),
		states:   make([]inboxState, capacity),
		capacity: capacity,
		step:     capacity / 2,
		max:      max,
	}
}

// MemoryInbox is a memory based inbox for super high speed throughput.
type MemoryInbox struct {
	// Circular list of items, auto grows `capacity` (by `step`) to `max` capacity.
	items  []*transit.Entry
	states []inboxState

	mu       sync.Mutex
	capacity uint64
	max      uint64
	step     uint64
	head     uint64
	tail     uint64
	alive    uint64
}

func (i *MemoryInbox) grow() bool {
	// As part of growing, we need to choose between making the queue larger or compacting the existing items.
	// To help with this, we have an alive counter, if the number of alive entries is < 0.5x the capacity, it's worth
	// compacting the queue rather than expanding it.

	if i.alive == 0 {
		i.head = 0
		i.tail = 0
		return true
	}

	if i.alive < i.capacity/2 {
		// Perform compaction instead
		return i.compact()
	}

	head := i.head
	tail := i.tail
	cap := i.capacity

	bufCap := cap + i.step
	if bufCap > i.max {
		bufCap = i.max
	}

	if bufCap <= cap {
		// Can't resize smaller
		return false
	}

	itemsBuf := make([]*transit.Entry, bufCap)
	statesBuf := make([]inboxState, bufCap)

	if head == tail {
		// Zero length list requires no copying
		i.head = 0
		i.tail = 0
	} else if head < tail {
		// Simple copy (head->tail)
		copy(itemsBuf, i.items[head:tail])
		copy(statesBuf, i.states[head:tail])
		i.head = 0
		i.tail = tail - head
	} else {
		// Wraps around the end (head->cap + 0->tail)
		copy(itemsBuf, i.items[head:cap])
		copy(statesBuf, i.states[head:cap])
		copy(itemsBuf[cap-head:bufCap], i.items[0:tail])
		copy(statesBuf[cap-head:bufCap], i.states[0:tail])
		i.head = 0
		i.tail = cap - head + tail
	}

	i.items = itemsBuf
	return true
}

func (i *MemoryInbox) compact() bool {
	n := i.alive
	itemsBuf := make([]*transit.Entry, n)
	stateBuf := make([]inboxState, n)

	pos := uint64(0)
	i.iter(func(e *transit.Entry, s inboxState) bool {
		if pos >= n {
			// This should never happen... if it does, someone's messed up.
			fmt.Printf("Items: %#v\n", i.items)
			fmt.Printf("Alive: %d\n", i.alive)
			fmt.Printf("Pos:   %d\n", pos)
			panic("alive count is inconsistent while compacting inbox")
		}

		if e != nil && s != Acked {
			itemsBuf[pos] = e
			stateBuf[pos] = s
			pos++
		}
		return false
	})

	copy(i.items, itemsBuf)
	copy(i.states, stateBuf)
	i.head = 0
	i.tail = pos
	return true
}

// Add inserts the entry into the inbox, growing it if required, returns false if the inbox is at maximum capacity.
func (i *MemoryInbox) Add(entry *transit.Entry) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	h := i.head
	t := i.tail

	next := (t + 1) % i.capacity
	if next == h {
		// Capacity reached, need to grow.
		if !i.grow() {
			return false
		}

		t = i.tail
	}

	i.items[t] = entry
	i.states[t] = Ready
	i.alive++

	i.tail = (t + 1) % i.capacity
	return true
}

type iterSelectorFunc func(e *transit.Entry, s inboxState) bool
type itemSelectorFunc func(e *transit.Entry) bool

// iterates over items in order until selector returns true, then returns the selected entry.
func (i *MemoryInbox) iter(cb iterSelectorFunc) (j uint64, e *transit.Entry, s *inboxState) {
	for j = i.head; j < i.tail; j = (j + 1) % i.capacity {
		e = i.items[j]
		s = &i.states[j]
		if cb(e, *s) {
			return
		}
	}
	return 0, nil, nil
}

// takes an item selector function and calls it for any ready items
func readyState(cb itemSelectorFunc) iterSelectorFunc {
	return func(e *transit.Entry, s inboxState) bool {
		if s == Ready {
			return cb(e)
		}
		return false
	}
}

// Next gets the next unsent item from the current list.
func (i *MemoryInbox) Next(sub Subscriber) *transit.Entry {
	i.mu.Lock()
	defer i.mu.Unlock()

	_, e, s := i.iter(readyState(sub.CanAccept))
	if s != nil {
		*s = Sent
	}

	return e
}

// Ack marks the given item as completed and allows it to be eventually removed from the inbox.
func (i *MemoryInbox) Ack(id uint64) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	adjacent := true
	advance := uint64(0)
	_, _, s := i.iter(func(e *transit.Entry, s inboxState) bool {
		if e.ID == id {
			if adjacent {
				advance++
			}
			return true // Stop processing
		}
		if adjacent {
			if s == Acked {
				advance++
			} else {
				adjacent = false
			}
		}
		return false
	})

	if s != nil {
		*s = Acked
		i.alive--
	}

	if advance > 0 {
		i.head = (i.head + advance) % i.capacity
	}

	return s != nil
}