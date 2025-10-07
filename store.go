package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// PathTransformFunc is a function type that defines how a key (e.g., a filename)
// is transformed into a structured file path on disk.
type PathTransformFunc func(string) PathKey

// CASPathTransformFunc implements a Content-Addressable Storage (CAS) strategy.
// It takes a key, computes its SHA1 hash, and then splits the hash into a nested
// directory structure. This prevents having too many files in a single directory,
// which can improve filesystem performance.
func CASPathTransformFunc(key string) PathKey {
	hash := sha1.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])

	// The hash is split into segments to form a cascading directory structure.
	blocksize := 5
	sliceLen := len(hashStr) / blocksize
	paths := make([]string, sliceLen)

	for i := 0; i < sliceLen; i++ {
		from, to := i*blocksize, (i*blocksize)+blocksize
		paths[i] = hashStr[from:to]
	}

	return PathKey{
		PathName: strings.Join(paths, "/"),
		Filename: hashStr,
	}
}

// PathKey holds the structured path and the final filename for a given key.
type PathKey struct {
	PathName string
	Filename string
}

// FirstPathName returns the top-level directory name from the structured path.
// This is useful for deleting the entire directory associated with a key.
func (p PathKey) FirstPathName() string {
	paths := strings.Split(p.PathName, "/")
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// FullPath concatenates the structured path and filename.
func (p PathKey) FullPath() string {
	return fmt.Sprintf("%s/%s", p.PathName, p.Filename)
}

// StoreOpts holds the configuration options for a Store.
type StoreOpts struct {
	// Root is the top-level folder on disk where all files will be stored.
	Root string
	// PathTransformFunc determines the storage path for each file.
	PathTransformFunc PathTransformFunc
}

// DefaultPathTransformFunc provides a simple, non-cascading path transformation
// where the directory and filename are the same as the key.
var DefaultPathTransformFunc = func(key string) PathKey {
	return PathKey{
		PathName: key,
		Filename: key,
	}
}

// Store manages file storage and retrieval on the local disk.
type Store struct {
	StoreOpts
}

// NewStore creates a new Store with the given options, applying defaults if needed.
func NewStore(opts StoreOpts) *Store {
	if opts.PathTransformFunc == nil {
		opts.PathTransformFunc = DefaultPathTransformFunc
	}
	if len(opts.Root) == 0 {
		opts.Root = "default_network" // A default root folder name.
	}
	return &Store{
		StoreOpts: opts,
	}
}

// Has checks if a file for a given key exists in the store for a specific peer ID.
func (s *Store) Has(id string, key string) bool {
	pathKey := s.PathTransformFunc(key)
	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())
	_, err := os.Stat(fullPathWithRoot)
	// The file exists if os.Stat returns no error, or an error other than "not exist".
	return !errors.Is(err, os.ErrNotExist)
}

// Clear completely removes the root storage directory and all its contents.
// This is a destructive operation.
func (s *Store) Clear() error {
	return os.RemoveAll(s.Root)
}

// Delete removes the file and its parent directories associated with a key.
// It deletes from the first-level directory of the transformed path downwards.
func (s *Store) Delete(id string, key string) error {
	pathKey := s.PathTransformFunc(key)

	defer func() {
		log.Printf("deleted [%s] from disk", pathKey.Filename)
	}()

	// Construct the path to the top-level directory for this key.
	firstPathNameWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FirstPathName())
	return os.RemoveAll(firstPathNameWithRoot)
}

// Write saves the content from an io.Reader to disk for the given key.
func (s *Store) Write(id string, key string, r io.Reader) (int64, error) {
	return s.writeStream(id, key, r)
}

// WriteDecrypt reads an encrypted stream from 'r', decrypts it, and writes the
// resulting plaintext to disk.
func (s *Store) WriteDecrypt(encKey []byte, id string, key string, r io.Reader) (int64, error) {
	f, err := s.openFileForWriting(id, key)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := copyDecrypt(encKey, r, f)
	return int64(n), err
}

// openFileForWriting is a helper that creates the necessary directory structure
// and returns an open file handle for writing.
func (s *Store) openFileForWriting(id string, key string) (*os.File, error) {
	pathKey := s.PathTransformFunc(key)
	// Create the nested directory structure if it doesn't exist.
	pathNameWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.PathName)
	if err := os.MkdirAll(pathNameWithRoot, os.ModePerm); err != nil {
		return nil, err
	}
	// Create the final file for writing.
	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())
	return os.Create(fullPathWithRoot)
}

// writeStream handles the logic of streaming data from a reader to a file on disk.
func (s *Store) writeStream(id string, key string, r io.Reader) (int64, error) {
	f, err := s.openFileForWriting(id, key)
	if err != nil {
		return 0, err
	}
	// Using defer ensures the file is closed even if io.Copy fails.
	defer f.Close()
	return io.Copy(f, r)
}

// Read opens a file for a given key and returns its size and an io.Reader for its content.
func (s *Store) Read(id string, key string) (int64, io.Reader, error) {
	return s.readStream(id, key)
}

// readStream handles the logic of opening a file for reading.
// It returns the file size and an io.ReadCloser. The caller is responsible
// for closing the reader when done.
func (s *Store) readStream(id string, key string) (int64, io.ReadCloser, error) {
	pathKey := s.PathTransformFunc(key)
	fullPathWithRoot := fmt.Sprintf("%s/%s/%s", s.Root, id, pathKey.FullPath())

	file, err := os.Open(fullPathWithRoot)
	if err != nil {
		return 0, nil, err
	}

	fi, err := file.Stat()
	if err != nil {
		return 0, nil, err
	}
	return fi.Size(), file, nil
}
