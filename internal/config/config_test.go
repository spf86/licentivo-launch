package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestProductionRequiresExplicitSecretsAndHTTPS(t *testing.T) {
	env := map[string]string{"LAUNCH_ENV": "production"}
	load := func() (Config, error) { return Load(func(k string) string { return env[k] }) }
	if _, err := load(); err == nil {
		t.Fatal("incomplete production accepted")
	}
	env["LAUNCH_DATABASE_DSN"] = "user:private-sentinel@tcp(db:3306)/launch"
	env["LAUNCH_PUBLIC_URL"] = "https://merchant.example"
	env["LAUNCH_MASTER_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := load(); err != nil {
		t.Fatal(err)
	}
	env["LAUNCH_MASTER_KEY"] = "private-sentinel"
	if _, err := load(); err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatal("invalid secret accepted or disclosed")
	}
}

func TestOptionalHTTPSListenerRequiresADistinctAddress(t *testing.T) {
	base := map[string]string{
		"LAUNCH_HTTP_ADDR":  "127.0.0.1:8080",
		"LAUNCH_HTTPS_ADDR": "127.0.0.1:8443",
	}
	getenv := func(key string) string { return base[key] }
	config, err := Load(getenv)
	if err != nil || config.HTTPSAddr != "127.0.0.1:8443" {
		t.Fatalf("valid HTTPS listener was not accepted: %+v, %v", config, err)
	}
	base["LAUNCH_HTTPS_ADDR"] = "127.0.0.1:8080"
	if _, err := Load(getenv); err == nil {
		t.Fatal("matching HTTP and HTTPS listener addresses were accepted")
	}
	base["LAUNCH_HTTPS_ADDR"] = "invalid"
	if _, err := Load(getenv); err == nil {
		t.Fatal("invalid HTTPS listener address was accepted")
	}
}
