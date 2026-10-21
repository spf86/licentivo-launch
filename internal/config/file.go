package config

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// Environment values override the optional local JSON configuration file.
func Lookup(getenv func(string) string) (func(string) string, error) {
	path := getenv("LAUNCH_CONFIG_FILE")
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
	var values map[string]string
	if json.Unmarshal(raw, &values) != nil {
		return nil, errors.New("LAUNCH_CONFIG_FILE must be a JSON object of string values")
	}
	for key := range values {
		if !strings.HasPrefix(key, "LAUNCH_") || key == "LAUNCH_CONFIG_FILE" {
			return nil, errors.New("unsupported configuration key")
		}
	}
	return func(key string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return values[key]
	}, nil
}
