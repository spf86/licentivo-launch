package httpapi_test

import (
	"context"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
	"net/http"
	"time"
)

func testDeviceLifecycle(a *testApp, admin, customer *browser, service *app.Service, l *licenseDouble, product string) {
	t := a.t
	ctx := context.Background()
	l.releases = map[string]ports.DeviceRelease{}
	l.releaseTimeout = true
	for _, id := range []string{"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666", "77777777-7777-4777-8777-777777777777"} {
		l.devices = append(l.devices, ports.Device{ID: id, LicenseID: "33333333-3333-4333-8333-333333333333", Name: "Local test device", Status: "active", Generation: 1})
	}
	path := "/api/v1/me/products/" + product + "/devices"
	view := data[app.DevicesView](t, a.request("GET", path+"?environment=sandbox", nil, customer))
	if len(view.Devices) != 3 || view.Remaining != 2 || !view.CanRelease {
		t.Fatal("device list or quota wrong")
	}
	a.expect(a.request("GET", path+"?environment=sandbox", nil, admin), 404)
	input := map[string]string{"environment": "sandbox", "device_id": l.devices[0].ID, "idempotency_key": "device-release-test-01"}
	w := a.request("POST", path+"/release", input, customer)
	a.expect(w, 200)
	op := data[app.DeviceOperationView](t, w)
	w = a.request("POST", path+"/release", input, customer)
	a.expect(w, 200)
	if data[app.DeviceOperationView](t, w).ID != op.ID {
		t.Fatal("device retry reserved twice")
	}
	if handled, err := service.ProcessDeviceOperation(ctx); err != nil || !handled {
		t.Fatalf("device release: %v", err)
	}
	l.devices[0].Generation++
	l.devices[0].Status = "active"
	if err := a.db.Model(&storage.DeviceOperation{}).Where("id = ?", op.ID).Update("available_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessDeviceOperation(ctx); err != nil || !handled {
		t.Fatalf("device release replay: %v", err)
	}
	if l.devices[0].Status != "active" || l.devices[0].Generation != 2 || len(l.releases) != 1 {
		t.Fatal("old device retry released a new generation")
	}
	view = data[app.DevicesView](t, a.request("GET", path+"?environment=sandbox", nil, customer))
	if view.Remaining != 1 {
		t.Fatal("device response loss consumed quota twice")
	}
	input["device_id"] = l.devices[1].ID
	input["idempotency_key"] = "device-release-test-02"
	a.expect(a.request("POST", path+"/release", input, customer), 200)
	input["device_id"] = l.devices[2].ID
	input["idempotency_key"] = "device-release-test-03"
	a.expect(a.request("POST", path+"/release", input, customer), 409)
	if handled, err := service.ProcessDeviceOperation(ctx); err != nil || !handled {
		t.Fatalf("second device: %v", err)
	}
	// An unrelated fixture IP lets the ownership assertion remain independent of
	// the separate per-IP rate-limit protection exercised elsewhere.
	a.expect(a.request("POST", path+"/release", input, admin, func(r *http.Request) { r.RemoteAddr = "192.0.2.11:4000" }), 404)
}
