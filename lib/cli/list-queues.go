package cli

import (
	"time"

	"google.golang.org/grpc"
	"github.com/norganna/style"

	"github.com/nedscode/transit/proto"
	"github.com/nedscode/transit/lib/inboxes"
)

func init() {
	addHelp(queuesGroup, "list-queues", "List the queues from the leader.")
}

func (c *Config) listQueues() error {
	style.Println("‹hc:Queues:›\n")
	style.Printlnf("  ‹dh:5:|%s›  ‹dh:20:|%s›  ‹dh:20:|%s›  ‹dh:12:|%s›  ‹dh:12:|%s›",
		"Num", "Prefix", "Group", "Distrib", "Delivery")

	ctx, cancel := c.timeout()
	defer cancel()

	_, key := c.getPeersKey()
	ret, err := c.node.Dump(
		ctx,
		&transit.Void{},
		grpc.PerRPCCredentials(&transit.TokenCredentials{
			Token: key,
		}),
	)
	if err != nil {
		return err
	}

	boxes := boxSlice(ret.Boxes)
	items := ret.Items

	itemQueues := map[uint64][][3]string{}

	boxes.Sort()
	for _, box := range boxes {
		style.Printlnf(
			"  ‹b:%5d›  %-20s  %-20s  %-12s  %-12s",
			len(box.States),
			box.Prefix,
			box.Group,
			box.Delivery.String(),
			box.Distribution.String(),
		)
		if len(box.States) > 0 {
			for id, state := range box.States {
				state := inboxes.State(state)
				sig := [3]string{box.Prefix, box.Group, state.String()}
				itemQueues[id] = append(itemQueues[id], sig)
			}
		}
	}

	style.Println("\n‹hc:Entries:›\n")
	style.Printlnf(
		"  ‹dh:8:|%s› ‹dh:18:|%s› ‹dh:18:|%s› ‹dh:9:|%s› ‹dh:6:|%s› ‹dh:6:|%s› ‹dh:10:|%s› ‹dh:3:|%s› ‹dh:3:|%s›",
		"ID", "Topic", "Identity", "Lot", "Len", "Age", "Concern", "N", "E",
	)

	var ids uint64Slice

	for id := range items {
		ids = append(ids, id)
	}
	ids.Sort()

	for _, id := range ids {
		boxItem := items[id]
		notified := "No"
		if boxItem.Notified {
			notified = "Yes"
		}
		expired := "No"
		if boxItem.Expired {
			expired = "Yes"
		}

		inserted := time.Unix(int64(boxItem.Inserted / 1000), int64(boxItem.Inserted % 1000 * uint64(time.Millisecond)))

		delta := float64(time.Now().Sub(inserted)) / float64(time.Second)
		unit := "s"
		if delta > 60 {
			delta = delta / 60
			unit = "m"
			if delta > 60 {
				delta = delta / 60
				unit = "h"
				if delta > 24 {
					delta = delta / 60
					unit = "d"
				}
			}
		}
		age := style.Sprintf("%0.1f%s", delta, unit)

		style.Printlnf(
			"  ‹b:%8d› %-18s %-18s %-9s %6d %-6s %-10s %-3s %-3s",
			id,
			boxItem.Entry.Topic,
			boxItem.Entry.Identity,
			boxItem.Entry.Lot,
			len(boxItem.Entry.Message.Value),
			age,
			boxItem.Concern,
			notified,
			expired,
		)
		q := itemQueues[id]
		n := len(q)
		li := "li"
		for i, queue := range q {
			if i == n-1 {
				li = "ll"
			}
			style.Printlnf(
				"  ‹sp:8›  ‹%s: %s » %s›  <%s>",
				li,
				queue[0],
				queue[1],
				queue[2],
			)
		}
	}

	return nil
}

func (c *Config) listQueuesCommand(args []string) (cb afterFunc, err error) {
	cb = c.withLeader(c.listQueues)
	return
}
