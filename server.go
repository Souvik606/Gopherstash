package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"souvik606/dfs/p2p"
	"sync"
	"time"
)

// FileServerOpts holds the configuration for a FileServer instance.
type FileServerOpts struct {
	// ID is the unique identifier for this node in the network.
	ID string
	// EncKey is the 256-bit encryption key for securing file transfers.
	EncKey []byte
	// StorageRoot is the root directory on disk for this node's storage.
	StorageRoot string
	// PathTransformFunc defines how filenames are converted to storage paths.
	PathTransformFunc PathTransformFunc
	// Transport is the underlying network layer for peer-to-peer communication.
	Transport p2p.Transport
	// BootstrapNodes is a list of known nodes to connect to upon starting.
	BootstrapNodes []string
}

// FileServer represents a node in the distributed file system.
type FileServer struct {
	FileServerOpts

	peerLock sync.Mutex
	peers    map[string]p2p.Peer

	store  *Store
	quitch chan struct{}
}

// NewFileServer creates and initializes a new FileServer.
func NewFileServer(opts FileServerOpts) *FileServer {
	storeOpts := StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
	}

	// If no ID is provided, generate a unique random one.
	if len(opts.ID) == 0 {
		opts.ID = generateID()
	}

	return &FileServer{
		FileServerOpts: opts,
		store:          NewStore(storeOpts),
		quitch:         make(chan struct{}),
		peers:          make(map[string]p2p.Peer),
	}
}

// broadcast sends a message to all connected peers.
func (s *FileServer) broadcast(msg *Message) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	for _, peer := range s.peers {
		// Send the message type prefix, followed by the gob-encoded payload.
		peer.Send([]byte{p2p.IncomingMessage})
		if err := peer.Send(buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

// Message is a generic container for any payload sent between peers.
type Message struct {
	Payload any
}

// MessageStoreFile is the payload for announcing a new file to the network.
type MessageStoreFile struct {
	ID   string // The ID of the node that originally holds the file.
	Key  string // The hashed key of the file.
	Size int64  // The total size of the encrypted file, including the IV.
}

// MessageGetFile is the payload for requesting a file from the network.
type MessageGetFile struct {
	ID  string // The ID of the node requesting the file.
	Key string // The hashed key of the file being requested.
}

// Get retrieves a file. It first checks local storage, and if the file is
// not found, it requests it from the network.
func (s *FileServer) Get(key string) (io.Reader, error) {
	// If we have the file locally, serve it directly.
	hashedKey := hashKey(key)
	if s.store.Has(s.ID, hashedKey) {
		fmt.Printf("[%s] serving file (%s) from local disk\n", s.Transport.Addr(), key)
		_, r, err := s.store.Read(s.ID, hashedKey)
		return r, err
	}

	fmt.Printf("[%s] don't have file (%s) locally, fetching from network...\n", s.Transport.Addr(), key)

	// If not found locally, broadcast a request to the network.
	msg := Message{
		Payload: MessageGetFile{
			ID:  s.ID,
			Key: hashedKey,
		},
	}
	if err := s.broadcast(&msg); err != nil {
		return nil, err
	}

	// TODO: This logic simply waits for any peer to respond and assumes the first
	// response is the correct one. A more robust implementation would handle
	// multiple responses or timeouts.
	for _, peer := range s.peers {
		// The protocol for receiving a file is:
		// 1. Read the file size (int64) from the connection.
		var fileSize int64
		binary.Read(peer, binary.LittleEndian, &fileSize)

		// 2. Read exactly `fileSize` bytes from the peer and decrypt the stream.
		n, err := s.store.WriteDecrypt(s.EncKey, s.ID, key, io.LimitReader(peer, fileSize))
		if err != nil {
			return nil, err
		}

		fmt.Printf("[%s] received (%d) bytes over the network from (%s)\n", s.Transport.Addr(), n, peer.RemoteAddr())

		// 3. Signal the transport layer that we are done with the stream.
		peer.CloseStream()
	}

	// After receiving and storing the file, read it from local disk to return it.
	_, r, err := s.store.Read(s.ID, key)
	return r, err
}

// Store saves a file to the local disk and broadcasts it to all peers.
func (s *FileServer) Store(key string, r io.Reader) error {
	var (
		fileBuffer = new(bytes.Buffer)
		// TeeReader simultaneously writes data to the file and an in-memory buffer.
		tee = io.TeeReader(r, fileBuffer)
	)

	// Write the file to local storage first.
	size, err := s.store.Write(s.ID, hashKey(key), tee)
	if err != nil {
		return err
	}

	// Announce the new file to all peers via a broadcasted message.
	msg := Message{
		Payload: MessageStoreFile{
			ID:  s.ID,
			Key: hashKey(key),
			// The total size is the file size plus the IV size (16 bytes for AES).
			Size: size + 16,
		},
	}
	if err := s.broadcast(&msg); err != nil {
		return err
	}

	// Brief pause to allow peers to process the announcement message.
	time.Sleep(time.Millisecond * 5)

	// Use io.MultiWriter to stream the encrypted file to all peers simultaneously.
	peers := []io.Writer{}
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	mw := io.MultiWriter(peers...)

	// Send the stream prefix byte, then the encrypted file content.
	mw.Write([]byte{p2p.IncomingStream})
	n, err := copyEncrypt(s.EncKey, fileBuffer, mw)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] received and written (%d) bytes to disk\n", s.Transport.Addr(), n)

	return nil
}

// Stop gracefully shuts down the server.
func (s *FileServer) Stop() {
	close(s.quitch)
}

// OnPeer is a callback executed by the transport layer when a new peer connects.
func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()
	s.peers[p.RemoteAddr().String()] = p
	log.Printf("connected with remote %s", p.RemoteAddr())
	return nil
}

