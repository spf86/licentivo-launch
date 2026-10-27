package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

// Inject query failures without depending on an external database. Replacing
// the query callback keeps every request inside the normal GORM call path.
func accountQueryDB(t *testing.T, query func(*gorm.DB)) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		DryRun:               true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Callback().Query().Replace("gorm:query", query); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAccountQueriesDistinguishStorageFailureFromInvalidSession(t *testing.T) {
	for _, method := range []string{"authenticate", "identity"} {
		for _, table := range []string{"sessions", "users"} {
			for _, missing := range []bool{false, true} {
				name := method + "/" + table + "/storage_failure"
				failure, want := errors.New("database connection lost"), ErrStorage
				if missing {
					name = method + "/" + table + "/missing_record"
					failure, want = gorm.ErrRecordNotFound, ErrUnauthorized
				}
				t.Run(name, func(t *testing.T) {
					failed := false
					db := accountQueryDB(t, func(tx *gorm.DB) {
						if tx.Statement.Table == table {
							failed = true
							tx.AddError(failure)
							return
						}
						switch value := tx.Statement.Dest.(type) {
						case *storage.InstanceSettings:
							value.LicensingProvider = "licentivo"
						case *storage.ProductLicensingBinding:
							tx.AddError(gorm.ErrRecordNotFound)
						case *storage.Session:
							*value = storage.Session{ID: "session", UserID: "account", Kind: "desktop"}
						default:
							t.Fatalf("unexpected query: %s", tx.Statement.Table)
						}
					})
					s := &Service{db: db}
					var err error
					if method == "authenticate" {
						_, err = s.Authenticate(context.Background(), strings.Repeat("a", 43), "desktop")
					} else {
						p := Principal{User: storage.User{ID: "account"}, Session: storage.Session{ID: "session", Kind: "desktop"}}
						_, err = s.Identity(context.Background(), p, "11111111-1111-4111-8111-111111111111", "sandbox")
					}
					if !failed || !errors.Is(err, want) {
						t.Fatalf("query reached = %v; got %v, want %v", failed, err, want)
					}
				})
			}
		}
	}
}

func TestLoginReportsStorageFailureSeparatelyFromBadCredentials(t *testing.T) {
	hash, err := security.HashPassword("test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []bool{false, true} {
		name := "storage_failure"
		failure := errors.New("database connection lost")
		wantStatus := 503
		if missing {
			name, failure, wantStatus = "unknown_account", gorm.ErrRecordNotFound, 401
		}
		t.Run(name, func(t *testing.T) {
			db := accountQueryDB(t, func(tx *gorm.DB) {
				switch value := tx.Statement.Dest.(type) {
				case *storage.InstanceSettings:
					at := now()
					value.InstallationCompletedAt = &at
				case *storage.User:
					tx.AddError(failure)
				default:
					t.Fatalf("unexpected query: %s", tx.Statement.Table)
				}
			})
			s := &Service{db: db, dummyHash: hash}
			_, err := s.Login(context.Background(), "account@example.invalid", "test-only-password", "desktop")
			var api *Error
			if !errors.As(err, &api) || api.Status != wantStatus {
				t.Fatalf("got %v, want HTTP %d", err, wantStatus)
			}
		})
	}
}
