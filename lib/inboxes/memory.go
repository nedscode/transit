package inboxes

import (
	"context"
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
	return func(ctx context.Context) Inbox {
		return NewMemoryInbox(ctx, capacity, max)
	}
}

// NewMemoryInbox creates a new MemoryInbox.
func NewMemoryInbox(ctx context.Context, capacity, max uint64) *MemoryInbox {
	return &MemoryInbox{
		ctx:      ctx,
		items:    make([]*EntryWrap, capacity),
		states:   make([]State, capacity),
		changes:  make([]chan bool, capacity),
		capacity: capacity,
		step:     capacity / 2,
		max:      max,
		avail:    make(chan *struct{}),
	}
}

// MemoryInbox is a memory based inbox for super high speed throughput.
type MemoryInbox struct {
	ctx context.Context

	// Circular list of items, auto grows `capacity` (by `step`) to `max` capacity.
	items   []*EntryWrap
	states  []State
	changes []chan bool

	mu       sync.Mutex
	capacity uint64
	max      uint64
	step     uint64
	head     uint64
	tail     uint64
	alive    uint64

	avail chan *struct{}
}

var _ Inbox = (*MemoryInbox)(nil)

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

	itemsBuf := make([]*EntryWrap, bufCap)
	statesBuf := make([]State, bufCap)
	changesBuf := make([]chan bool, bufCap)

	if head == tail {
		// Zero length list requires no copying
		i.head = 0
		i.tail = 0
	} else if head < tail {
		// Simple copy (head->tail)
		copy(itemsBuf, i.items[head:tail])
		copy(statesBuf, i.states[head:tail])
		copy(changesBuf, i.changes[head:tail])
		i.head = 0
		i.tail = tail - head
	} else {
		// Wraps around the end (head->cap + 0->tail)
		copy(itemsBuf, i.items[head:cap])
		copy(statesBuf, i.states[head:cap])
		copy(changesBuf, i.changes[head:cap])
		copy(itemsBuf[cap-head:bufCap], i.items[0:tail])
		copy(statesBuf[cap-head:bufCap], i.states[0:tail])
		copy(changesBuf[cap-head:bufCap], i.changes[0:tail])
		i.head = 0
		i.tail = cap - head + tail
	}

	i.items = itemsBuf
	return true
}

func (i *MemoryInbox) compact() bool {
	n := i.alive
	itemsBuf := make([]*EntryWrap, n)
	stateBuf := make([]State, n)
	changesBuf := make([]chan bool, n)

	pos := uint64(0)
	for j := i.head; j < i.tail; j = (j + 1) % i.capacity {
		e := i.items[j]
		s := i.states[j]
		c := i.changes[j]

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
			changesBuf[pos] = c
			pos++
		}
	}

	copy(i.items, itemsBuf)
	copy(i.states, stateBuf)
	i.head = 0
	i.tail = pos
	return true
}

// Add inserts the entry into the inbox, growing it if required, returns false if the inbox is at maximum capacity.
func (i *MemoryInbox) Add(entry *EntryWrap) bool {
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
	entry.CountInboxes++

	i.tail = (t + 1) % i.capacity

	select {
	case i.avail <- nil:
	default:
	}

	return true
}

type iterSelectorFunc func(*EntryWrap, State) bool
type itemSelectorFunc func(*EntryWrap) bool

// iterates over items in order until selector returns true, then returns the selected entry.
func (i *MemoryInbox) iter(cb iterSelectorFunc) (j uint64, e *EntryWrap, s *State, c chan bool) {
	for j = i.head; j < i.tail; j = (j + 1) % i.capacity {
		e = i.items[j]
		s = &i.states[j]
		c = i.changes[j]
		if cb(e, *s) {
			return
		}
	}
	return 0, nil, nil, nil
}

// takes an item selector function and calls it for any ready items
func readyState(cb itemSelectorFunc) iterSelectorFunc {
	return func(e *EntryWrap, s State) bool {
		if s == Ready {
			return cb(e)
		}
		return false
	}
}

// Next gets the next unsent item from the current list.
func (i *MemoryInbox) Next(sub Subscriber) (*EntryWrap, <-chan bool) {
	for {
		e, c := func() (*EntryWrap, <-chan bool) {
			i.mu.Lock()
			defer i.mu.Unlock()

			_, e, s, c := i.iter(readyState(sub.CanAccept))

			if s != nil {
				*s = Sent
			}

			return e, c
		}()

		if e != nil {
			return e, c
		}

		// Wait for an item to be made available.
		select {
		case <-i.avail:
		case <-i.ctx.Done():
			return nil, nil
		}
	}
}

// Ack updates the given item's completion status and allows it to be eventually removed from the inbox.
func (i *MemoryInbox) Ack(a *transit.Acknowledgement) *EntryWrap {
	i.mu.Lock()
	defer i.mu.Unlock()

	adjacent := true
	advance := uint64(0)
	_, wrap, state, change := i.iter(func(e *EntryWrap, s State) bool {
		if e.ID == a.Sub.ID {
			return true // We've found the requested entry, stop processing.
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

	// Only a Sent item may be ACKed.
	if state != nil && *state == Sent {
		if a.Ack {
			// Mark it as ACKed.
			*state = Acked
			i.alive--
			wrap.CountAcked++
			if adjacent {
				advance++
			}

			// Notify that our state has changed, if anyone is listening
			select {
			case change <- !a.Close:
			default:
			}
		} else {
			// Needs to be marked as ready so it can be redelivered to another subscriber.
			*state = Ready

			// Notify that our state has changed, if anyone is listening
			select {
			case change <- !a.Close:
			default:
			}
		}
	}

	if advance > 0 {
		i.head = (i.head + advance) % i.capacity
	}

	if state == nil {
		return nil
	}
	return wrap
}
