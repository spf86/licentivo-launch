package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Only a fresh, explicitly named test database is accepted. No DROP DATABASE,
// TRUNCATE, or mutation of an existing application database is performed.
func TestMySQLBaseline(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_DATABASE_DSN to a fresh launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("integration database must be named launch_test_*")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer Close(db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var tableCount int
	if err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tableCount).Error; err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatal("integration database must be empty; refusing to modify an existing schema")
	}
	if err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, db); err != nil {
		t.Fatalf("repeated GORM initialization: %v", err)
	}
	if err := Ready(ctx, db); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("upgrade pre-reversal schema", func(t *testing.T) {
		if err := db.Exec("ALTER TABLE orders MODIFY COLUMN status ENUM('pending','paid','closed','refunding','refunded') NOT NULL DEFAULT 'pending'").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropTable(&PaymentReversal{}); err != nil {
			t.Fatal(err)
		}
		if err := Initialize(ctx, db); err != nil {
			t.Fatal(err)
		}
		var column string
		if err := db.Raw("SELECT COLUMN_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='orders' AND column_name='status'").Scan(&column).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(column, "'reversed'") || !db.Migrator().HasTable(&PaymentReversal{}) {
			t.Fatal("existing schema did not gain financial reversal support")
		}
	})
	t.Run("upgrade product routing schema", func(t *testing.T) {
		if err := db.Migrator().DropColumn(&Product{}, "LicentivoProductID"); err != nil {
			t.Fatal(err)
		}
		if err := Initialize(ctx, db); err != nil {
			t.Fatal(err)
		}
		if !db.Migrator().HasColumn(&Product{}, "LicentivoProductID") {
			t.Fatal("existing products did not gain the nullable routing field")
		}
	})
	t.Run("constraints", func(t *testing.T) { testConstraints(t, ctx, pool) })
}

