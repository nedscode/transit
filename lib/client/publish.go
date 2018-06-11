package transit

import (
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
)

// PublishOpt is a function that sets options for a Publication.
type PublishOpt func(p *Publication) error

// Lot sets the lot string in a publication.
func (c *Client) Lot(lot string) PublishOpt {
	return func(p *Publication) error {
		p.Entry.Lot = lot
		return nil
	}
}

// NotBefore sets the not before time in a publication.
func (c *Client) NotBefore(t time.Time) PublishOpt {
	return func(p *Publication) error {
		p.Entry.NotBefore = uint64(t.UnixNano() / int64(time.Millisecond))
		return nil
	}
}

// NotAfter sets the not after time in a publication.
func (c *Client) NotAfter(t time.Time) PublishOpt {
	return func(p *Publication) error {
		p.Entry.NotAfter = uint64(t.UnixNano() / int64(time.Millisecond))
		return nil
	}
}

// Meta sets a meta key/value in a publication.
func (c *Client) Meta(key, value string) PublishOpt {
	return func(p *Publication) error {
		if p.Entry.Meta == nil {
			p.Entry.Meta = map[string]string{}
		}
		p.Entry.Meta[key] = value
		return nil
	}
}

// AnyMessage allows setting the message directly to an Any in a publication.
func (c *Client) AnyMessage(any *any.Any) PublishOpt {
	return func(p *Publication) error {
		p.Entry.Message = any
		return nil
	}
}

// Concern sets the write concern for a publication.
func (c *Client) Concern(concern WriteConcern) PublishOpt {
	return func(p *Publication) error {
		p.Concern = concern
		return nil
	}
}

// Timeout sets the maximum time to wait for the desired write concern by a publication.
func (c *Client) Timeout(timeout time.Duration) PublishOpt {
	return func(p *Publication) error {
		p.Timeout = uint64(timeout / time.Millisecond)
		return nil
	}
}

// Publish enters an Entry into the publication queue.
func (c *Client) Publish(topic, identity string, message proto.Message, opts ...PublishOpt) *Pub {
	p := &Publication{
		Entry: &Entry{
			Topic:    topic,
			Identity: identity,
		},
	}

	if message != nil {
		any, err := ptypes.MarshalAny(message)
		if err != nil {
			return &Pub{
				err: err,
			}
		}

		p.Entry.Message = any
	}

	for _, opt := range opts {
		err := opt(p)
		if err != nil {
			return &Pub{
				p:   p,
				err: err,
			}
		}
	}

	return c.enqueue(p)
}
