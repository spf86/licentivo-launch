package licentivo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMappedProductSigningKeysAreValidatedAndSanitized(t *testing.T) {
	const product = "11111111-1111-4111-8111-111111111111"
	pin := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	key := func(id string, active bool) map[string]any {
		return map[string]any{"key_id": id, "algorithm": "Ed25519", "public_key": pin, "active": active, "private_key": "private-sentinel"}
	}
	for _, scenario := range []string{"valid", "wrong-product", "duplicate", "invalid-key", "wrong-algorithm", "retired-only", "empty", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/products/"+product+"/signing-keys" || r.Header.Get("Authorization") != "Bearer lv_api_test_keys" {
					t.Error("wrong product key request")
					w.WriteHeader(400)
					return
				}
				identity := product
				keys := []map[string]any{key("current", true), key("retired", false)}
				switch scenario {
				case "wrong-product":
					identity = "22222222-2222-4222-8222-222222222222"
				case "duplicate":
					keys[1]["key_id"] = "current"
				case "invalid-key":
					keys[0]["public_key"] = "private-sentinel"
				case "wrong-algorithm":
					keys[0]["algorithm"] = "RSA"
				case "retired-only":
					keys = []map[string]any{key("retired", false)}
				case "empty":
					keys = nil
				case "unavailable":
					w.WriteHeader(503)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"product_id": identity, "keys": keys}})
			}))
			defer server.Close()
			client, err := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "lv_api_test_keys", AllowPrivate: true, AllowHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			keys, err := client.ProductPublicKeys(context.Background(), product)
			if scenario == "valid" {
				if err != nil || len(keys) != 2 || keys["current"] != pin || keys["retired"] != pin {
					t.Fatalf("valid rotation keys were not returned: keys=%v err=%v", keys, err)
				}
			} else if err == nil || keys != nil || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("invalid bundle accepted or provider data disclosed")
			}
			before := calls
			if _, err := client.ProductPublicKeys(context.Background(), "../invalid"); err == nil || calls != before {
				t.Fatal("invalid product reached the service")
			}
		})
	}
}