func testConstraints(t *testing.T, ctx context.Context, db *sql.DB) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id := func(n int) string { return fmt.Sprintf("%08d-1111-4111-8111-111111111111", n) }
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("fixture failed: %v", err)
		}
	}
	reject := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, query, args...); err == nil {
			t.Fatal("database accepted an invariant violation")
		}
	}
	exec("INSERT INTO secret_records(id,purpose,key_id,ciphertext) VALUES(?,'test','test',?)", id(1), []byte{1, 2, 3})
	exec("INSERT INTO users(id,email,email_key,password_hash) VALUES(?,'test@example.invalid','test@example.invalid','test-only')", id(2))
	exec("INSERT INTO products(id,slug,name,content) VALUES(?,'test','Test',JSON_OBJECT())", id(3))
	exec("INSERT INTO plans(id,product_id,name) VALUES(?,?,'Test')", id(4), id(3))
	exec("INSERT INTO plan_versions(id,plan_id,product_id,version,currency,amount_minor,purchase_type,entitlement_snapshot,snapshot_hash) VALUES(?,?,?,1,'USD',100,'perpetual',JSON_OBJECT(),UNHEX(SHA2('test',256)))", id(5), id(4), id(3))
	reject("INSERT INTO plan_versions(id,plan_id,product_id,version,currency,amount_minor,purchase_type,entitlement_snapshot,snapshot_hash) VALUES(?,?,?,2,'USD',-1,'perpetual',JSON_OBJECT(),UNHEX(SHA2('test',256)))", id(50), id(4), id(3))
	exec("INSERT INTO licentivo_connections(id,base_url,credential_secret_id,capabilities,catalog) VALUES(?,'https://licensing.example.invalid',?,JSON_OBJECT(),JSON_OBJECT())", id(6), id(1))
	for i, environment := range []string{"sandbox", "live"} {
		paymentAccount, order, entitlement := id(8+i), id(10+i), id(12+i)
		exec("INSERT INTO payment_accounts(id,provider,environment,merchant_ref,credential_secret_id) VALUES(?,'stripe',?,'test',?)", paymentAccount, environment, id(1))
		exec("INSERT INTO orders(id,user_id,product_id,plan_version_id,environment,action,idempotency_key,request_hash,currency,amount_minor,price_snapshot,entitlement_snapshot,expected_entitlement_version,expires_at) VALUES(?,?,?,?,?,'purchase','stable',UNHEX(SHA2('test',256)),'USD',100,JSON_OBJECT(),JSON_OBJECT(),0,UTC_TIMESTAMP(6))", order, id(2), id(3), id(5), environment)
		exec("INSERT INTO entitlements(id,user_id,product_id,environment,snapshot) VALUES(?,?,?,?,JSON_OBJECT())", entitlement, id(2), id(3), environment)
	}
	reject("INSERT INTO orders(id,user_id,product_id,plan_version_id,environment,action,idempotency_key,request_hash,currency,amount_minor,price_snapshot,entitlement_snapshot,expected_entitlement_version,expires_at) VALUES(?,?,?,?,'sandbox','purchase','stable',UNHEX(SHA2('test',256)),'USD',100,JSON_OBJECT(),JSON_OBJECT(),0,UTC_TIMESTAMP(6))", id(51), id(2), id(3), id(5))
	// Sandbox orders cannot use live payment accounts.
	reject("INSERT INTO payments(id,order_id,account_id,environment,operation_id,status,currency,amount_minor) VALUES(?,?,?,'sandbox',?,'pending','USD',100)", id(20), id(10), id(9), id(21))
	exec("INSERT INTO payments(id,order_id,account_id,environment,operation_id,status,currency,amount_minor,external_transaction_id) VALUES(?,?,?,'sandbox',?,'succeeded','USD',100,'txn-1')", id(20), id(10), id(8), id(21))
	reject("INSERT INTO payments(id,order_id,account_id,environment,operation_id,status,currency,amount_minor,external_transaction_id) VALUES(?,?,?,'sandbox',?,'succeeded','USD',100,'txn-1')", id(22), id(10), id(8), id(23))
	exec("INSERT INTO payment_events(id,account_id,external_event_id,kind,body_hash,verified_at) VALUES(?,?,'evt-1','test',UNHEX(SHA2('test',256)),UTC_TIMESTAMP(6))", id(24), id(8))
	reject("INSERT INTO payment_events(id,account_id,external_event_id,kind,body_hash,verified_at) VALUES(?,?,'evt-1','test',UNHEX(SHA2('test',256)),UTC_TIMESTAMP(6))", id(25), id(8))
	// A binding must use the one configured Licentivo connection and only one can be current.
	reject("INSERT INTO license_bindings(id,entitlement_id,connection_id,customer_ref,remote_license_id,credential_secret_id) VALUES(?,?,?,?,?,?)", id(30), id(12), id(7), strings.Repeat("r", 32), id(31), id(1))
	exec("INSERT INTO license_bindings(id,entitlement_id,connection_id,customer_ref,remote_license_id,credential_secret_id) VALUES(?,?,?,?,?,?)", id(30), id(12), id(6), strings.Repeat("r", 32), id(31), id(1))
	reject("INSERT INTO license_bindings(id,entitlement_id,connection_id,customer_ref,remote_license_id,credential_secret_id) VALUES(?,?,?,?,?,?)", id(32), id(12), id(6), strings.Repeat("s", 32), id(33), id(1))
	exec("UPDATE license_bindings SET replaced_at=UTC_TIMESTAMP(6) WHERE id=?", id(30))
	exec("INSERT INTO license_bindings(id,entitlement_id,connection_id,customer_ref,remote_license_id,credential_secret_id) VALUES(?,?,?,?,?,?)", id(32), id(12), id(6), strings.Repeat("r", 32), id(33), id(1))
	reject("INSERT INTO sync_tasks(operation_id,entitlement_id,expected_version,target_version,request_json,request_hash) VALUES(?,?,2,1,JSON_OBJECT(),UNHEX(SHA2('test',256)))", id(40), id(12))
}
