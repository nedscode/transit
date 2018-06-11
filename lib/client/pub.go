package transit

// Pub is what's returned from calling transit.Publish() and represents an async publish call's status.
type Pub struct {
	id      uint64
	concern WriteConcern

	p    *Publication
	err  error
	done chan struct{}
}

// Done checks if the Publication of our Entry has completed (according to the server) yet.
// You can block until it is done by passing wait = true.
func (p *Pub) Done(wait bool) bool {
	if p.err != nil {
		return false
	}

	if wait {
		<-p.done
		return true
	}

	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Err returns the error if any from the publish command.
func (p *Pub) Err() error {
	return p.err
}

// ID is the Entry ID that the server assigned to our published Entry.
func (p *Pub) ID() uint64 {
	return p.id
}

// Success returns true if the desired write concern had been reached.
func (p *Pub) Success() bool {
	return p.concern >= p.p.Concern
}

// Concern returns the actual concern that was reached.
// You should prefer Success() instead, unless you actually need to take remedial action based on what state was
// attained.
func (p *Pub) Concern() WriteConcern {
	return p.concern
}
