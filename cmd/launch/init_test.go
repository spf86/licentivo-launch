package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"licentivo-launch/internal/config"
)

func TestInitPreservesExistingConfigurationAndUsesIndependentKeys(t *testing.T) {
	first := t.TempDir()
	if err := initializeConfiguration(first); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(first, "config.yaml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := config.Lookup(func(name string) string {
		if name == "LAUNCH_CONFIG_FILE" {
			return configPath
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(lookup("LAUNCH_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		t.Fatal("missing valid master key")
	}
	if lookup("LAUNCH_SETUP_TOKEN") != "" {
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
	other, err := os.ReadFile(filepath.Join(second, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, other) {
		t.Fatal("independent instances share a master key")
	}
}

func TestInitIgnoresObsoleteJSONAndSupportsExplicitPath(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".local", "dev", "launch-config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"LAUNCH_MASTER_KEY":"existing-instance-key"}`)
	if err := os.WriteFile(legacy, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := initializeConfiguration(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("legacy configuration changed")
	}
	if _, err := os.Stat(filepath.Join(root, "config.yaml")); err != nil {
		t.Fatal("init did not create the standard YAML configuration")
	}
	path := filepath.Join("private", "instance.yml")
	if err := initializeConfigurationAt(root, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, path)); err != nil {
		t.Fatal(err)
	}
	if err := initializeConfigurationAt(root, path); err == nil {
		t.Fatal("explicit config overwritten")
	}
	if err := initializeConfigurationAt(root, "config.json"); err == nil {
		t.Fatal("init generated YAML under a JSON extension")
	}
}
