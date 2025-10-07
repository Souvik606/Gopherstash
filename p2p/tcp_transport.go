package p2p

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

// TCPPeer represents a remote node connected via a TCP connection.
type TCPPeer struct {
	// net.Conn is the underlying TCP connection. Embedding this type allows
	// a TCPPeer to be used wherever a net.Conn is expected.
	net.Conn

	// outbound is a boolean that is true if this peer is the result of a Dial
	// (we initiated the connection) and false if it's from an Accept
	// (the remote node initiated the connection).
	outbound bool

	// wg is a WaitGroup used for a specific purpose: to block and unblock the
	// connection's read loop. This is the core mechanism that allows for handling
	// raw data streams. When a stream is initiated, the read loop calls wg.Add(1)
	// and then wg.Wait(), pausing the loop. The application, after consuming the
	// stream, will call CloseStream(), which calls wg.Done(), unblocking the loop.
	wg *sync.WaitGroup
}

// NewTCPPeer creates and returns a new TCPPeer.
func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		Conn:     conn,
		outbound: outbound,
		wg:       &sync.WaitGroup{},
	}
}

// CloseStream signals that the application has finished processing a raw data stream.
// It calls Done() on the WaitGroup, which unblocks the handleConn read loop.
func (p *TCPPeer) CloseStream() {
	p.wg.Done()
}

// Send is a convenience method to write a byte slice to the peer's connection.
func (p *TCPPeer) Send(b []byte) error {
	_, err := p.Conn.Write(b)
	return err
}

// TCPTransportOpts contains the configuration options for a TCPTransport.
type TCPTransportOpts struct {
	// ListenAddr is the TCP address and port for the transport to listen on.
	ListenAddr string
	// HandshakeFunc is a function used to perform a handshake with a remote peer
	// after a connection is established.
	HandshakeFunc HandshakeFunc
	// Decoder is used to decode incoming data from the network into RPC objects.
	Decoder Decoder
	// OnPeer is an optional callback function that is executed when a new peer
	// connects. It can be used to manage the peer list in the application layer.
	OnPeer func(Peer) error
}

// TCPTransport implements the Transport interface for TCP connections.
type TCPTransport struct {
	TCPTransportOpts
	listener net.Listener
	// rpcch is a channel for communicating decoded RPC messages from the
	// transport layer up to the application layer.
	rpcch chan RPC
}

// NewTCPTransport creates a new TCPTransport with the given options.
func NewTCPTransport(opts TCPTransportOpts) *TCPTransport {
	return &TCPTransport{
		TCPTransportOpts: opts,
		rpcch:            make(chan RPC, 1024),
	}
}

// Addr returns the local listen address of the transport.
func (t *TCPTransport) Addr() string {
	return t.ListenAddr
}

// Consume returns a read-only channel for receiving RPCs from other peers.
// This is the primary way the application layer interacts with the transport.
func (t *TCPTransport) Consume() <-chan RPC {
	return t.rpcch
}

// Close shuts down the transport by closing its TCP listener.
func (t *TCPTransport) Close() error {
	return t.listener.Close()
}

// Dial establishes a TCP connection to the given address.
// Once connected, it spawns a goroutine to handle the new connection.
func (t *TCPTransport) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	go t.handleConn(conn, true)

	return nil
}

// ListenAndAccept starts the TCP listener and begins accepting new connections.
// It spawns a goroutine to handle the accept loop.
func (t *TCPTransport) ListenAndAccept() error {
	var err error

	t.listener, err = net.Listen("tcp", t.ListenAddr)
	if err != nil {
		return err
	}

	go t.startAcceptLoop()

	log.Printf("TCP transport listening on port: %s\n", t.ListenAddr)

	return nil
}

// startAcceptLoop is an infinite loop that accepts incoming TCP connections.
// For each new connection, it spawns a goroutine to handle it.
func (t *TCPTransport) startAcceptLoop() {
	for {
		conn, err := t.listener.Accept()
		// If the listener is closed, net.ErrClosed will be returned. We exit gracefully.
		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			fmt.Printf("TCP accept error: %s\n", err)
		}

		go t.handleConn(conn, false)
	}
}

// handleConn is the main logic for handling a single TCP connection.
// It is run in a separate goroutine for each connected peer.
func (t *TCPTransport) handleConn(conn net.Conn, outbound bool) {
	var err error

	defer func() {
		fmt.Printf("dropping peer connection: %s", err)
		conn.Close()
	}()

	peer := NewTCPPeer(conn, outbound)

	// Perform the handshake if one is configured.
	if err = t.HandshakeFunc(peer); err != nil {
		return
	}

	// If the OnPeer callback is set, execute it with the new peer.
	// This allows the application layer to be notified of new connections.
	if t.OnPeer != nil {
		if err = t.OnPeer(peer); err != nil {
			return
		}
	}

	// Read loop: continuously decode messages from the connection.
	for {
		rpc := RPC{}
		// Use the configured decoder to read the next message from the connection.
		err = t.Decoder.Decode(conn, &rpc)
		if err != nil {
			// An error here (e.g., EOF) means the connection is likely closed.
			return
		}

		rpc.From = conn.RemoteAddr().String()

		// This is the core logic for handling raw data streams.
		if rpc.Stream {
			// 1. If the message is a stream notification, block this read loop.
			peer.wg.Add(1)
			fmt.Printf("[%s] incoming stream, waiting...\n", conn.RemoteAddr())
			// 2. The Wait() call will hang here until the application layer,
			//    which has been notified of the stream by other means, finishes
			//    reading the raw data and calls peer.CloseStream().
			peer.wg.Wait()
			fmt.Printf("[%s] stream closed, resuming read loop\n", conn.RemoteAddr())
			// 3. Once unblocked, continue to the next iteration of the loop
			//    to wait for the next message. Do not send this RPC to the consume channel.
			continue
		}

		// For standard messages, push the decoded RPC onto the consume channel
		// for the application layer to process.
		t.rpcch <- rpc
	}
}
