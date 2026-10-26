package launchclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrNoSession = errors.New("no remembered Launch session")
var ErrSecureStoreUnavailable = errors.New("OS-protected session storage unavailable; supply a secure SessionStore or use memory-only login")

// SessionStore stores sensitive serialized session data. Implementations MUST
// protect confidentiality and integrity for the current OS user. Keys are opaque.
// Load returns ErrNoSession for missing entries. Never log values.
// Stores must isolate the current OS user/host, support multiple opaque keys,
// and replace each value atomically. Active login and account logout tasks use
// distinct keys; do not implement this as a single unkeyed record slot.
type SessionStore interface {
	Load(context.Context, string) ([]byte, error)
	Save(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

// MemoryStore explicitly disables remembered login across process restarts.
type MemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *MemoryStore) Load(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, ErrNoSession
	}
	return append([]byte(nil), value...), nil
}
func (s *MemoryStore) Save(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string][]byte)
	}
	clear(s.values[key])
	s.values[key] = append([]byte(nil), value...)
	return nil
}
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.values[key])
	delete(s.values, key)
	return nil
}

func hashScope(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

type protectedFileStore struct{ directory string }

func (s *protectedFileStore) path(key string) string {
	return filepath.Join(s.directory, hashScope(key)+".protected")
}
func (s *protectedFileStore) Load(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := s.path(key)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > 1<<20 {
		return nil, errors.New("invalid protected session size")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return unprotect(value, []byte(key))
}
func (s *protectedFileStore) Save(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(value) > 1<<20 {
		return errors.New("session is too large")
	}
	sealed, err := protect(value, []byte(key))
	if err != nil {
		return err
	}
	if err := ProtectDirectory(s.directory); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.directory, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(sealed)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path(key))
}
func (s *protectedFileStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(s.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
