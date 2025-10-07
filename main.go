package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"souvik606/dfs/p2p"
	"strings"
	"time"
)

// makeServer is a helper function that creates and configures a new FileServer instance.
// It simplifies the setup of the transport layer and the file server itself.
func makeServer(listenAddr string, nodes ...string) *FileServer {
	// Configure the underlying TCP transport.
	tcptransportOpts := p2p.TCPTransportOpts{
		ListenAddr:    listenAddr,
		HandshakeFunc: p2p.NOPHandshakeFunc,
		Decoder:       p2p.DefaultDecoder{},
	}
	tcpTransport := p2p.NewTCPTransport(tcptransportOpts)

	// Configure the file server.
	fileServerOpts := FileServerOpts{
		EncKey: newEncryptionKey(),
		// Create a unique storage root directory based on the listen address.
		StorageRoot:       strings.Replace(listenAddr, ":", "", 1) + "_network",
		PathTransformFunc: CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
	}

	s := NewFileServer(fileServerOpts)

	// Set the server's OnPeer callback to handle new peer connections.
	tcpTransport.OnPeer = s.OnPeer

	return s
}

func main() {
	// Create three server instances.
	// s1 and s2 are initial seed nodes.
	s1 := makeServer(":3000", "")
	s2 := makeServer(":7000", "")
	// s3 will connect to s1 and s2 to join the network.
	s3 := makeServer(":5000", ":3000", ":7000")

	// Start s1 and s2 in separate goroutines.
	go func() { log.Fatal(s1.Start()) }()
	time.Sleep(500 * time.Millisecond)
	go func() { log.Fatal(s2.Start()) }()

	// Wait for the seed nodes to be ready.
	time.Sleep(2 * time.Second)

	// Start s3, which will then connect to the others.
	go s3.Start()
	time.Sleep(2 * time.Second)

	// This loop demonstrates the full functionality of the DFS.
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("picture_%d.png", i)
		data := bytes.NewReader([]byte("my big data file here!"))

		// 1. s3 stores the file. This writes it to s3's local disk
		//    and broadcasts it to s1 and s2.
		s3.Store(key, data)

		// 2. To test the network retrieval, s3 immediately deletes its local copy.
		if err := s3.store.Delete(s3.ID, hashKey(key)); err != nil {
			log.Fatal(err)
		}

		// 3. s3 requests the file back from the network. Since it doesn't have it
		//    locally, it will fetch it from either s1 or s2.
		r, err := s3.Get(key)
		if err != nil {
			log.Fatal(err)
		}

		// 4. Read the content of the retrieved file and print it to verify
		//    that the data is intact after the store/delete/get cycle.
		b, err := io.ReadAll(r)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("retrieved data: %s\n", string(b))
	}
}
