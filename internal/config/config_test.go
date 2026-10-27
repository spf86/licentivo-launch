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

func TestPublicOriginRejectsUnusableAddresses(t *testing.T) {
	for _, origin := range []string{
		"https://merchant.example?", "https://merchant.example#",
		"https://merchant.example:", "https://merchant.example:65536", "http://merchant.example:0",
	} {
		t.Run(origin, func(t *testing.T) {
			if _, err := Load(func(key string) string {
				if key == "LAUNCH_PUBLIC_URL" {
					return origin
				}
				return ""
			}); err == nil {
				t.Fatal("unusable public origin was accepted")
			}
		})
	}
}

func TestPublicOriginNormalizesBrowserOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://MERCHANT.example:443/": "https://merchant.example",
		"http://merchant.example:80":    "http://merchant.example",
		"https://[::1]:443/":            "https://[::1]",
		"http://localhost:18080/":       "http://localhost:18080",
	} {
		t.Run(raw, func(t *testing.T) {
			config, err := Load(func(key string) string {
				if key == "LAUNCH_PUBLIC_URL" {
					return raw
				}
				return ""
			})
			if err != nil || config.PublicURL != want {
				t.Fatalf("PublicURL = %q, %v; want %q", config.PublicURL, err, want)
			}
		})
	}
}
