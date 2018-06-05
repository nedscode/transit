package inboxes

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nedscode/transit/proto"
	"github.com/sirupsen/logrus"
)

const (
	// DeliveredPrefix is prepended to an entry's topic to notify interested listeners of a message's delivery.
	// Sometimes a subscriber may wish to receive notification that certain messages have been completely delivered,
	// we send out a publication with the following prefix which can be subscribed to to let anyone know that the
	// message has been completely delivered.
	// Note: Publishers can just set their desired write concern to Processed to receive notification that their items
	// have been delivered.
	DeliveredPrefix = "⇒."
)

// Inboxes represent the entirety of the subscriptions in use by the server.
type Inboxes struct {
	ctx context.Context
	mu  sync.RWMutex

	logger logrus.FieldLogger

	// iderator is a monotonically incrementing id generator.
	iderator *iderator
	// boxes is a list of inboxes per group (`map[topic/group]`).
	boxes map[string]*inboxDetail
	// ids is a list of IDs to their inbox, so that when an ID is acked, we can inform the inbox.
	ids map[uint64]string
	// factory is the default factory that is used when creating new inboxes.
	factory InboxFactory
	// syncMode is the mode of syncronisation we use to sync with slave servers.
	syncMode SyncMode

	stats InboxStats
}

// InboxStats contains statistics about the entries placed into inboxes.
type InboxStats struct {
	// Entries is the number of distinct entries inserted into the inboxes.
	Entries uint64

	// Inboxes is the total number of total inserts of entries into inboxes.
	Inboxes uint64

	// Acked is the number of inserted entries that have been acked.
	Acked uint64

	// Finalised is the number of entries that have been finalised (all delivered).
	Finalised uint64
}

// InboxFactory is a function that can return a new Inbox if required.
type InboxFactory func(ctx context.Context) Inbox

// Inbox is a place to accumulate message entries for a group.
type Inbox interface {
	Add(*EntryWrap) bool
	Next(Subscriber) (*EntryWrap, <-chan bool)
	Ack(*transit.Acknowledgement) *EntryWrap
}

type inboxDetail struct {
	Inbox

	topic string
	group string

	created time.Time

	subscribers int
	disconnect  time.Time
}

var _ Inbox = (*inboxDetail)(nil)

// New creates a new set of Inboxes.
func New(ctx context.Context, logger logrus.FieldLogger, factory InboxFactory, mode SyncMode) *Inboxes {
	if factory == nil {
		factory = MemoryInboxFactory(100, 0)
	}

	logger = logger.WithField("prefix", "inboxes")

	return &Inboxes{
		ctx:      ctx,
		logger:   logger,
		iderator: &iderator{},
		factory:  factory,
		boxes:    map[string]*inboxDetail{},
		syncMode: mode,
	}
}

// Inbox will get or create the inbox for the given topic and group, using factory if not nil, else default factory.
func (i *Inboxes) Inbox(topic, group string, factory InboxFactory) (inbox Inbox) {
	// Use the lighter `.getInbox()` first as statistically the inbox is likely to already exist, and getting only
	// requires a read lock.
	b := i.getInbox(topic, group)
	if b == nil {
		b = i.createInbox(topic, group, factory)
	}

	inbox = b
	return
}

// Add inserts an entry into any matching inbox.
func (i *Inboxes) Add(wrap *EntryWrap) {
	wrap.ID = i.iderator.Next()
	wrap.SetConcern(transit.Concern_Received)

	i.mu.RLock()
	defer i.mu.RUnlock()

	fullTopic := wrap.Topic
	if wrap.Identity != "" {
		fullTopic += ":" + wrap.Identity
	}

	added := false
	for _, box := range i.boxes {
		if topicMatch(fullTopic, box.topic) {
			box.Add(wrap)
			added = true
		}
	}

	if !added {
		wrap.SetConcern(transit.Concern_Processed)
		return
	}

	i.stats.Entries++
	runtime.SetFinalizer(wrap, i.processFinal)

	if i.syncMode != SyncNone {
		// First go to Delivered mode...
		wrap.SetConcern(transit.Concern_Delivered)

		/*
			// TODO - sync with slave servers
			if i.syncMode == SyncConfirm {
				// TODO - wait for confirmation from slave servers
			}
		*/
	}

	// Now confirm delivery...
	wrap.SetConcern(transit.Concern_Confirmed)
}

// Ack takes an Acknowledgement and gets the appropriate inbox to process it.
func (i *Inboxes) Ack(ack *transit.Acknowledgement) *EntryWrap {
	if ack.Sub == nil {
		return nil
	}

	box := i.getInbox(ack.Sub.Prefix, ack.Sub.Group)
	if box == nil {
		return nil
	}

	wrap := box.Ack(ack)
	if !wrap.Notified && wrap.CountAcked >= wrap.CountInboxes {
		i.notifyDelivery(wrap)
	}

	return wrap
}

// Cheap method to get a preexisting inbox using only a read lock.
func (i *Inboxes) getInbox(prefix, group string) (inbox *inboxDetail) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", prefix, group)
	if b, ok := i.boxes[key]; ok {
		inbox = b
	}
	return
}

// Method to get or create an inbox, but uses a write lock to do so.
func (i *Inboxes) createInbox(prefix, group string, factory InboxFactory) (inbox *inboxDetail) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if factory == nil {
		factory = i.factory
	}

	key := fmt.Sprintf("%s/%s", prefix, group)
	if b, ok := i.boxes[key]; ok {
		inbox = b
	} else {
		now := time.Now()
		inbox = &inboxDetail{
			Inbox: factory(i.ctx),

			topic: prefix,
			group: group,

			created:    now,
			disconnect: now,
		}

		i.boxes[key] = inbox
	}

	return
}

// Updates the stats when an item is garbage collected.
func (i *Inboxes) processFinal(wrap *EntryWrap) {
	i.stats.Inboxes += wrap.CountInboxes
	i.stats.Acked += wrap.CountAcked
	i.stats.Finalised++

	if !wrap.Notified {
		i.notifyDelivery(wrap)
	}
}

func (i *Inboxes) notifyDelivery(wrap *EntryWrap) {
	wrap.SetConcern(transit.Concern_Processed)

	if !strings.HasPrefix(wrap.Topic, DeliveredPrefix) {
		i.Add(&EntryWrap{
			Entry: &transit.Entry{
				Topic:    DeliveredPrefix + wrap.Topic,
				Identity: wrap.Identity,
				Meta: map[string]string{
					"DeliveredID": strconv.FormatUint(wrap.ID, 10),
				},
			},
		})
	}
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
