# Inboxes

In Transit, the inbox is the central piece of the puzzle that keeps track of and delivers incoming items to subscribers.

An `Inbox` is created for each subscribed prefix (topic) and group.

When a published `Entry` is added using the `Inboxes` `Add` method, the entry's `Topic:Instance` is compared against
each subscription prefix, and added to each group's `Inbox` for delivery.

Transit comes with a single type of inbox built in, that of the `MemoryInbox`. However you can feel free to build your
own derivative application and provide your own `Inbox` implementation, for example to incorporate file or database
backed inboxes.

## Usage

```go
inboxes := New(nil)

inbox := inboxes.Inbox("foo.bar", "my-group", nil)

inboxes.Add(&Entry{
	Topic: "foo.bar.baz", 
	Instance: "123",
})

entry := inbox.Next()
if entry != nil {
	inbox.Ack(entry.ID)
}
```