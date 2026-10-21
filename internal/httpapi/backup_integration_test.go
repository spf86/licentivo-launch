package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/backup"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
	"os"
	"strings"
)

func testBackupRecovery(a *testApp, service *app.Service, config app.Config) {
	t := a.t
	ctx := context.Background()
	dsn := os.Getenv("LAUNCH_TEST_RESTORE_DATABASE_DSN")
	if dsn == "" {
		t.Log("restore drill skipped: set LAUNCH_TEST_RESTORE_DATABASE_DSN")
		return
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("restore target must be a fresh launch_test_* database")
	}
	target, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(target)
	var tables int64
	if target.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables).Error != nil || tables != 0 {
		t.Fatal("refusing nonempty restore test schema")
	}
	if err := storage.Initialize(ctx, target); err != nil {
		t.Fatal(err)
	}
	snap, err := storage.MakeSnapshot(ctx, a.db, a.key, map[string]string{"LAUNCH_PUBLIC_URL": "https://merchant.example"})
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(snap)
	encrypted, err := backup.Seal(plain, "local-test-backup-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("owner@example.invalid")) {
		t.Fatal("backup contained plaintext customer data")
	}
	restored, err := backup.Open(encrypted, "local-test-backup-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	var recovered storage.Snapshot
	if json.Unmarshal(restored, &recovered) != nil {
		t.Fatal("invalid decrypted archive")
	}
	if err := storage.RestoreSnapshot(ctx, target, recovered); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreSnapshot(ctx, target, recovered); err == nil {
		t.Fatal("restore overwrote existing database")
	}
	loaded, err := app.New(ctx, target, config)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.RecoveryRequired(ctx) {
		t.Fatal("restored financial workers were not held for reconciliation")
	}
	var originalUsers, restoredUsers int64
	a.db.Model(&storage.User{}).Count(&originalUsers)
	target.Model(&storage.User{}).Count(&restoredUsers)
	if originalUsers != restoredUsers {
		t.Fatal("restored customer count differs")
	}
	grant, err := loaded.Login(ctx, "owner@example.invalid", "owner-password-for-tests", "desktop")
	if err != nil {
		t.Fatal("restored admin cannot log in")
	}
	principal, err := loaded.Authenticate(ctx, grant.AccessToken, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.CreateOrder(ctx, principal, app.CheckoutInput{}); err == nil {
		t.Fatal("restore allowed checkout before reconciliation")
	}
	var keyRecord storage.SecretRecord
	if target.First(&keyRecord, "purpose = 'license_key'").Error != nil {
		t.Fatal("restored license secret missing")
	}
	vault, _ := security.NewVault(recovered.MasterKey)
	key, err := vault.Open(keyRecord.Ciphertext, "secret:"+keyRecord.ID)
	if err != nil || string(key) != "lv_lic_test_customer_only" {
		t.Fatal("restored credential could not be decrypted")
	}
	if err := loaded.FinishRecovery(ctx, principal, "owner-password-for-tests", "已核对备份时刻以后的测试付款、退款与授权操作"); err != nil {
		t.Fatal(err)
	}
	if loaded.RecoveryRequired(ctx) {
		t.Fatal("recovery review could not resume workers")
	}
	t.Log("encrypted backup restored into a separate fresh database; login, license decryption, history and recovery gate verified")
}
