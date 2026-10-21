package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
)

// initializeConfiguration never overwrites an existing instance master key.
func initializeConfiguration(root string) error {
	directory := filepath.Join(root, ".local", "dev")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	values := map[string]string{
		"LAUNCH_ENV":          "development",
		"LAUNCH_HTTP_ADDR":    "127.0.0.1:18080",
		"LAUNCH_PUBLIC_URL":   "http://localhost:18080",
		"LAUNCH_DATABASE_DSN": "launch:REPLACE_WITH_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch",
		"LAUNCH_MASTER_KEY":   base64.StdEncoding.EncodeToString(key[:]),
		"LAUNCH_UPLOAD_DIR":   ".local/uploads",
	}
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	return privateFile(filepath.Join(directory, "launch-config.json"), append(raw, '\n'))
}
