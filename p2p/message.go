package p2p

const (
	// IncomingMessage is a protocol flag (byte) used to indicate that the
	// following data on the wire is a standard, structured message.
	IncomingMessage = 0x1
	// IncomingStream is a protocol flag (byte) used to indicate that the
	// following data on the wire is a raw data stream (e.g., a file).
	// This signals the receiver to handle the connection differently.
	IncomingStream = 0x2
)

// RPC represents a message that is sent over the transport layer between peers.
// It contains the sender's address, the message payload, and a flag for stream handling.
type RPC struct {
	// From is the network address of the peer that sent this message.
	From string
	// Payload is the actual content of the message.
	Payload []byte
	// Stream is a boolean flag that is true only when the message is a notification
	// for an incoming raw data stream. In this case, the Payload will be empty.
	Stream bool
}
