package inboxes

// SyncMode represents a desired server cluster synchronisation level.
type SyncMode int

const (
	// SyncNone doesn't perform any synchronisation with slave servers, if we switch over, we sync then at that point,
	// otherwise slave servers remain empty and start from scratch.
	SyncNone SyncMode = iota

	// SyncSend sends messages and queues blindly to slave servers, but are not concerned with the slave server being
	// up to date before continuing with our business.
	// It's possible in the event of a hard failure of the master server that the slave servers may be in a slightly
	// inconsistent state.
	SyncSend

	// SyncConfirm syncs messages with slave servers and waits for confirmations before doing anything.
	// This mode is super slow, but is 100% consistent.
	SyncConfirm
)
