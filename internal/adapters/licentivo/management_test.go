package licentivo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"licentivo-launch/internal/ports"
)

func TestManagementAPIAdapter(t *testing.T) {
	const (
		productID = "11111111-1111-4111-8111-111111111111"
		policyID  = "22222222-2222-4222-8222-222222222222"
		licenseID = "33333333-3333-4333-8333-333333333333"
		deviceID  = "44444444-4444-4444-8444-444444444444"
	)
	type mockState struct {
		sync.Mutex
		license      nativeLicense
		created      bool
		deviceStatus string
		batches      map[string]json.RawMessage
	}
	s := &mockState{deviceStatus: "active", batches: map[string]json.RawMessage{}}
	write := func(w http.ResponseWriter, status int, data any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Lock()
		defer s.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/policies/"+policyID:
			write(w, 200, nativePolicy{ID: policyID, ProductID: productID, Name: "Professional", Mode: "timed", Version: 1, DurationDays: nil, MaxDevices: 2, Features: []string{"export"}, HeartbeatSeconds: 600, OfflineMode: "limited", OfflineSeconds: 86400})
		case r.Method == "GET" && r.URL.Path == "/api/v1/policies/"+policyID+"/capabilities":
			write(w, 200, nativePolicyCapabilities{PolicyVersion: 1, LicenseModel: "node_locked", SessionSeconds: 1200, SelfReleaseLimit: 2})
		case r.Method == "POST" && r.URL.Path == "/api/v1/licenses/batch":
			var body struct {
				RequestID string   `json:"request_id"`
				Operation string   `json:"operation"`
				Days      int      `json:"days"`
				IDs       []string `json:"ids"`
			}
			raw := json.RawMessage{}
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&body); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			if previous, ok := s.batches[body.RequestID]; ok {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(previous)
				return
			}
			item := nativeBatchItem{ID: licenseID, Operation: body.Operation}
			switch body.Operation {
			case "create":
				s.created = true
				s.license = nativeLicense{ID: licenseID, ProductID: productID, PolicyID: policyID, PolicyVersion: 1, Mode: "timed", Status: "active", DurationDays: nil, CreatedAt: time.Now().UTC()}
				item.LicenseKey = "lv_lic_" + strings.Repeat("a", 43)
			case "renew":
				if !s.created || len(body.IDs) != 1 || body.IDs[0] != licenseID || body.Days < 1 {
					http.Error(w, "conflict", 409)
					return
				}
				if s.license.DurationDays == nil {
					s.license.DurationDays = intPointer(0)
				}
				*s.license.DurationDays += body.Days
			default:
				http.Error(w, "unsupported", 400)
				return
			}
			result := nativeBatchResult{RequestID: body.RequestID, Operation: body.Operation, Items: []nativeBatchItem{item}, Count: 1}
			payload, _ := json.Marshal(map[string]any{"data": result})
			s.batches[body.RequestID] = payload
			raw = payload
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		case r.Method == "GET" && r.URL.Path == "/api/v1/licenses/"+licenseID:
			if !s.created {
				http.NotFound(w, r)
				return
			}
			write(w, 200, s.license)
		case r.Method == "PATCH" && r.URL.Path == "/api/v1/licenses/"+licenseID:
			var body struct {
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.license.Status = body.Status
			write(w, 200, map[string]string{"id": licenseID})
		case r.Method == "GET" && r.URL.Path == "/api/v1/activations":
			write(w, 200, map[string]any{"items": []map[string]any{{"id": deviceID, "license_id": licenseID, "device_name": "Test PC", "status": s.deviceStatus, "generation": 1}}, "total": 1})
		case r.Method == "POST" && r.URL.Path == "/api/v1/activations/"+deviceID+"/release":
			s.deviceStatus = "released"
			write(w, 200, map[string]string{"id": deviceID, "status": "released"})
		default:
			http.Error(w, "unexpected route", 404)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "lv_api_" + strings.Repeat("b", 43), AllowPrivate: true, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := client.Capabilities(context.Background())
	if err != nil || caps.ValidateSales() != nil || !caps.ManagementAPI || !caps.DeviceOperations {
		t.Fatalf("management capabilities unavailable: %+v %v", caps, err)
	}
	check := ports.PolicyCheck{ProductID: productID, PolicyID: policyID, PolicyVersion: 1, SnapshotHash: strings.Repeat("c", 64), Snapshot: json.RawMessage(`{}`), PurchaseType: "fixed_term", TermUnit: "day", TermCount: 30}
	if err := client.CheckPolicy(context.Background(), check); err != nil {
		t.Fatalf("policy check: %v", err)
	}
	definition, err := client.PolicyDefinition(context.Background(), policyID)
	if err != nil || definition.ProductID != productID || definition.MaxDevices != 2 || definition.HeartbeatSeconds != 600 || definition.OfflineMode != "limited" || definition.SessionSeconds != 1200 || definition.SelfReleaseLimit != 2 || len(definition.Features) != 1 || definition.Features[0] != "export" {
		t.Fatalf("policy definition: %+v %v", definition, err)
	}
	from := time.Now().UTC().Truncate(time.Second)
	until := from.Add(30 * 24 * time.Hour)
	create := ports.EntitlementCommand{OperationID: "55555555-5555-4555-8555-555555555555", CustomerRef: strings.Repeat("R", 32), ProductID: productID, PolicyID: policyID, PolicyVersion: 1, SnapshotHash: strings.Repeat("d", 64), ExpectedVersion: 0, TargetVersion: 1, ValidFrom: from, ValidUntil: &until, DesiredStatus: "active"}
	result, err := client.Apply(context.Background(), create)
	if err != nil || result.LicenseID != licenseID || !strings.HasPrefix(string(result.Secret), "lv_lic_") {
		t.Fatalf("create: %+v %v", result, err)
	}
	// Replaying the same target recovers the same one-time key instead of
	// creating another remote license.
	replayed, err := client.Apply(context.Background(), create)
	if err != nil || replayed.LicenseID != licenseID || string(replayed.Secret) != string(result.Secret) {
		t.Fatalf("create replay: %+v %v", replayed, err)
	}
	extended := until.Add(30 * 24 * time.Hour)
	renew := create
	renew.OperationID = "66666666-6666-4666-8666-666666666666"
	renew.LicenseID = licenseID
	renew.ExpectedVersion, renew.TargetVersion, renew.ValidUntil = 1, 2, &extended
	if _, err := client.Apply(context.Background(), renew); err != nil {
		t.Fatalf("renew: %v", err)
	}
	s.Lock()
	duration := *s.license.DurationDays
	s.Unlock()
	if duration != 60 {
		t.Fatalf("duration = %d, want 60", duration)
	}
	suspend := renew
	suspend.OperationID = "77777777-7777-4777-8777-777777777777"
	suspend.ExpectedVersion, suspend.TargetVersion, suspend.DesiredStatus = 2, 3, "suspended"
	if _, err := client.Apply(context.Background(), suspend); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	observed, err := client.LicenseState(context.Background(), licenseID)
	if err != nil || !observed.NativeManagement || observed.Status != "suspended" {
		t.Fatalf("state: %+v %v", observed, err)
	}
	devices, err := client.Devices(context.Background(), licenseID)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %+v %v", devices, err)
	}
	if err := client.ReleaseDevice(context.Background(), ports.DeviceRelease{OperationID: "88888888-8888-4888-8888-888888888888", LicenseID: licenseID, DeviceID: deviceID, ExpectedGeneration: 1}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := client.ReleaseDevice(context.Background(), ports.DeviceRelease{OperationID: "88888888-8888-4888-8888-888888888888", LicenseID: licenseID, DeviceID: deviceID, ExpectedGeneration: 1}); err != nil {
		t.Fatalf("release replay: %v", err)
	}
}

func intPointer(v int) *int { return &v }
