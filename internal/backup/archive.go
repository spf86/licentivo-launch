package backup

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/argon2"
	"io"
	"licentivo-launch/internal/security"
)

const MaxSize = 256 << 20

var ErrArchive = errors.New("backup is damaged, unsupported, too large, or the passphrase is incorrect")

const header = "LLBACKUP1"

func Seal(plain []byte, passphrase string) ([]byte, error) {
	if len(passphrase) < 16 || len(passphrase) > 256 || len(plain) > MaxSize {
		return nil, ErrArchive
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, ErrArchive
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		return nil, ErrArchive
	}
	if err := writer.Close(); err != nil {
		return nil, ErrArchive
	}
	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 4, 32)
	vault, err := security.NewVault(key)
	clear(key)
	if err != nil {
		return nil, ErrArchive
	}
	sealed, err := vault.Seal(compressed.Bytes(), header)
	if err != nil {
		return nil, ErrArchive
	}
	out := append([]byte(header), salt...)
	return append(out, sealed...), nil
}
func Open(sealed []byte, passphrase string) ([]byte, error) {
	if len(sealed) > MaxSize || len(sealed) < len(header)+16 || string(sealed[:len(header)]) != header || len(passphrase) < 16 || len(passphrase) > 256 {
		return nil, ErrArchive
	}
	key := argon2.IDKey([]byte(passphrase), sealed[len(header):len(header)+16], 3, 64*1024, 4, 32)
	vault, err := security.NewVault(key)
	clear(key)
	if err != nil {
		return nil, ErrArchive
	}
	compressed, err := vault.Open(sealed[len(header)+16:], header)
	if err != nil {
		return nil, ErrArchive
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, ErrArchive
	}
	defer reader.Close()
	plain, err := io.ReadAll(io.LimitReader(reader, MaxSize+1))
	if err != nil || len(plain) > MaxSize {
		return nil, ErrArchive
	}
	return plain, nil
}