// loop is the main event loop for the server, processing incoming messages.
func (s *FileServer) loop() {
	defer func() {
		log.Println("file server stopped due to error or user quit action")
		s.Transport.Close()
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			var msg Message
			if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
				log.Printf("decoding error: %s", err)
			}
			if err := s.handleMessage(rpc.From, &msg); err != nil {
				log.Printf("handle message error: %s", err)
			}
		case <-s.quitch:
			return
		}
	}
}

// handleMessage routes incoming messages to the appropriate handler based on payload type.
func (s *FileServer) handleMessage(from string, msg *Message) error {
	switch v := msg.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(from, v)
	case MessageGetFile:
		return s.handleMessageGetFile(from, v)
	}
	return nil
}

// handleMessageGetFile processes a request for a file from a peer.
func (s *FileServer) handleMessageGetFile(from string, msg MessageGetFile) error {
	if !s.store.Has(msg.ID, msg.Key) {
		return fmt.Errorf("[%s] need to serve file (%s) but it does not exist on disk", s.Transport.Addr(), msg.Key)
	}

	fmt.Printf("[%s] serving file (%s) over the network to %s\n", s.Transport.Addr(), msg.Key, from)

	fileSize, r, err := s.store.Read(msg.ID, msg.Key)
	if err != nil {
		return err
	}
	if rc, ok := r.(io.ReadCloser); ok {
		defer rc.Close()
	}

	peer, ok := s.peers[from]
	if !ok {
		return fmt.Errorf("peer %s not in map", from)
	}

	// Protocol for sending a file:
	// 1. Send the "IncomingStream" prefix byte.
	peer.Send([]byte{p2p.IncomingStream})
	// 2. Send the file size as a binary-encoded int64.
	binary.Write(peer, binary.LittleEndian, fileSize)
	// 3. Copy the file content to the peer's connection.
	n, err := io.Copy(peer, r)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes over the network to %s\n", s.Transport.Addr(), n, from)
	return nil
}

// handleMessageStoreFile processes an incoming file announcement from a peer.
// It prepares to receive the raw file stream that follows the announcement.
func (s *FileServer) handleMessageStoreFile(from string, msg MessageStoreFile) error {
	peer, ok := s.peers[from]
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in the peer list", from)
	}

	// Read exactly `msg.Size` bytes from the peer and write them to local storage.
	n, err := s.store.Write(msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written %d bytes to disk\n", s.Transport.Addr(), n)

	// Signal the transport layer that the stream has been fully consumed.
	// This unblocks the peer's connection loop.
	peer.CloseStream()
	return nil
}

// bootstrapNetwork connects the server to its initial set of known peers.
func (s *FileServer) bootstrapNetwork() error {
	for _, addr := range s.BootstrapNodes {
		if len(addr) == 0 {
			continue
		}
		go func(addr string) {
			fmt.Printf("[%s] attempting to connect with remote %s\n", s.Transport.Addr(), addr)
			if err := s.Transport.Dial(addr); err != nil {
				log.Println("dial error: ", err)
			}
		}(addr)
	}
	return nil
}

// Start initializes and runs the file server.
func (s *FileServer) Start() error {
	fmt.Printf("[%s] starting fileserver...\n", s.Transport.Addr())

	// Start listening for incoming connections.
	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	// Connect to bootstrap nodes to join the network.
	s.bootstrapNetwork()

	// Start the main message processing loop.
	s.loop()

	return nil
}

// The init function registers the concrete message types with the gob package.
// This is necessary so that gob can encode and decode the generic `any` type
// in the Message struct's Payload field.
func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
}
