package inboxes

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/norganna/logeric"
	"github.com/norganna/style"

	"github.com/nedscode/transit/proto"
)

const (
	// DefaultMaxInboxCapacity is used if a maximum capacity isn't specified.
	DefaultMaxInboxCapacity = 10000000
)

// MemoryInboxFactory is an in-memory inbox generator function.
func MemoryInboxFactory(logger logeric.FieldLogger, capacity, max uint64) InboxFactory {
	if max == 0 {
		max = DefaultMaxInboxCapacity
	}
	if max < capacity {
		max = capacity
	}
	return func(ctx context.Context) Inbox {
		return NewMemoryInbox(ctx, logger, capacity, max)
	}
}

// NewMemoryInbox creates a new MemoryInbox.
func NewMemoryInbox(ctx context.Context, logger logeric.FieldLogger, capacity, max uint64) *MemoryInbox {
	logger = logger.WithField("prefix", "memory-inbox")

	ctx, cancel := context.WithCancel(ctx)

	box := &MemoryInbox{
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
		items:    make([]*EntryWrap, capacity),
		states:   make([]State, capacity),
		changes:  make([]chan bool, capacity),
		capacity: capacity,
		step:     capacity / 2,
		max:      max,
		avail:    make(chan *struct{}),
		strategy: &Strategy{
			Distribution: transit.DistributionStrategy_Arbitrary,
			Delivery:     transit.DeliveryStrategy_Concurrent,
		},
	}
	box.setTime = updateableTimer(ctx, box.avail)

	return box
}

// MemoryInbox is a memory based inbox for super high speed throughput.
type MemoryInbox struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger logeric.FieldLogger

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
	counter  uint64

	strategy *Strategy

	nextTime time.Time
	setTime  chan<- time.Time

	avail chan *struct{}
}

var _ Inbox = (*MemoryInbox)(nil)

var debugMemNext = os.Getenv("TRANSIT_DEBUG_INBOX_NEXT") != ""
var debugMemAlloc = os.Getenv("TRANSIT_DEBUG_INBOX_ALLOC") != ""

// Add inserts the entry into the inbox, growing it if required, returns false if the inbox is at maximum capacity.
func (i *MemoryInbox) Add(entry *EntryWrap) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	if debugMemAlloc {
		style.Printlnf("Adding ‹b:@%d› (head = ‹i:%d›, tail = ‹i:%d›, cap = ‹i:%d›)", i.counter, i.head, i.tail, i.capacity)
	}

	h := i.head
	t := i.tail

	next := (t + 1) % i.capacity
	if next == h {
		if debugMemAlloc {
			style.Printlnf("  ‹li› at capacity")
		}
		// Capacity reached, need to grow.
		if !i.grow() {
			if debugMemAlloc {
				style.Printlnf("  ‹ll› ‹ec:failed to grow›")
			}
			return false
		}

		t = i.tail
	}

	i.items[t] = entry
	i.states[t] = Ready
	i.changes[t] = make(chan bool)
	i.alive++
	entry.CountInboxes++

	i.tail = (t + 1) % i.capacity
	if debugMemAlloc {
		style.Printlnf("  ‹ll› added item at ‹i:%d›", t)
	}

	select {
	case i.avail <- nil:
	default:
	}

	return true
}

