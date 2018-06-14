package inboxes

import (
	"time"

	"github.com/nedscode/transit/proto"
)

// Subscriber is an interface that defines a wildSubscriber that can accept an entry.
type Subscriber interface {
	ID() string
	CanAccept(strategy *Strategy, entry *EntryWrap) (tril, time.Time)
	HasAllotment(lot string) (has tril, fixed bool)
	SetAllotment(lot string, set tril)
	Distribution() transit.DistributionStrategy
}

type acceptAnySubscriber struct {
}

func (a *acceptAnySubscriber) ID() string {
	return "any"
}

func (a *acceptAnySubscriber) CanAccept(*Strategy, *EntryWrap) (tril, time.Time) {
	return yes, time.Time{}
}

func (a *acceptAnySubscriber) HasAllotment(lot string) (has tril, fixed bool) {
	return yes, false
}

func (a *acceptAnySubscriber) SetAllotment(lot string, set tril) {
}

func (a *acceptAnySubscriber) Distribution() transit.DistributionStrategy {
	return transit.DistributionStrategy_Arbitrary
}

// AcceptAny is a simple subscriber that will accept any entry.
var AcceptAny Subscriber = (*acceptAnySubscriber)(nil)
