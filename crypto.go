package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"io"
)

// generateID creates a cryptographically secure, random 32-byte string.
// It is used to generate a unique identifier for each server instance (peer).
func generateID() string {
	buf := make([]byte, 32)
	io.ReadFull(rand.Reader, buf)
	return hex.EncodeToString(buf)
}

// hashKey creates an MD5 hash of a given key (e.g., a filename).
// In this context, it is used for consistent addressing of content rather than
// for cryptographic security, making it a form of content-addressable key.
func hashKey(key string) string {
	hash := md5.Sum([]byte(key))
	return hex.EncodeToString(hash[:])
}

// newEncryptionKey generates a new 32-byte (256-bit) key suitable for AES-256 encryption.
func newEncryptionKey() []byte {
	keyBuf := make([]byte, 32)
	io.ReadFull(rand.Reader, keyBuf)
	return keyBuf
}

// copyStream is a helper function that reads from a source (src), processes the data
// through a cipher.Stream, and writes the result to a destination (dst).
// It processes the data in chunks for efficiency.
func copyStream(stream cipher.Stream, blockSize int, src io.Reader, dst io.Writer) (int, error) {
	var (
		// buf is a buffer for chunking read/write operations.
		buf = make([]byte, 32*1024)
		// nw tracks the total number of bytes written. It's initialized to the
		// block size to account for the IV that is prepended to the stream.
		nw = blockSize
	)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			// XORKeyStream applies the stream cipher to the buffer.
			// For encryption, this encrypts plaintext. For decryption, it decrypts ciphertext.
			stream.XORKeyStream(buf, buf[:n])
			nn, err := dst.Write(buf[:n])
			if err != nil {
				return 0, err
			}
			nw += nn
		}
		// If we've reached the end of the source reader, break the loop.
		if err == io.EOF {
			break
		}
		// If any other read error occurred, return it.
		if err != nil {
			return 0, err
		}
	}
	return nw, nil
}

// copyDecrypt reads encrypted data from src, decrypts it using the provided key,
// and writes the plaintext to dst. It uses AES in CTR (Counter) mode.
func copyDecrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	// Create a new AES cipher block from the 256-bit key.
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}

	// The first part of the encrypted stream is the Initialization Vector (IV).
	// Read the IV from the source reader. The IV size is equal to the AES block size.
	iv := make([]byte, block.BlockSize())
	if _, err := src.Read(iv); err != nil {
		return 0, err
	}

	// Create a new CTR stream cipher with the AES block and the read IV.
	stream := cipher.NewCTR(block, iv)
	// Decrypt the rest of the stream and write it to the destination.
	return copyStream(stream, block.BlockSize(), src, dst)
}

// copyEncrypt reads plaintext data from src, encrypts it using the provided key,
// and writes the ciphertext to dst. It uses AES in CTR (Counter) mode.
func copyEncrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	// Create a new AES cipher block from the 256-bit key.
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}

	// Generate a new, cryptographically random Initialization Vector (IV).
	// A unique IV must be used for each encryption to ensure security.
	iv := make([]byte, block.BlockSize()) // 16 bytes for AES
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return 0, err
	}

	// Prepend the IV to the destination writer. The decrypting side will need
	// this IV to initialize its own cipher.
	if _, err := dst.Write(iv); err != nil {
		return 0, err
	}

	// Create a new CTR stream cipher with the AES block and the new IV.
	stream := cipher.NewCTR(block, iv)
	// Encrypt the source stream and write it to the destination.
	return copyStream(stream, block.BlockSize(), src, dst)
}
