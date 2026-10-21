package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInitPreservesExistingConfigurationAndUsesIndependentKeys(t *testing.T) {
	first := t.TempDir()
	if err := initializeConfiguration(first); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(first, ".local", "dev", "launch-config.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(before, &values); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(values["LAUNCH_MASTER_KEY"])
	if err != nil || len(key) != 32 {
		t.Fatal("missing valid master key")
	}
	if _, exists := values["LAUNCH_SETUP_TOKEN"]; exists {
		t.Fatal("init must not require a setup token")
	}
	if err := initializeConfiguration(first); err == nil {
		t.Fatal("init overwrote an existing configuration")
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing instance configuration changed")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("configuration is not private")
		}
	}
	second := t.TempDir()
	if err := initializeConfiguration(second); err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(second, ".local", "dev", "launch-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, other) {
		t.Fatal("independent instances share a master key")
	}
}
