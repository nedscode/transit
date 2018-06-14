package inboxes

import (
	"context"
	"time"
)

func updateableTimer(ctx context.Context, avail chan<- *struct{}) chan<- time.Time {
	set := make(chan time.Time)

	zero := time.Time{}
	go func() {
		var c <-chan time.Time
		t := zero
		n := 0

		tock := time.NewTicker(5 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return

			case s := <-set:
				now := time.Now()
				if s.After(now) && (t.IsZero() || s.Before(t)) {
					dur := s.Sub(now)
					c = time.After(dur)
					t = s
					n = 0
				}

			case <-c:
				select {
				case avail <- nil:
				default:
				}
				t = zero
				n = 0

			case <-tock.C:
				n++
				if n >= 10 {
					select {
					case avail <- nil:
					default:
					}
					n = 0
				}
			}
		}
	}()

	return set
}
