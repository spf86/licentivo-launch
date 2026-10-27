package launchclient

import "testing"

func TestMerchantHTTPRequiresSandboxOptIn(t *testing.T) {
	for _, origin := range []string{"http://192.168.1.20:18080", "http://qa-server:18080", "http://customer-test.example:18080"} {
		if _, err := newClient(origin, false, false); err == nil {
			t.Fatal("HTTP remote origin must not be accepted by default", origin)
		}
		if _, err := newClient(origin, false, true); err != nil {
			t.Fatal("explicit HTTP test origin was rejected", origin, err)
		}
		if _, err := NewApplication(Config{Origin: origin, Environment: "live", AllowHTTP: true}); err == nil {
			t.Fatal("live HTTP must remain forbidden")
		}
	}
}