// Next gets the next unsent item from the current list.
func (i *MemoryInbox) Next(ctx context.Context, sub Subscriber) (*EntryWrap, <-chan bool) {
	for {
		if debugMemNext {
			style.Printlnf("  ‹i:%10s› Attempting next for subscriber ", sub.ID())
		}
		e, c := func() (*EntryWrap, <-chan bool) {
			i.mu.Lock()
			defer i.mu.Unlock()

			var minTime time.Time
			if debugMemNext {
				style.Printlnf("  ‹i:%10s›  ‹li› queue looks like: head ‹i:%d›, tail ‹i:%d›,  cap ‹i:%d›", sub.ID(), i.head, i.tail, i.capacity)
			}
			next, possible := i.iterate(func(t *Strategy, e *EntryWrap, s State) (can tril) {
				if debugMemNext {
					style.Printlnf("  ‹i:%10s›  ‹li› processing %d", sub.ID(), e.ID)
				}

				if e.Expired {
					if debugMemNext {
						style.Printlnf("  ‹i:%10s›  ‹li›  ‹ll› item is expired", sub.ID())
					}
					return no
				}

				if s != Ready {
					if debugMemNext {
						style.Printlnf("  ‹i:%10s›  ‹li›  ‹ll› not ready", sub.ID())
					}
					return no
				}

				var entryTime time.Time

				can = yes

				if e.NotBefore+e.NotAfter > 0 {
					now := time.Now()
					if e.NotBefore > 0 {
						secs := int64(e.NotBefore / 1000)
						nanos := int64(e.NotBefore%1000) * int64(time.Millisecond)
						unix := time.Unix(secs, nanos)
						if now.Before(unix) {
							// We can't receive this yet because the message is too young.
							can = no
							entryTime = unix
							if debugMemNext {
								style.Printlnf("  ‹i:%10s›  ‹li›  ‹li› too early", sub.ID())
							}
						}
					}
					if e.NotAfter > 0 {
						secs := int64(e.NotAfter / 1000)
						nanos := int64(e.NotAfter%1000) * int64(time.Millisecond)
						if now.After(time.Unix(secs, nanos)) {
							// This message has passed it's expiry time (and will need to be collected).
							e.Expired = true
							can = no
							if debugMemNext {
								style.Printlnf("  ‹i:%10s›  ‹li›  ‹li› ‹ec:too late!› item is expired", sub.ID())
							}
						}
					}
				}

				subCan, subTime := sub.CanAccept(t, e)
				if debugMemNext && subCan == no {
					style.Printlnf("  ‹i:%10s›  ‹li›  ‹li› subscriber doesn't want it", sub.ID())
				}
				if subCan == no && !subTime.IsZero() && subTime.After(entryTime) {
					entryTime = subTime
				}

				can = can.And(subCan)

				if !entryTime.IsZero() && (minTime.IsZero() || entryTime.Before(minTime)) {
					minTime = entryTime
				}

				if debugMemNext {
					var delay int64
					if !entryTime.IsZero() {
						delay = int64(time.Until(entryTime) / time.Millisecond)
					}
					match := "no"
					switch can {
					case maybe:
						match = "possible"
					case yes:
						match = "found"
					}

					style.Printlnf("  ‹i:%10s›  ‹li›  ‹ll› %s match (available in: %dms)", sub.ID(), match, delay)
				}

				return
			})

			if !minTime.IsZero() {
				i.setTime <- minTime
				if debugMemNext {
					delay := uint(time.Until(minTime) / time.Millisecond)
					style.Printlnf("  ‹i:%10s›  ‹ll› min time: %dms", sub.ID(), delay)
				}
			}

			if len(possible) > 0 {
				if debugMemNext {
					style.Printlnf("  ‹i:%10s›  ‹li› checking possibles", sub.ID())
				}
				if i.strategy.Distribution != transit.DistributionStrategy_Requested {
					next = possible[0]
					if i.strategy.Distribution == transit.DistributionStrategy_Assigned {
						sub.SetAllotment(next.wrap.Lot, yes)
					}
					if debugMemNext {
						style.Printlnf("  ‹i:%10s›  ‹li›  ‹ll› using possible %d", sub.ID(), next.wrap.ID)
					}
				}
			}

			if debugMemNext {
				style.Printlnf("  ‹i:%10s›  ‹li› finished looking", sub.ID())
			}

			if next != nil {
				if debugMemNext {
					style.Printlnf("  ‹i:%10s›  ‹ll› found next ‹i:%d›", sub.ID(), next.wrap.ID)
				}
				*next.state = Sent
				return next.wrap, next.change
			}

			if debugMemNext {
				style.Printlnf("  ‹i:%10s›  ‹ll› ‹ec:next not found›", sub.ID())
			}
			return nil, nil
		}()

		if e != nil {
			// Drain the changed channel first, in case the previous subscriber didn't.
			select {
			case <-c:
			default:
			}

			return e, c
		}

		// Wait for an item to be made available.
		select {
		case <-i.ctx.Done():
			return nil, nil
		case <-ctx.Done():
			return nil, nil
		case <-i.avail:
		}
	}
}

// Ack updates the given item's completion status and allows it to be eventually removed from the inbox.
func (i *MemoryInbox) Ack(a *transit.Acknowledgement) *EntryWrap {
	i.mu.Lock()
	defer i.mu.Unlock()

	adjacent := true
	advance := uint64(0)
	found, _ := i.iterate(func(_ *Strategy, e *EntryWrap, s State) tril {
		if e.ID == a.Sub.ID {
			return yes // We've found the requested entry, stop processing.
		}
		if adjacent {
			if e.Expired || s == Acked {
				advance++
			} else {
				adjacent = false
			}
		}
		return no
	})

	// Only a Sent item may be ACKed.
	if found != nil && found.state != nil && *found.state == Sent {
		var notify bool
		if a.Ack {
			// Mark it as ACKed.
			*found.state = Acked
			i.alive--
			found.wrap.CountAcked++
			if adjacent {
				advance++
			}
			notify = true
		} else {
			// Needs to be marked as ready so it can be redelivered to another subscriber.
			*found.state = Ready
			i.avail <- nil
			notify = true
		}

		if notify {
			// Notify that our state has changed, if anyone is listening
			select {
			case <-found.change:
			default:
			}
			select {
			case found.change <- !a.Close:
			default:
			}
		}
	}

	if advance > 0 {
		i.counter += advance
		i.head = (i.head + advance) % i.capacity
		e := i.items[i.head]
		id := uint64(0)
		if e != nil {
			id = e.ID
		}

		if debugMemAlloc {
			style.Printlnf("Advancing ‹b:%d› -> ‹b:%d› #%d (head = ‹i:%d›, tail = ‹i:%d›, cap = ‹i:%d›)", advance, i.counter, id, i.head, i.tail, i.capacity)
		}
	}

	if found == nil || found.state == nil {
		return nil
	}
	return found.wrap
}

