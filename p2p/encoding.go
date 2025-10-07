package p2p

import (
	"encoding/gob"
	"io"
)

// Decoder is an interface for decoding a byte stream from an io.Reader
// into an RPC object. This allows for different serialization formats to be used.
type Decoder interface {
	Decode(io.Reader, *RPC) error
}

// GOBDecoder is an implementation of the Decoder interface that uses the
// standard Go `gob` package for serialization.
type GOBDecoder struct{}

// Decode implements the Decoder interface for GOB.
func (dec GOBDecoder) Decode(r io.Reader, msg *RPC) error {
	return gob.NewDecoder(r).Decode(msg)
}

// DefaultDecoder is a custom decoder for the simple binary protocol used in this system.
// The protocol distinguishes between standard messages and raw data streams.
type DefaultDecoder struct{}

// Decode implements the custom binary protocol.
// The protocol expects the first byte of any transmission to be a flag indicating
// the type of data that follows.
func (dec DefaultDecoder) Decode(r io.Reader, msg *RPC) error {
	// Read the first byte to determine the message type.
	peekBuf := make([]byte, 1)
	if _, err := r.Read(peekBuf); err != nil {
		// If there is an error (like io.EOF), we return nil to gracefully
		// close the connection loop in the transport layer.
		return nil
	}

	// Check if the first byte indicates an incoming raw stream.
	if peekBuf[0] == IncomingStream {
		// If it's a stream, we set the Stream flag on the RPC object to true.
		// The RPC payload will be empty. This signals to the transport's read loop
		// that it should pause and wait for the application to handle the raw stream.
		msg.Stream = true
		return nil
	}

	// If the first byte was not a stream flag, it must be the IncomingMessage flag.
	// We read the rest of the data as the payload of a standard message.
	// Note: This assumes a simple protocol where the message payload follows the prefix byte.
	buf := make([]byte, 1028)
	n, err := r.Read(buf)
	if err != nil {
		return err
	}

	msg.Payload = buf[:n]

	return nil
}
