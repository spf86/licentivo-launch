package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

var ErrCiphertext = errors.New("invalid encrypted value or encryption context")

// Vault binds each value to its record and purpose using authenticated data.
// Contexts must be stable, e.g. "license-key:<binding UUID>".
type Vault struct{ aead cipher.AEAD }

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("vault requires a 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

func (v *Vault) Seal(plain []byte, context string) ([]byte, error) {
	if context == "" {
		return nil, errors.New("encryption context is required")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Version byte followed by nonce and authenticated ciphertext.
	out := append([]byte{1}, nonce...)
	return v.aead.Seal(out, nonce, plain, []byte(context)), nil
}

func (v *Vault) Open(sealed []byte, context string) ([]byte, error) {
	n := v.aead.NonceSize()
	if context == "" || len(sealed) < 1+n+v.aead.Overhead() || sealed[0] != 1 {
		return nil, ErrCiphertext
	}
	plain, err := v.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(context))
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}

// RandomReference does not derive an external identity from personal data.
func RandomReference() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