// All returns all the entries in this inbox.
func (i *MemoryInbox) All() (all []InboxItem) {
	i.iterate(func(_ *Strategy, e *EntryWrap, s State) tril {
		all = append(all, InboxItem{
			EntryWrap: e,
			State:     s,
		})
		return no
	})
	return
}

// Strategy updates (optionally) the current strategy and returns the new strategy and whether it was updated.
func (i *MemoryInbox) Strategy(set *Strategy) (Strategy, bool) {
	return i.strategy.Set(set)
}

type iterSelectorFunc func(*Strategy, *EntryWrap, State) tril
type itemSelectorFunc func(*Strategy, *EntryWrap) tril

type iterItem struct {
	index  uint64
	wrap   *EntryWrap
	state  *State
	change chan bool
}

// iterate over items in order until selector returns yes, then returns the selected entry.
func (i *MemoryInbox) iterate(cb iterSelectorFunc) (found *iterItem, possible []*iterItem) {
	for j := i.head; j < i.tail || i.tail < i.head && j >= i.head; j = (j + 1) % i.capacity {
		e := i.items[j]
		s := &i.states[j]
		c := i.changes[j]
		t := cb(i.strategy, e, *s)
		if t == yes {
			found = &iterItem{
				index:  j,
				wrap:   e,
				state:  s,
				change: c,
			}
			return
		} else if t == maybe {
			possible = append(possible, &iterItem{
				index:  j,
				wrap:   e,
				state:  s,
				change: c,
			})
		}
	}
	return
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

	if debugMemAlloc {
		style.Printlnf("  ‹li› growing ‹b:@%d› (head = ‹i:%d›, tail = ‹i:%d›, cap = ‹i:%d›)", i.counter, i.head, i.tail, i.capacity)
	}

	head := i.head
	tail := i.tail
	cap := i.capacity

	bufCap := cap + i.step
	if bufCap > i.max {
		bufCap = i.max
	}

	if debugMemAlloc {
		style.Printlnf("  ‹li›  ‹li› new capacity = %d", bufCap)
	}

	if bufCap <= cap {
		// Can't resize smaller
		if debugMemAlloc {
			style.Printlnf("  ‹li›  ‹ll› ‹ec:cap is not larger than existing cap›")
		}

		return false
	}

	itemsBuf := make([]*EntryWrap, bufCap)
	statesBuf := make([]State, bufCap)
	changesBuf := make([]chan bool, bufCap)

	if head == tail {
		// Zero length list requires no copying
		i.head = 0
		i.tail = 0

		if debugMemAlloc {
			style.Printlnf("  ‹li›  ‹li› head = tail, no copy")
		}
	} else if head < tail {
		// Simple copy (head->tail)
		copy(itemsBuf, i.items[head:tail])
		copy(statesBuf, i.states[head:tail])
		copy(changesBuf, i.changes[head:tail])
		i.head = 0
		i.tail = tail - head

		if debugMemAlloc {
			style.Printlnf("  ‹li›  ‹li› head < tail, straight copy")
		}
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

		if debugMemAlloc {
			style.Printlnf("  ‹li›  ‹li› head > tail, double copy")
		}
	}

	i.items = itemsBuf
	i.states = statesBuf
	i.changes = changesBuf
	i.capacity = bufCap

	if debugMemAlloc {
		style.Printlnf("  ‹li›  ‹ll› copy completed (head = ‹i:%d›, tail = ‹i:%d›, cap = ‹i:%d›)", i.head, i.tail, i.capacity)
	}
	return true
}

func (i *MemoryInbox) compact() bool {
	if debugMemAlloc {
		style.Printlnf("Compacting ‹b:@%d› (head = ‹i:%d›, tail = ‹i:%d›, cap = ‹i:%d›)", i.counter, i.head, i.tail, i.capacity)
	}

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
			i.logger.Warn("Items: %#v\n", i.items)
			i.logger.Warn("Alive: %d\n", i.alive)
			i.logger.Warn("Pos:   %d\n", pos)
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

// takes an item selector function and calls it for any ready items
func readyState(cb itemSelectorFunc) iterSelectorFunc {
	return func(t *Strategy, e *EntryWrap, s State) tril {
		if s == Ready {
			return cb(t, e)
		}
		return no
	}
}
