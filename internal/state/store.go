// Package state provides an encrypted, transactional ledger for a single-owner
// instance. An exclusive process lock prevents two workers from delivering the
// same outbox or running the same attendance queue.
package state

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrNotFound = errors.New("record not found")
var header = []byte("KLAP-STATE-1\n")

type Records map[string]json.RawMessage
type Store struct {
	mu      sync.Mutex
	path    string
	aead    cipher.AEAD
	lock    *os.File
	records Records
	closed  bool
}

func Open(dir string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("state key must be 32 bytes")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another KLAP instance is using this data directory")
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	s := &Store{path: filepath.Join(dir, "state.sealed"), aead: aead, lock: lock, records: Records{}}
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err == nil {
		err = s.decode(body)
	}
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("cannot open state: %w", err)
	}
	return s, nil
}

func (s *Store) decode(body []byte) error {
	n := s.aead.NonceSize()
	if !bytes.HasPrefix(body, header) || len(body) < len(header)+n {
		return errors.New("invalid encrypted state")
	}
	body = body[len(header):]
	plain, err := s.aead.Open(nil, body[:n], body[n:], header)
	if err != nil {
		return errors.New("state key mismatch or corrupted data")
	}
	if err = json.Unmarshal(plain, &s.records); err != nil {
		return err
	}
	if s.records == nil {
		return errors.New("invalid state records")
	}
	return nil
}

func (s *Store) View(fn func(Records) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	return fn(clone(s.records))
}

// Update publishes all record changes together, only after the encrypted file
// has been fsynced and renamed. Returning an error from fn rolls everything back.
func (s *Store) Update(fn func(Records) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	next := clone(s.records)
	if err := fn(next); err != nil {
		return err
	}
	plain, err := json.Marshal(next)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	body := append(append([]byte{}, header...), nonce...)
	body = s.aead.Seal(body, nonce, plain, header)
	if err = atomicWrite(s.path, body); err != nil {
		return err
	}
	s.records = clone(next)
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	return s.lock.Close()
}
func clone(in Records) Records {
	out := Records{}
	for k, v := range in {
		out[k] = bytes.Clone(v)
	}
	return out
}
func Get[T any](r Records, key string) (T, error) {
	var v T
	b, ok := r[key]
	if !ok {
		return v, ErrNotFound
	}
	err := json.Unmarshal(b, &v)
	return v, err
}
func Put(r Records, key string, v any) error {
	b, err := json.Marshal(v)
	if err == nil {
		r[key] = b
	}
	return err
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// LoadKey never replaces a missing key when an encrypted ledger already exists.
// Back up this file separately; losing it makes all saved credentials unreadable.
func LoadKey(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid master.key")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "state.sealed")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("master.key is missing; restore the original key")
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return LoadKey(dir)
	}
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(key); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	return key, closeErr
}
