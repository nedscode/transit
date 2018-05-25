package inboxes

import (
	"fmt"
	"sync"
	"time"

	"github.com/nedscode/transit/proto"
)

// Inboxes represent the entirety of the subscriptions in use by the server.
type Inboxes struct {
	mu sync.RWMutex

	// iderator is a monotonically incrementing id generator.
	iderator *iderator
	// boxes is a list of inboxes per group (`map[topic/group]`).
	boxes map[string]*inboxDetail
	// ids is a list of IDs to their inbox, so that when an ID is acked, we can inform the inbox.
	ids map[uint64]string
	// factory is the default factory that is used when creating new inboxes.
	factory InboxFactory
}

// InboxFactory is a function that can return a new Inbox if required.
type InboxFactory func() Inbox

// Inbox is a place to accumulate message entries for a group.
type Inbox interface {
	Add(entry *transit.Entry) bool
	Next(sub Subscriber) *transit.Entry
	Ack(id uint64)
}

type inboxDetail struct {
	Inbox

	topic string
	group string

	created time.Time

	subscribers int
	disconnect  time.Time
}

// New creates a new set of Inboxes.
func New(factory InboxFactory) *Inboxes {
	if factory == nil {
		factory = MemoryInboxFactory(100, 0)
	}

	return &Inboxes{
		iderator: &iderator{},
		factory:  factory,
		boxes:    map[string]*inboxDetail{},
	}
}

// Inbox will get or create the inbox for the given topic and group, using factory if not nil, else default factory.
func (i *Inboxes) Inbox(topic, group string, factory InboxFactory) (inbox Inbox) {
	// Use the lighter `.getInbox()` first as statistically the inbox is likely to already exist, and getting only
	// requires a read lock.
	inbox = i.getInbox(topic, group)
	if inbox == nil {
		inbox = i.createInbox(topic, group, factory)
	}
	return
}

// Add inserts an entry into any matching inbox.
func (i *Inboxes) Add(entry *transit.Entry) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	entry.ID = i.iderator.Next()

	fullTopic := entry.Topic
	if entry.Identity != "" {
		fullTopic += ":" + entry.Identity
	}

	for _, box := range i.boxes {
		if topicMatch(fullTopic, box.topic) {
			box.Add(entry)
		}
	}
}

// Cheap method to get a preexisting inbox using only a read lock.
func (i *Inboxes) getInbox(topic, group string) (inbox *inboxDetail) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", topic, group)
	if b, ok := i.boxes[key]; ok {
		inbox = b
	}
	return
}

// Method to get or create an inbox, but uses a write lock to do so.
func (i *Inboxes) createInbox(topic, group string, factory InboxFactory) (inbox *inboxDetail) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if factory == nil {
		factory = i.factory
	}

	key := fmt.Sprintf("%s/%s", topic, group)
	inbox, ok := i.boxes[key]
	if !ok {
		now := time.Now()
		inbox = &inboxDetail{
			Inbox: factory(),

			topic: topic,
			group: group,

			created:    now,
			disconnect: now,
		}

		i.boxes[key] = nil
	}

	return
}

// IDFactory is an ID generator function.
type IDFactory func() uint64

type iderator struct {
	mu sync.Mutex

	id uint64
}

// Next returns a new unused ID for the item.
func (i *iderator) Next() uint64 {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.id++

	if i.id == 0 {
		// To deal with potential wrapping.
		i.id = 1
	}

	return i.id
}
