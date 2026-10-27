package origin

import "testing"

func TestNormalize(t *testing.T) {
	for raw, want := range map[string]string{
		" HTTPS://MERCHANT.example:443/ ":  "https://merchant.example",
		"http://merchant.example:00080/":   "http://merchant.example",
		"https://merchant.example:08443":   "https://merchant.example:8443",
		"http://localhost:65535/":          "http://localhost:65535",
		"https://[0:0:0:0:0:0:0:1]:443/":   "https://[::1]",
		"http://[::ffff:127.0.0.1]:8080":   "http://[::ffff:7f00:1]:8080",
		"https://xn--fsqu00a.xn--0zwm56d/": "https://xn--fsqu00a.xn--0zwm56d",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := Normalize(raw)
			if err != nil || got != want {
				t.Fatalf("Normalize(%q) = %q, %v; want %q", raw, got, err, want)
			}
			if again, err := Normalize(got); err != nil || again != got {
				t.Fatalf("normalized origin changed on reuse: %q, %v", again, err)
			}
		})
	}
}

func TestRejectInvalidOrigin(t *testing.T) {
	for _, raw := range []string{
		"", "//merchant.example", "file://merchant.example", "https://user:secret@merchant.example",
		"https://merchant.example?", "https://merchant.example#", "https://merchant.example?q=1",
		"https://merchant.example/path", "https://merchant.example/%2f", "https://merchant.example:",
		"https://merchant.example:0", "https://merchant.example:65536", "https://merchant.example:abc",
		"https://例子.测试", "https://[not-an-ipv6]", "http://[fe80::1%25eth0]", "https://::1",
	} {
		t.Run(raw, func(t *testing.T) {
			if got, err := Normalize(raw); err == nil {
				t.Fatalf("invalid origin accepted: %q", got)
			}
		})
	}
}
