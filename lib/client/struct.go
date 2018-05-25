package transit

import (
	pb "github.com/nedscode/transit/proto"
)

// You shouldn't have to include the protobuf just to use it's types if you're already including the client lib.
// This file includes the aliases you will need so you don't have to.

// Pong is an alias to the protobuf struct of the same name.
type Pong = pb.Pong

// Publication is an alias to the protobuf struct of the same name.
type Publication = pb.Publication

// Entry is an alias to the protobuf struct of the same name.
type Entry = pb.Entry

// Subscription is an alias to the protobuf struct of the same name.
type Subscription = pb.Subscription

// Notification is an alias to the protobuf struct of the same name.
type Notification = pb.Notification

// Sub is an alias to the protobuf struct of the same name.
type Sub = pb.Sub

// Acked is an alias to the protobuf struct of the same name.
type Acked = pb.Acked

// Success is an alias to the protobuf struct of the same name.
type Success = pb.Success

// String is an alias to the protobuf struct of the same name.
type String = pb.String

// Strings is an alias to the protobuf struct of the same name.
type Strings = pb.Strings

// StringMap is an alias to the protobuf struct of the same name.
type StringMap = pb.StringMap

// Void is an alias to the protobuf struct of the same name.
type Void = pb.Void

// DistributionStrategy is an alias to the protobuf type of the same name.
type DistributionStrategy = pb.DistributionStrategy

// DeliveryStrategy is an alias to the protobuf type of the same name.
type DeliveryStrategy = pb.DeliveryStrategy

const (
	// Concurrent messages are not treated specially.
	Concurrent DeliveryStrategy = pb.DeliveryStrategy_Concurrent
	// Drop means if there's currently one waiting, don't bother with this one and drop it.
	Drop DeliveryStrategy = pb.DeliveryStrategy_Drop
	// Replace means if there's currently one waiting, replace it with this one.
	Replace DeliveryStrategy = pb.DeliveryStrategy_Replace
	// Ignore means if there's currently one waiting, delete it and place this one at the back of the queue.
	Ignore DeliveryStrategy = pb.DeliveryStrategy_Ignore
	// Serial means a message will not begin processing a duplicate identity message at the same time as another.
	Serial DeliveryStrategy = pb.DeliveryStrategy_Serial
)

const (
	// Arbitrary distribution means that any processor may get any message.
	Arbitrary DistributionStrategy = pb.DistributionStrategy_Arbitrary
	// Requested distribution limits messages to processors that have requested type of the specific lot.
	Requested DistributionStrategy = pb.DistributionStrategy_Requested
	// Assigned distribution will assign unrequested lots to processors based on load.
	Assigned DistributionStrategy = pb.DistributionStrategy_Assigned
)
