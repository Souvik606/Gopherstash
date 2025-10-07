package p2p

import "net"

// Peer is an interface that represents a remote node in the network.
// It abstracts the underlying connection technology, allowing for different
// transport mechanisms (e.g., TCP, UDP) to be used.
type Peer interface {
	// A Peer is also a network connection, so it embeds the net.Conn interface.
	// This allows for direct reading from and writing to the connection.
	net.Conn

	// Send sends a byte slice to the remote peer.
	// This provides a convenient wrapper around the underlying Conn.Write method.
	Send([]byte) error

	// CloseStream is a special method to signal that the application has finished
	// reading a raw data stream from the connection. This is part of a protocol
	// to handle large file transfers without interfering with the message-based RPCs.
	CloseStream()
}

// Transport is an interface that handles the communication between nodes.
// It's responsible for dialing, listening for connections, and managing peers.
// It abstracts the details of the network protocol (e.g., TCP, UDP).
type Transport interface {
	// Addr returns the local network address the transport is listening on.
	Addr() string

	// Dial connects to a remote address. On success, it should establish
	// a connection and manage the resulting peer.
	Dial(string) error

	// ListenAndAccept starts listening for and accepting incoming connections
	// on the address specified in the transport's configuration.
	ListenAndAccept() error

	// Consume returns a read-only channel of RPC objects. The application layer
	// will read from this channel to receive messages from other peers.
	Consume() <-chan RPC

	// Close shuts down the transport, closing the listener and any active connections.
	Close() error
}
