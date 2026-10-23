//go:build !windows

package launchclient

import "os"

// Supply a SessionStore backed by Keychain or Secret Service on this platform.
// MemoryStore is available for explicitly non-persistent sessions.
func NewSystemSessionStore(string) (SessionStore, error) { return nil, ErrSecureStoreUnavailable }
func ProtectDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return os.Chmod(directory, 0700)
}
func protect([]byte, []byte) ([]byte, error)   { return nil, ErrSecureStoreUnavailable }
func unprotect([]byte, []byte) ([]byte, error) { return nil, ErrSecureStoreUnavailable }
