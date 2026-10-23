package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfiguration(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestYAMLConfigurationAndEnvironmentPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	writeConfiguration(t, "config.yaml", `# Quotes preserve password punctuation.
environment: production
http_addr: "127.0.0.1:18080"
https_addr: "127.0.0.1:18443"
public_url: "https://shop.example"
database_dsn: "launch:private:password#value@tcp(db:3306)/launch"
master_key: "`+key+`"
upload_dir: ".local/files"
admin_email: "owner@example.invalid"
admin_password: "initial-private-password"
backup_passphrase: "separate-backup-passphrase"
`)
	lookup, err := Lookup(func(name string) string {
		if name == "LAUNCH_HTTP_ADDR" {
			return "127.0.0.1:19090"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.HTTPAddr != "127.0.0.1:19090" || cfg.HTTPSAddr != "127.0.0.1:18443" || cfg.PublicURL != "https://shop.example" || !strings.Contains(cfg.DatabaseDSN, "password#value") || len(cfg.MasterKey) != 32 || cfg.AdminEmail != "owner@example.invalid" || cfg.AdminPassword != "initial-private-password" || lookup("LAUNCH_BACKUP_PASSPHRASE") != "separate-backup-passphrase" {
		t.Fatal("configuration mapping or environment override failed")
	}
	if cfg.UploadDir != filepath.Join(mustWorkingDirectory(t), ".local", "files") {
		t.Fatal("upload directory was not resolved against the working directory")
	}
}

func mustWorkingDirectory(t *testing.T) string {
	t.Helper()
	path, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultConfigurationSelectsOnlyYAML(t *testing.T) {
	t.Chdir(t.TempDir())
	legacy := filepath.Join(".local", "dev", "launch-config.json")
	writeConfiguration(t, legacy, `{"LAUNCH_HTTP_ADDR":"127.0.0.1:18081","LAUNCH_SETUP_TOKEN":""}`)
	lookup, err := Lookup(func(string) string { return "" })
	if err != nil || lookup("LAUNCH_HTTP_ADDR") != "" {
		t.Fatal("obsolete JSON configuration was selected")
	}
	writeConfiguration(t, "config.yaml", "http_addr: '127.0.0.1:18082'\n")
	lookup, err = Lookup(func(string) string { return "" })
	if err != nil || lookup("LAUNCH_HTTP_ADDR") != "127.0.0.1:18082" {
		t.Fatal("config.yaml did not take precedence")
	}
	writeConfiguration(t, "selected.yml", "\ufeffhttp_addr: '127.0.0.1:18083'\n")
	lookup, err = Lookup(func(name string) string {
		if name == "LAUNCH_CONFIG_FILE" {
			return "selected.yml"
		}
		return ""
	})
	if err != nil || lookup("LAUNCH_HTTP_ADDR") != "127.0.0.1:18083" {
		t.Fatal("explicit YAML configuration was not selected")
	}
	writeConfiguration(t, "config.yaml", "database_dsn: [private-sentinel\n")
	if _, err := Lookup(func(string) string { return "" }); err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatal("invalid default YAML fell back to a different instance or leaked configuration")
	}
}

func TestConfigurationFileErrorsDoNotExposeSecrets(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, test := range []struct{ name, content string }{
		{"bad.yaml", "database_dsn: [private-sentinel\n"},
		{"duplicate.yaml", "master_key: private-sentinel\nmaster_key: another-value\n"},
		{"unknown.yaml", "unknown: private-sentinel\n"},
		{"nested.yaml", "database_dsn:\n  password: private-sentinel\n"},
		{"documents.yaml", "database_dsn: private-sentinel\n---\nmaster_key: something\n"},
		{"boolean.yaml", "master_key: false\n"},
		{"numeric.yaml", "master_key: 1234\n"},
		{"null.yaml", "master_key:\n"},
		{"alias.yaml", "database_dsn: &secret private-sentinel\nmaster_key: *secret\n"},
		{"sequence.yaml", "- private-sentinel\n"},
		{"empty.yaml", ""},
		{"large.yaml", "database_dsn: " + strings.Repeat("x", 65536)},
		{"bad.json", `{"LAUNCH_DATABASE_DSN":"private-sentinel"`},
		{"null.json", "null"},
		{"key.json", `{"LAUNCH_CONFIG_FILE":"private-sentinel"}`},
		{"nonstring.json", `{"LAUNCH_MASTER_KEY":123}`},
		{"unknown.txt", "private-sentinel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeConfiguration(t, test.name, test.content)
			_, err := Lookup(func(name string) string {
				if name == "LAUNCH_CONFIG_FILE" {
					return test.name
				}
				return ""
			})
			if err == nil || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("invalid configuration accepted or secret disclosed")
			}
		})
	}
	if _, err := Lookup(func(name string) string {
		if name == "LAUNCH_CONFIG_FILE" {
			return "missing.yaml"
		}
		return ""
	}); err == nil {
		t.Fatal("missing explicit configuration was ignored")
	}
}

func TestEnvironmentOnlyConfigurationAndYMLExtension(t *testing.T) {
	t.Chdir(t.TempDir())
	lookup, err := Lookup(func(name string) string {
		if name == "LAUNCH_HTTP_ADDR" {
			return "127.0.0.1:18084"
		}
		return ""
	})
	if err != nil || lookup("LAUNCH_HTTP_ADDR") != "127.0.0.1:18084" {
		t.Fatal("environment-only configuration no longer works")
	}
	writeConfiguration(t, "instance.yml", "\ufeffhttp_addr: '127.0.0.1:18085'\n")
	lookup, err = Lookup(func(name string) string {
		if name == "LAUNCH_CONFIG_FILE" {
			return "instance.yml"
		}
		return ""
	})
	if err != nil || lookup("LAUNCH_HTTP_ADDR") != "127.0.0.1:18085" {
		t.Fatal("explicit .yml configuration was not accepted")
	}
}

func TestRecoveredStartupYAMLPreservesSecretsAndExcludesBackupPassphrase(t *testing.T) {
	t.Chdir(t.TempDir())
	values := map[string]string{
		"LAUNCH_ENV": "production", "LAUNCH_HTTP_ADDR": "127.0.0.1:18080",
		"LAUNCH_DATABASE_DSN":      "launch:private:#password@tcp(db:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4",
		"LAUNCH_MASTER_KEY":        base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		"LAUNCH_ADMIN_PASSWORD":    "quoted: #pass\\word\nsecond-line",
		"LAUNCH_BACKUP_PASSPHRASE": "excluded-backup-passphrase",
	}
	raw, err := MarshalStartup(values)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), values["LAUNCH_BACKUP_PASSPHRASE"]) || strings.Contains(string(raw), "LAUNCH_") {
		t.Fatal("backup passphrase or obsolete startup keys were included")
	}
	writeConfiguration(t, "config.yaml", string(raw))
	lookup, err := Lookup(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range values {
		if name == "LAUNCH_BACKUP_PASSPHRASE" {
			want = ""
		}
		if lookup(name) != want {
			t.Fatalf("startup value did not round-trip: %s", name)
		}
	}
}
