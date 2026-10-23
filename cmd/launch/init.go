package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"licentivo-launch/internal/config"
)

// initializeConfiguration never overwrites an existing instance master key.
func initializeConfiguration(root string) error {
	return initializeConfigurationAt(root, "")
}

func initializeConfigurationAt(root, path string) error {
	if path == "" {
		existing, err := config.DefaultPath(root)
		if err != nil {
			return err
		}
		if existing != "" {
			return errors.New("configuration already exists; retain the existing master key and configuration")
		}
		path = "config.yaml"
	}
	if extension := strings.ToLower(filepath.Ext(path)); extension != ".yaml" && extension != ".yml" {
		return errors.New("launch init creates YAML configuration; choose a .yaml or .yml path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	raw := fmt.Sprintf(`# Private startup configuration. Keep this file and master_key between upgrades.
# Nonempty LAUNCH_* environment variables override the corresponding values.
environment: development
http_addr: "127.0.0.1:18080"
https_addr: ""
public_url: "http://localhost:18080"
# Launch enforces parseTime=true, UTC and charset=utf8mb4 for every connection.
database_dsn: "launch:REPLACE_WITH_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4"
master_key: "%s"
upload_dir: ".local/uploads"
# Optional initial administrator overrides; used only for a new database.
admin_email: ""
admin_password: ""
`, base64.StdEncoding.EncodeToString(key[:]))
	return privateFile(path, []byte(raw))
}
