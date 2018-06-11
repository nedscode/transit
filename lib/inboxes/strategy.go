package inboxes

import (
	"fmt"

	"github.com/nedscode/transit/proto"
)

// Strategy represents a queue strategy.
type Strategy struct {
	// Distribution is the default distribution strategy for the entire queue.
	Distribution transit.DistributionStrategy
	// Delivery is the delivery strategy for the queue.
	Delivery transit.DeliveryStrategy
}

// Set updates (optionally) the strategy and returns the strategy and whether it was changed.
func (s *Strategy) Set(from *Strategy) (ret Strategy, changed bool) {
	if from != nil {
		if from.Delivery > 0 && s.Delivery != from.Delivery {
			changed = true
			s.Delivery = from.Delivery
		}
		if from.Distribution > 0 && s.Distribution != from.Distribution {
			changed = true
			s.Distribution = from.Distribution
		}
	}

	ret = *s
	return
}

// String returns a stringular representation of the strategy.
func (s *Strategy) String() string {
	return fmt.Sprintf(
		"Delivery: %s, Distribution: %s",
		s.Delivery.String(),
		s.Distribution.String(),
	)
}
