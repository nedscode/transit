package inboxes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/nedscode/transit/proto"
	"github.com/sirupsen/logrus/hooks/test"
)

func findIn(in []*EntryWrap, x *EntryWrap) int {
	for i, e := range in {
		if e == x {
			return i
		}
	}
	return -1
}

func getOrder(base []*EntryWrap, from []*EntryWrap) string {
	order := make([]string, len(from))
	for i, x := range from {
		n := findIn(base, x)
		if n >= 0 {
			order[i] = strconv.Itoa(n)
		} else {
			order[i] = "X"
		}
	}
	return "[ " + strings.Join(order, ", ") + " ]"
}

func TestMemoryGrow(t *testing.T) {
	in := NewMemoryInbox(context.Background(), 10, 100)
	base := []*EntryWrap{{}, {}, {}, {}, {}, {}, {}, {}, {}, {}}

	copy(in.items, base)
	oh := uint64(2)
	ot := uint64(9)
	in.head = oh
	in.tail = ot
	in.alive = ot - oh
	in.grow()

	order := getOrder(base, in.items)
	t.Logf("has old array order: %s", getOrder(base, base))
	t.Logf("got new array order: %s", order)

	if order != "[ 2, 3, 4, 5, 6, 7, 8, X, X, X, X, X, X, X, X ]" {
		t.Errorf("unexpected array order")
	}

	if len(in.items) != 15 {
		t.Errorf("expected new length to be inital capacity * 1.5 (15), got %d", len(in.items))
	}

	if in.head != 0 {
		t.Errorf("expected head to start at 0 for newly grown slice, got %d", in.head)
	}

	if in.tail != 7 {
		t.Errorf("expected tail to end at 7 for newly grown slice, got %d", in.tail)
	}

	if in.items[in.head] != base[oh] {
		t.Errorf("expected item at head of new slice to be same as old head %d, got %d",
			findIn(base, base[oh]),
			findIn(base, in.items[in.head]),
		)
	}

	if in.items[in.tail-1] != base[ot-1] {
		t.Errorf("expected item at tail of new slice to be same as old tail %d, got %d",
			findIn(base, base[ot]),
			findIn(base, in.items[in.tail]),
		)
	}
}

func TestMemoryGrowWrap(t *testing.T) {
	in := NewMemoryInbox(context.Background(), 10, 100)
	base := []*EntryWrap{{}, {}, {}, {}, {}, {}, {}, {}, {}, {}}

	copy(in.items, base)
	oh := uint64(5)
	ot := uint64(4)
	in.head = oh
	in.tail = ot
	in.alive = 10 - oh + ot
	in.grow()

	order := getOrder(base, in.items)
	t.Logf("has old array order: %s", getOrder(base, base))
	t.Logf("got new array order: %s", order)

	if order != "[ 5, 6, 7, 8, 9, 0, 1, 2, 3, X, X, X, X, X, X ]" {
		t.Errorf("unexpected array order")
	}

	if len(in.items) != 15 {
		t.Errorf("expected new length to be inital capacity * 1.5 (15), got %d", len(in.items))
	}

	if in.head != 0 {
		t.Errorf("expected head to start at 0 for newly grown slice, got %d", in.head)
	}

	if in.tail != 9 {
		t.Errorf("expected tail to end at 9 for newly grown slice, got %d", in.tail)
	}

	if in.items[in.head] != base[oh] {
		t.Errorf("expected item at head of new slice to be same as old head %d, got %d",
			findIn(base, base[oh]),
			findIn(base, in.items[in.head]),
		)
	}

	if in.items[in.tail-1] != base[ot-1] {
		t.Errorf("expected item at tail of new slice to be same as old tail %d, got %d",
			findIn(base, base[ot]),
			findIn(base, in.items[in.tail]),
		)
	}

}

type wildSubscriber struct{}

func (*wildSubscriber) CanAccept(entry *EntryWrap) bool {
	fmt.Printf("checking accept on %#v\n", entry)
	return true
}

func TestMemoryInbox(t *testing.T) {
	topic := "foo.bar.baz"
	identity := "123"

	logger, _ := test.NewNullLogger()

	in := NewMemoryInbox(context.Background(), 100, 1000)
	ib := New(context.Background(), logger, nil, SyncNone)
	ib.boxes[topic] = &inboxDetail{
		Inbox: in,
		topic: topic,
		group: "test",
	}

	ib.Add(Wrap(&transit.Entry{
		Topic:    topic,
		Identity: identity,
	}))

	entry, _ := in.Next(&wildSubscriber{})

	if entry == nil {
		t.Fatal("Expected to have a next item")
	}

	if entry.Topic != topic {
		t.Errorf("expected topic %q, got %q", topic, entry.Topic)
	}

	if entry.Identity != identity {
		t.Errorf("expected identity %q, got %q", identity, entry.Identity)
	}

	if entry.ID == 0 {
		t.Errorf("expected entry to have assigned id, got %d", entry.ID)
	}
}
