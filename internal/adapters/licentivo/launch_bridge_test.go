package licentivo

import (
	"context"
	"encoding/json"
	"errors"
	"licentivo-launch/internal/ports"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLaunchDeviceProtocolPreservesAccountAndGeneration(t *testing.T) {
	const connection = "11111111-1111-4111-8111-111111111111"
	const device = "22222222-2222-4222-8222-222222222222"
	const operation = "33333333-3333-4333-8333-333333333333"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lv_api_"+strings.Repeat("a", 43) {
			t.Error("server credentials not isolated")
		}
		var data any
		if r.Method == "GET" {
			if r.URL.Path != "/api/v1/launch/connections/"+connection+"/entitlements/purchase_ref/devices" || r.URL.Query().Get("account_ref") != "account_ref" || r.URL.Query().Get("environment") != "sandbox" {
				t.Error("customer scope was changed")
			}
			// Use the wire representation so new upstream fields cannot silently
			// disappear when decoded into Launch's device model.
			data = map[string]any{"items": []map[string]any{{
				"id": device, "generation": 7, "status": "active",
				"device_name": "MacBook Air", "device_alias": "Work Mac", "device_code": "ABCD-1234",
				"platform": "darwin", "os_version": "macOS 15.6", "cpu": "Apple M2", "memory_mb": 16384,
				"gpu": "Apple M2 GPU", "system_type": "arm64", "storage_gb": 512, "app_version": "1.2.3",
				"device_id": "private-fingerprint", "device_key": "private-device-key", "license_key": "private-license-key",
			}}, "remaining": 2, "can_release": true}
		} else {
			var in ports.LaunchDeviceRelease
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.OperationID != operation || in.ExpectedGeneration != 7 || in.AccountRef != "account_ref" || in.EntitlementRef != "purchase_ref" || in.Environment != "sandbox" {
				t.Error("release scope/generation was changed")
			}
			if r.URL.Path != "/api/v1/launch/connections/"+connection+"/devices/"+device+"/release" {
				t.Error("legacy release endpoint used")
			}
			w.WriteHeader(202)
			data = ports.LaunchDeviceOperation{OperationID: operation, Status: "pending"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	client, err := NewBridgeClient(server.URL, "lv_api_"+strings.Repeat("a", 43), true)
	if err != nil {
		t.Fatal(err)
	}
	view, err := client.Devices(context.Background(), connection, "purchase_ref", "account_ref", "sandbox")
	if err != nil || view.Remaining != 2 || !view.CanRelease {
		t.Fatal("authoritative response not forwarded", err)
	}
	expected := ports.Device{ID: device, Generation: 7, Status: "active", Name: "MacBook Air", Alias: "Work Mac", Code: "ABCD-1234", Platform: "darwin", OSVersion: "macOS 15.6", CPU: "Apple M2", MemoryMB: 16384, GPU: "Apple M2 GPU", SystemType: "arm64", StorageGB: 512, AppVersion: "1.2.3"}
	if len(view.Items) != 1 || view.Items[0] != expected {
		t.Fatal("device display details were lost or changed", view.Items)
	}
	encoded, err := json.Marshal(view.Items)
	if err != nil || strings.Contains(string(encoded), "private-") {
		t.Fatal("private upstream device fields escaped the public model", err)
	}
	result, err := client.Release(context.Background(), connection, device, ports.LaunchDeviceRelease{OperationID: operation, AccountRef: "account_ref", EntitlementRef: "purchase_ref", Environment: "sandbox", ExpectedGeneration: 7})
	if err != nil || result.Status != "pending" {
		t.Fatal("remote operation was not preserved", err)
	}
}

func TestSessionRevocationProtocolRequiresCommittedMatchingReceipt(t *testing.T) {
	const connection = "11111111-1111-4111-8111-111111111111"
	const event = "22222222-2222-4222-8222-222222222222"
	in := ports.SessionRevocation{EventID: event, Environment: "sandbox", AccountRef: "opaque_customer", Scope: "account", Reason: "password_changed"}
	at := time.Now().UTC()
	valid := ports.SessionRevocationReceipt{ConnectionID: connection, SessionRevocation: in, Status: "succeeded", RevokedBefore: &at, CreatedAt: at, CompletedAt: at}
	for _, scenario := range []string{"committed", "wrong account", "wrong environment", "missing cutoff", "pending", "negative count", "missing receipt"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/launch/connections/"+connection+"/session-revocations" && r.URL.Path != "/api/v1/launch/connections/"+connection+"/session-revocations/"+event {
					t.Error("wrong revocation path")
				}
				if r.Method == "POST" {
					var request ports.SessionRevocation
					decoder := json.NewDecoder(r.Body)
					decoder.DisallowUnknownFields()
					if err := decoder.Decode(&request); err != nil || request != in {
						t.Fatal("revocation body contains changed or extraneous fields", err)
					}
				}
				out := valid
				switch scenario {
				case "wrong account":
					out.AccountRef = "someone_else"
				case "wrong environment":
					out.Environment = "live"
				case "missing cutoff":
					out.RevokedBefore = nil
				case "pending":
					out.Status = "pending"
				case "negative count":
					out.ReleasedDevices = -1
				case "missing receipt":
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
			}))
			defer server.Close()
			client, err := NewBridgeClient(server.URL, "lv_api_"+strings.Repeat("a", 43), true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.RevokeSessions(context.Background(), connection, in)
			if scenario == "committed" && err != nil {
				t.Fatal(err)
			}
			if scenario != "committed" && err == nil {
				t.Fatal("invalid receipt was accepted")
			}
			if scenario == "missing receipt" {
				_, err = client.SessionRevocation(context.Background(), connection, event)
				if !errors.Is(err, ports.ErrContractUnsupported) {
					t.Fatal("missing receipt cannot be safely recovered", err)
				}
			}
		})
	}
}
