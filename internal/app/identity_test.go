package app

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"licentivo-launch/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestIdentityMatchesLicentivoTypedAssertion(t *testing.T) {
	s := &Service{config: Config{PublicURL: "https://merchant.example", MasterKey: make([]byte, 32)}}
	at := time.Now().UTC().Add(-time.Second)
	session := storage.Session{ID: "11111111-1111-4111-8111-111111111111", CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	trust := storage.LaunchTrustConnection{RemoteID: "22222222-2222-4222-8222-222222222222", Audience: IdentityAudience, Environment: "sandbox"}
	v, err := s.signIdentity(storage.User{ID: "opaque-account"}, session, "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", "sandbox", trust, "purchase_ref")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(v.IdentityToken, ".")
	if len(parts) != 3 {
		t.Fatal("invalid token")
	}
	decode := func(value string) []byte {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if !ed25519.Verify(s.identityPrivateKey().Public().(ed25519.PublicKey), []byte(parts[0]+"."+parts[1]), decode(parts[2])) {
		t.Fatal("bad signature")
	}
	var header map[string]string
	_ = json.Unmarshal(decode(parts[0]), &header)
	if header["alg"] != "Ed25519" || header["typ"] != "licentivo-launch-identity+jwt" || header["kid"] != s.identityKeyID() {
		t.Fatal("upstream JOSE contract mismatch")
	}
	var claims map[string]any
	_ = json.Unmarshal(decode(parts[1]), &claims)
	for _, name := range []string{"iss", "aud", "sub", "sid", "jti", "iat", "nbf", "exp", "auth_time", "session_exp", "environment", "launch_product_id", "product_id", "entitlement_ref"} {
		if _, ok := claims[name]; !ok {
			t.Fatal("missing identity field", name)
		}
	}
	for _, name := range []string{"features", "license_key", "max_devices", "valid_until", "offline_seconds", "policy_id"} {
		if _, ok := claims[name]; ok {
			t.Fatal("authorization field in identity", name)
		}
	}
	if claims["sub"] == "opaque-account" || claims["environment"] != "sandbox" || claims["sid"] != session.ID || claims["entitlement_ref"] != "purchase_ref" {
		t.Fatal("identity context mismatch")
	}
	if claims["exp"].(float64)-claims["iat"].(float64) > 120 || claims["session_exp"].(float64) != float64(session.ExpiresAt.Unix()) {
		t.Fatal("identity lifetime mismatch")
	}
}

func TestIdentityCannotExtendAnAccountSession(t *testing.T) {
	s := &Service{config: Config{PublicURL: "https://merchant.example", MasterKey: make([]byte, 32)}}
	at := time.Now().UTC().Add(-time.Minute)
	for _, expiry := range []time.Time{at.Add(-time.Second), at.Add(31 * 24 * time.Hour)} {
		if _, err := s.signIdentity(storage.User{ID: "a"}, storage.Session{ID: "s", CreatedAt: at, ExpiresAt: expiry}, "p", "r", "sandbox", storage.LaunchTrustConnection{}, "ref"); err == nil {
			t.Fatal("invalid account session produced an assertion")
		}
	}
}
