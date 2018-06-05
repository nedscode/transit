# Transit client library

This is the transit client library that you will typically use to connect, publish and subscribe to the transit server.

It handles all the intricacies of dealing with server connections/reconnecting, making sure you're connected to the master server, allows easy configuration of the subscriptions, publications, ACKing entries correctly, etc.

## Example:

There's a fully featured, runnable example that details the process of connecting to a transit server, subscribing to a topic prefix, publishing an entry, and processing the entry in a subscription handler.

The example is available at [github.com/nedscode/transit/example](https://github.com/nedscode/transit/example).
