package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/storage"
)

func TestPublicProductConfigRejectsSecretsAndMismatches(t *testing.T) {
	product := "11111111-1111-4111-8111-111111111111"
	base := map[string]any{"product_id": product, "base_url": LicentivoServiceURL, "trusted_keys": map[string]string{"key": base64.RawURLEncoding.EncodeToString(make([]byte, 32))}}
	raw, _ := json.Marshal(base)
	clean, err := validatePublicProductConfig(raw, product)
	if err != nil {
		t.Fatal(err)
	}
	output, _ := json.Marshal(clean)
	for _, private := range []string{"license_key", "cache_path", "device_name", "api_key"} {
		if strings.Contains(string(output), private) {
			t.Fatalf("runtime field %s leaked into download", private)
		}
	}
	for name, value := range map[string]any{"license_key": "customer-secret", "api_key": "management-secret", "private_key": "private-secret", "cache_path": "C:/customer/cache", "device_id": "personal-device", "proxy_url": "https://user:secret@proxy.invalid", "product_id": "another-product", "base_url": "https://attacker.invalid", "allow_http": true, "trusted_keys": map[string]string{"key": "invalid"}} {
		t.Run(name, func(t *testing.T) {
			mutated := map[string]any{}
			for key, value := range base {
				mutated[key] = value
			}
			mutated[name] = value
			raw, _ := json.Marshal(mutated)
			if _, err := validatePublicProductConfig(raw, product); err == nil {
				t.Fatal("unsafe or mismatched config accepted")
			}
		})
	}
	if _, err := validatePublicProductConfig(append(raw, []byte(` {}`)...), product); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestEntitlementActionableStatus(t *testing.T) {
	at := time.Now().UTC()
	before := at.Add(-time.Hour)
	later := at.Add(time.Hour)
	active := storage.Entitlement{Status: "active", ValidFrom: &before, ValidUntil: &later, AppliedVersion: 1, DesiredVersion: 1}
	cases := []struct {
		name         string
		change       func(*storage.Entitlement)
		code, action string
	}{
		{"ready", func(*storage.Entitlement) {}, "ENTITLEMENT_READY", "activate"},
		{"pending", func(e *storage.Entitlement) { e.AppliedVersion = 0 }, "ENTITLEMENT_PENDING", "retry"},
		{"synchronizing", func(e *storage.Entitlement) { e.DesiredVersion = 2 }, "ENTITLEMENT_PENDING", "retry"},
		{"future", func(e *storage.Entitlement) { e.ValidFrom = &later; e.ValidUntil = nil }, "ENTITLEMENT_PENDING", "retry"},
		{"expired_at_boundary", func(e *storage.Entitlement) { e.ValidUntil = &at }, "ENTITLEMENT_EXPIRED", "renew"},
		{"hold", func(e *storage.Entitlement) { e.ManualHold = true }, "ENTITLEMENT_SUSPENDED", "contact_support"},
		{"suspended", func(e *storage.Entitlement) { e.Status = "suspended" }, "ENTITLEMENT_SUSPENDED", "contact_support"},
		{"natural_expiry_suspends_remote", func(e *storage.Entitlement) { e.Status = "suspended"; e.ValidUntil = &at }, "ENTITLEMENT_EXPIRED", "renew"},
		{"revoked", func(e *storage.Entitlement) { e.Status = "revoked" }, "ENTITLEMENT_SUSPENDED", "contact_support"},
		{"hold_precedes_expiry", func(e *storage.Entitlement) { e.ManualHold = true; e.ValidUntil = &at }, "ENTITLEMENT_SUSPENDED", "contact_support"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			row := active
			test.change(&row)
			got := entitlementStatus(row, at)
			if got.Code != test.code || got.NextAction != test.action {
				t.Fatalf("unexpected state %+v", got)
			}
			if got.Status == "pending" {
				if got.MaxRetryAttempts != 5 || got.RetryAfterSeconds != 3 {
					t.Fatal("missing bounded retry guidance")
				}
			} else if got.MaxRetryAttempts != 0 || got.RetryAfterSeconds != 0 {
				t.Fatal("permanent status must not retry")
			}
		})
	}
}

func TestIntegrationReferencesAllNineLanguages(t *testing.T) {
	for _, language := range []string{"go", "python", "csharp", "java", "javascript", "rust", "ruby", "c", "cpp", "web"} {
		if integrationExamplePath(language) == "" {
			t.Fatalf("missing %s reference", language)
		}
	}
	if integrationExamplePath("unknown") != "" {
		t.Fatal("unsupported language accepted")
	}
}

func TestUnissuedSoftwareStatusRequiresConfirmedPayment(t *testing.T) {
	for _, paid := range []bool{false, true} {
		status := unissuedSoftwareStatus("product", "sandbox", paid)
		if paid {
			if status.Code != "ENTITLEMENT_PENDING" || status.NextAction != "retry" || status.MaxRetryAttempts != 5 {
				t.Fatalf("paid issuance must offer bounded retry: %+v", status)
			}
		} else if status.Code != "ENTITLEMENT_NOT_PURCHASED" || status.NextAction != "purchase" || status.MaxRetryAttempts != 0 {
			t.Fatalf("unpaid checkout must not retry issuance: %+v", status)
		}
	}
}
