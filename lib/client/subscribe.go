package transit

import (
	"sync"
	"time"
)

// A SubOption is a function that can modify subscription options within the stream object.
type SubOption func(s *SubStream) error

// A SubHandler is a function that can handle entries from a subscription.
type SubHandler func(entry *Entry) error

// SubStream represents the subscription stream returned when a client subscribes.
type SubStream struct {
	mu      sync.Mutex
	sem     chan bool
	handler SubHandler
	hErr    chan error
	req     *Subscription
}

// Allot sets the allotments that a subscription wishes to receive.
func Allot(allotments ...string) SubOption {
	return func(s *SubStream) error {
		s.req.Allotments = allotments
		return nil
	}
}

// Delay sets a minimum age on potential entries that it wishes to receive.
func Delay(duration time.Duration) SubOption {
	return func(s *SubStream) error {
		s.req.Delay = uint64(duration)
		return nil
	}
}

// MaxAge specifies the oldest entry that the subscriber wishes to receive.
func MaxAge(duration time.Duration) SubOption {
	return func(s *SubStream) error {
		s.req.MaxAge = uint64(duration)
		return nil
	}
}

// Distribution sets (or overwrites) the distribution strategy for this individual subscription.
func Distribution(strategy DistributionStrategy) SubOption {
	return func(s *SubStream) error {
		s.req.Distribution = strategy
		return nil
	}
}

// Delivery sets the delivery strategy to be used for the entire queue.
func Delivery(strategy DeliveryStrategy) SubOption {
	return func(s *SubStream) error {
		s.req.Delivery = strategy
		return nil
	}
}

// Subscribe allows a subscriber to create a Transit subscription to the given topic prefix and group.
// Optionally subscription options may be specified to refine the subscription parameters.
func (c *Client) Subscribe(prefix, group string, opts ...SubOption) (*SubStream, error) {
	sub := &SubStream{
		req: &Subscription{
			Prefix: prefix,
			Group:  group,
		},
	}

	for _, opt := range opts {
		err := opt(sub)
		if err != nil {
			return nil, err
		}
	}

	sc, err := c.client.Subscribe(c.ctx, sub.req)
	if err != nil {
		return nil, err
	}

	sub.sem = make(chan bool, 1)

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-sub.sem:
				s, err := sc.Recv()
				if err != nil {
					if err != nil {
						sub.hErr <- err
						break
					}
				}

				err = sub.handler(s.Entry)
				c.ack(s.Sub)
				if err != nil {
					sub.hErr <- err
					break
				}
				sub.sem <- true
			}
		}
	}()

	return sub, nil
}

// Handle is called on a subscription stream to allow new entries to be processed by the subscriber.
func (s *SubStream) Handle(handler SubHandler) error {
	s.mu.Lock()
	s.hErr = make(chan error, 1)
	s.handler = handler
	s.sem <- true

	defer func() {
		close(s.hErr)
		s.handler = nil
		s.hErr = nil
		s.mu.Unlock()
	}()

	return <-s.hErr
}
