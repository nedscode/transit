package server

import (
	"sync"
	"time"

	"github.com/nedscode/transit/lib/inboxes"
	"github.com/nedscode/transit/proto"
)

type subscriber struct {
	mu sync.RWMutex

	fixedAllotments   map[string]tril // doesn't need locking as it's only written at subscribe time
	dynamicAllotments map[string]tril // use lock when reading or writing this

	distribution transit.DistributionStrategy

	delay  uint64
	maxAge uint64
}

func newSubscriber(allotments []string) *subscriber {
	s := &subscriber{
		fixedAllotments:   map[string]tril{},
		dynamicAllotments: map[string]tril{},
	}
	for _, lot := range allotments {
		s.fixedAllotments[lot] = yes
	}
	return s
}

func (s *subscriber) SetAllotment(lot string, set tril) {
	if s.dynamicAllotments[lot] == set {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if set == maybe {
		delete(s.dynamicAllotments, lot)
	} else {
		s.dynamicAllotments[lot] = set
	}
}

func (s *subscriber) Distribution() transit.DistributionStrategy {
	return s.distribution
}

func (s *subscriber) HasAllotment(lot string) (has tril, fixed bool) {
	if has = s.fixedAllotments[lot]; has == maybe {
		if len(s.dynamicAllotments) > 0 {
			s.mu.RLock()
			defer s.mu.Unlock()

			has = s.dynamicAllotments[lot]
		}
	} else {
		fixed = true
	}
	return
}

func (s *subscriber) CanAccept(t *inboxes.Strategy, e *inboxes.EntryWrap) (can tril, until time.Time) {
	if t.Distribution != transit.DistributionStrategy_Arbitrary {
		has, fixed := s.HasAllotment(e.Lot)
		if has == no {
			// that's a hard no
			return no, time.Time{}
		}

		if t.Distribution == transit.DistributionStrategy_Requested && (has == maybe || !fixed) {
			// can only deliver requested lots to this one
			return no, time.Time{}
		}
	}

	if s.delay+s.maxAge > 0 {
		now := time.Now()
		unix := e.Inserted.Add(time.Duration(s.delay) * time.Millisecond)
		if s.delay > 0 && now.Before(unix) {
			// We can't receive this yet because the message is too young.
			return no, unix
		}
		if s.maxAge > 0 && now.After(e.Inserted.Add(time.Duration(s.maxAge)*time.Millisecond)) {
			// We can't receive this yet because the message is too young.
			return no, time.Time{}
		}
	}

	return
}
