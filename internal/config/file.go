package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultPath selects the single standard startup configuration.
func DefaultPath(root string) (string, error) {
	for _, name := range []string{"config.yaml"} {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("could not read default configuration file")
		}
		return path, nil
	}
	return "", nil
}

var yamlKeys = map[string]string{
	"environment": "LAUNCH_ENV", "http_addr": "LAUNCH_HTTP_ADDR",
	"https_addr": "LAUNCH_HTTPS_ADDR", "public_url": "LAUNCH_PUBLIC_URL",
	"database_dsn": "LAUNCH_DATABASE_DSN", "master_key": "LAUNCH_MASTER_KEY",
	"admin_email": "LAUNCH_ADMIN_EMAIL", "admin_password": "LAUNCH_ADMIN_PASSWORD",
	"upload_dir": "LAUNCH_UPLOAD_DIR", "backup_passphrase": "LAUNCH_BACKUP_PASSPHRASE",
}

// Nonempty environment values override the selected YAML configuration.
func Lookup(getenv func(string) string) (func(string) string, error) {
	path := getenv("LAUNCH_CONFIG_FILE")
	if path == "" {
		var err error
		path, err = DefaultPath(".")
		if err != nil {
			return nil, err
		}
	}
	if path == "" {
		return getenv, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("could not read LAUNCH_CONFIG_FILE")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("configuration file is too large")
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	values := map[string]string{}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		var document, extra yaml.Node
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		if decoder.Decode(&document) != nil || decoder.Decode(&extra) != io.EOF || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
			return nil, errors.New("YAML configuration must contain one mapping of string values")
		}
		content := document.Content[0].Content
		for i := 0; i < len(content); i += 2 {
			key, value := content[i], content[i+1]
			envKey, allowed := yamlKeys[key.Value]
			if !allowed || key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return nil, errors.New("unsupported YAML configuration key; see config.example.yaml")
			}
			if _, duplicate := values[envKey]; duplicate {
				return nil, errors.New("duplicate YAML configuration key")
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return nil, errors.New("YAML configuration values must be strings; quote numbers and empty values")
			}
			values[envKey] = value.Value
		}
	default:
		return nil, errors.New("configuration file must use .yaml or .yml")
	}
	return func(key string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return values[key]
	}, nil
}

// MarshalStartup emits the private startup values included in an encrypted backup.
func MarshalStartup(values map[string]string) ([]byte, error) {
	fields := map[string]string{}
	for key, env := range yamlKeys {
		if value, ok := values[env]; ok && env != "LAUNCH_BACKUP_PASSPHRASE" {
			fields[key] = value
		}
	}
	raw, err := yaml.Marshal(fields)
	if err != nil {
		return nil, errors.New("could not encode startup configuration")
	}
	return raw, nil
}
