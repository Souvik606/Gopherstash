# GopherStash 📦

![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)

GopherStash is a simple, peer-to-peer distributed file system built from scratch in Go. It demonstrates core concepts of P2P networking, content-addressable storage, and end-to-end encryption.

## ✨ Key Features

* **🏡 Peer-to-Peer Architecture:** No central server or single point of failure.
* **🔒 End-to-End Encryption:** Files are secured during transfer using AES-256 in CTR mode.
* **🗂️ Content-Addressable Storage:** Files are stored in a nested directory structure based on the SHA-1 hash of their key.
* **⚡ Raw TCP Networking:** The transport layer is built on raw TCP with a custom binary protocol.
* **🔄 Simple File Replication:** Storing a file on one node automatically replicates it to all connected peers.

## ⚙️ How It Works

The network is fully peer-to-peer with no central server. All communication is managed through a custom TCP protocol that distinguishes between metadata messages and raw, encrypted file streams.

### Storing a File 📤

When a user stores a file on a node, the following happens:
1.  **Local Write & Buffer:** The file is simultaneously written to the node's local disk and an in-memory buffer.
2.  **Metadata Broadcast:** A metadata message, containing the file's key and encrypted size, is `gob` encoded and sent to all connected peers.
3.  **Encrypted Stream:** Immediately after, the node encrypts the in-memory buffer (using AES-256 CTR with a unique IV) and streams the ciphertext directly to all peers.
4.  **Peer Reception:** The receiving peers read the raw stream of bytes, write the encrypted content to their local disks, and then unblock their connection to wait for the next message.

### Retrieving a File 📥

When a user requests a file:
1.  **Local Check:** The node first checks its own local storage. If the file is found, it is served immediately.
2.  **Network Request:** If the file is not found locally, the node broadcasts a request message to all its peers.
3.  **Peer Response:** Any peer that has the file will respond by:
    * First, sending a special byte to indicate a stream is coming.
    * Next, sending the total size of the encrypted file as an `int64`.
    * Finally, streaming the full encrypted file content.
4.  **Receive & Decrypt:** The original node reads the size, then reads that exact number of bytes from the connection, decrypts the stream, saves it to its own disk, and finally returns the file to the user.

## 🚀 Getting Started

### Prerequisites
* Go (version 1.21+)

### Running the Network

1.  **Clone the repository:**
    ```sh
    git clone [https://github.com/your-username/gopher-stash.git](https://github.com/your-username/gopher-stash.git)
    cd gopher-stash
    ```

2.  **Run the application:**
    ```sh
    go run .
    ```
    This command starts a 3-node network and runs a demo where one node stores, deletes, and retrieves files from the others.

## 📂 Project Structure

* `main.go` - Main application entrypoint, sets up and runs the network.
* `server.go` - High-level fileserver logic for `Store` and `Get` operations.
* `store.go` - Handles local disk storage and retrieval using a CAS strategy.
* `crypto.go` - All cryptographic functions for encryption and hashing.
* `p2p/` - The core P2P networking layer (transport, peers, encoding).
