package p2p

// HandshakeFunc is a function type that defines the logic for a peer-to-peer handshake.
// This allows the transport layer to be configured with custom authentication or
// version-checking logic that runs when a new connection is established.
type HandshakeFunc func(Peer) error

// NOPHandshakeFunc is a "No-Op" (No Operation) handshake function.
// It does nothing and returns nil, effectively disabling the handshake process.
// It serves as a useful default when no special handshake is required.
func NOPHandshakeFunc(Peer) error { return nil }
