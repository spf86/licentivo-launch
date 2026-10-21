package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"
)

func testTLSBundle(t *testing.T, hostname string, notBefore, notAfter time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname},
		DNSNames:     []string{hostname},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certificatePEM), string(privateKeyPEM)
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	var business *Error
	if !errors.As(err, &business) {
		t.Fatalf("expected business error, got %v", err)
	}
	return business.Code
}

func TestValidateManagedTLSBundle(t *testing.T) {
	at := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testTLSBundle(t, "merchant.example", at.Add(-time.Hour), at.Add(90*24*time.Hour))
	pair, metadata, err := validateManagedTLSBundle(certificatePEM, privateKeyPEM, "merchant.example", at)
	if err != nil {
		t.Fatal(err)
	}
	if pair.Leaf == nil || metadata.CommonName != "merchant.example" || metadata.FingerprintSHA256 == "" {
		t.Fatalf("certificate metadata was incomplete: %+v", metadata)
	}
	if len(metadata.Names) != 1 || metadata.Names[0] != "merchant.example" {
		t.Fatalf("unexpected certificate names: %#v", metadata.Names)
	}
}

func TestValidateManagedTLSBundleRejectsWrongHostnameAndExpiredCertificate(t *testing.T) {
	at := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testTLSBundle(t, "merchant.example", at.Add(-time.Hour), at.Add(24*time.Hour))
	if _, _, err := validateManagedTLSBundle(certificatePEM, privateKeyPEM, "other.example", at); errorCode(t, err) != "TLS_HOSTNAME_MISMATCH" {
		t.Fatalf("wrong hostname returned the wrong error: %v", err)
	}
	expiredCertificate, expiredKey := testTLSBundle(t, "merchant.example", at.Add(-48*time.Hour), at.Add(-time.Hour))
	if _, _, err := validateManagedTLSBundle(expiredCertificate, expiredKey, "merchant.example", at); errorCode(t, err) != "TLS_CERTIFICATE_EXPIRED" {
		t.Fatalf("expired certificate returned the wrong error: %v", err)
	}
}

func TestValidateManagedTLSBundleRejectsMismatchedPrivateKey(t *testing.T) {
	at := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	certificatePEM, _ := testTLSBundle(t, "merchant.example", at.Add(-time.Hour), at.Add(24*time.Hour))
	_, anotherKey := testTLSBundle(t, "merchant.example", at.Add(-time.Hour), at.Add(24*time.Hour))
	if _, _, err := validateManagedTLSBundle(certificatePEM, anotherKey, "merchant.example", at); errorCode(t, err) != "TLS_KEY_PAIR_INVALID" {
		t.Fatalf("mismatched private key returned the wrong error: %v", err)
	}
}

func TestNormalizePublicOrigin(t *testing.T) {
	for _, test := range []struct {
		name, raw, mode, want string
		valid                 bool
	}{
		{name: "https origin", raw: " https://merchant.example:8443/ ", mode: "https", want: "https://merchant.example:8443", valid: true},
		{name: "http origin", raw: "http://localhost:18080", mode: "http", want: "http://localhost:18080", valid: true},
		{name: "scheme mismatch", raw: "http://merchant.example", mode: "https"},
		{name: "path rejected", raw: "https://merchant.example/admin", mode: "https"},
		{name: "credentials rejected", raw: "https://user:pass@merchant.example", mode: "https"},
		{name: "unicode hostname requires punycode", raw: "https://例子.测试", mode: "https"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePublicOrigin(test.raw, test.mode)
			if test.valid && (err != nil || got != test.want) {
				t.Fatalf("normalizePublicOrigin() = %q, %v; want %q", got, err, test.want)
			}
			if !test.valid && err == nil {
				t.Fatalf("invalid origin was accepted: %q", got)
			}
		})
	}
}
