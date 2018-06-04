package transit

import (
	"errors"
	"sync"
	"time"
)

// SubHandlerError is a wrapped error that contains additional information about the severity of the error and the
// ability to continue processing entries.
type SubHandlerError struct {
	err       error
	processed bool
	closing   bool
}

// Error satisfies this becoming an error.
func (f SubHandlerError) Error() string {
	return f.err.Error()
}

// WrappedError returns the actual wrapped error.
func (f SubHandlerError) WrappedError() error {
	return f.err
}

// HandlerError allows you to wrap a normal error as a subscription handler error.
func HandlerError(err error, ack, shutdown bool) SubHandlerError {
	return SubHandlerError{
		err:       err,
		processed: ack,
		closing:   shutdown,
	}
}

// ErrShuttingDown should be returned by your handler if you've successfully processed the message, but don't want
// to process any more. The Entry will be acked and the subscription will be closed.
var ErrShuttingDown = HandlerError(errors.New("shutting down"), true, true)

// ErrFatal should be returned if your subscription suffers a fatal error and the message was not able to be
// processed. The message will be returned to the queue and the subscription will be closed.
var ErrFatal = HandlerError(errors.New("subscription suffered fatal error"), false, true)

// A SubOption is a function that can modify subscription options within the stream object.
type SubOption func(s *SubStream) error

// A SubHandler is a function that can handle entries from a subscription.
// When you're exiting due to shutdown, return ErrShuttingDown from above.
type SubHandler func(entry *Entry) error

// SubStream represents the subscription stream returned when a client subscribes.
type SubStream struct {
	mu         sync.Mutex
	sem        chan bool
	alive      bool
	handler    SubHandler
	handlerErr chan error
	req        *Subscription
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
		alive: true,
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
						sub.handlerErr <- err
						break
					}
				}

				err = sub.handler(s.Entry)
				ack := true
				close := false
				if e, ok := err.(*SubHandlerError); ok {
					ack = e.processed
					close = e.closing
				}

				if close {
					sub.alive = false
				}

				c.ack(s.Sub, ack, close)
				if err != nil {
					sub.handlerErr <- err
					break
				}
				sub.sem <- true
			}
		}
	}()

	return sub, nil
}

// Handle is called on a subscription stream to allow new entries to be processed by the subscriber.
// Any `Entry` sent to handler will be assumed to be completely processed.
// When a handler returns, the `Entry` is automatically acked regardless of error and a new `Entry` retrieved.
// If the handler returns an error, it will be returned to the caller instantly, which should deal with the error
// and then re-call Handle to keep processing entries.
// A special case is if the handler returns an `ErrShuttingDown` which will cause `Entry` to be ACKed without
// retrieving a new entry, and the subscription to end.
func (s *SubStream) Handle(handler SubHandler) error {
	if !s.alive {
		return ErrShuttingDown
	}

	s.mu.Lock()
	if !s.alive {
		return ErrShuttingDown
	}

	s.handlerErr = make(chan error, 1)
	s.handler = handler
	s.sem <- true

	defer func() {
		close(s.handlerErr)
		s.handler = nil
		s.handlerErr = nil
		s.mu.Unlock()
	}()

	return <-s.handlerErr
}
