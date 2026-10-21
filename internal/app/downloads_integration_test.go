package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"licentivo-launch/internal/config"
	"licentivo-launch/internal/storage"
)

// TestProductUploadRoundTrip uses a database transaction that is always rolled
// back and a new temporary upload directory. It can therefore verify the real
// MySQL/GORM path without modifying any existing product or installer.
func TestProductUploadRoundTrip(t *testing.T) {
	configPath := os.Getenv("LAUNCH_E2E_CONFIG_FILE")
	if configPath == "" {
		t.Skip("set LAUNCH_E2E_CONFIG_FILE to run the rollback-only upload test")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("could not read the E2E configuration")
	}
	var values map[string]string
	if json.Unmarshal(raw, &values) != nil {
		t.Fatal("invalid E2E configuration")
	}
	cfg, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal("invalid E2E configuration")
	}
	db, err := storage.Open(cfg.DatabaseDSN)
	if err != nil {
		t.Fatal("could not open the E2E database")
	}
	defer storage.Close(db)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal("could not start the E2E transaction")
	}
	defer tx.Rollback()

	uploadDir := t.TempDir()
	service, err := New(context.Background(), tx, Config{PublicURL: cfg.PublicURL, MasterKey: cfg.MasterKey, UploadDir: uploadDir})
	if err != nil {
		t.Fatal(err)
	}
	var staff storage.StaffAccount
	if tx.First(&staff, "role = ?", "admin").Error != nil {
		t.Fatal("administrator missing from E2E database")
	}
	var user storage.User
	if tx.First(&user, "id = ?", staff.UserID).Error != nil {
		t.Fatal("administrator user missing from E2E database")
	}
	productID, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	product := storage.Product{ID: productID, Name: "Upload E2E", Slug: "upload-e2e-" + productID[:8], Status: "published", Content: marshal(ProductContent{Summary: "temporary rollback-only test"})}
	if tx.Create(&product).Error != nil {
		t.Fatal("could not create temporary E2E product")
	}

	payload := []byte{'M', 'Z', 0x90, 0x00, 'L', 'a', 'u', 'n', 'c', 'h'}
	principal := Principal{User: user, Role: "admin"}
	view, err := service.SaveUploadedProductDownload(context.Background(), principal, productID, "windows", "e2e-1.0", "Launch Setup.exe", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if view.Platform != "windows" || view.Source != "upload" || view.Version != "e2e-1.0" || view.SizeBytes != uint64(len(payload)) {
		t.Fatalf("unexpected upload view: %+v", view)
	}
	download, err := service.OpenProductDownload(context.Background(), view.ID)
	if err != nil {
		t.Fatal(err)
	}
	received, readErr := io.ReadAll(download.File)
	download.File.Close()
	if readErr != nil || !bytes.Equal(received, payload) {
		t.Fatal("downloaded bytes did not match the uploaded installer")
	}
	if err := service.DeleteProductDownload(context.Background(), principal, productID, "windows"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(uploadDir, productID))
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary installer was not removed after the test")
	}
}
