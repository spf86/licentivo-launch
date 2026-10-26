package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SnapshotTable struct {
	Name string          `json:"name"`
	Rows json.RawMessage `json:"rows"`
}
type Snapshot struct {
	Format        string            `json:"format"`
	CreatedAt     time.Time         `json:"created_at"`
	MasterKey     []byte            `json:"master_key"`
	Configuration map[string]string `json:"configuration"`
	Tables        []SnapshotTable   `json:"tables"`
}

// Model-aware snapshots retain integers, dates, ciphertext and relationship IDs.
// The caller must encrypt this object before it leaves memory.
func MakeSnapshot(ctx context.Context, db *gorm.DB, key []byte, configuration map[string]string) (Snapshot, error) {
	snap := Snapshot{Format: "licentivo-launch-backup-v1", CreatedAt: time.Now().UTC(), MasterKey: append([]byte{}, key...), Configuration: configuration}
	if len(key) != 32 {
		return snap, errors.New("backup requires the application encryption key")
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range models() {
			stmt := &gorm.Statement{DB: tx}
			if err := stmt.Parse(model); err != nil {
				return err
			}
			slice := reflect.New(reflect.SliceOf(reflect.TypeOf(model).Elem())).Interface()
			if err := tx.Find(slice).Error; err != nil {
				return err
			}
			raw, err := json.Marshal(slice)
			if err != nil {
				return err
			}
			snap.Tables = append(snap.Tables, SnapshotTable{stmt.Schema.Table, raw})
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snap, err
}

// RestoreSnapshot refuses every nonempty target and restores in one transaction.
// Worker leases and login tokens from an old backup cannot be reused as current.
func RestoreSnapshot(ctx context.Context, db *gorm.DB, snap Snapshot) error {
	tableNames := make([]string, 0, len(models()))
	for _, model := range models() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		tableNames = append(tableNames, stmt.Schema.Table)
	}
	entries, err := snapshotEntries(snap, tableNames)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range models() {
			var count int64
			if err := tx.Model(model).Limit(1).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return errors.New("restore requires an empty database; existing data was preserved")
			}
		}
		for _, model := range models() {
			stmt := &gorm.Statement{DB: tx}
			if err := stmt.Parse(model); err != nil {
				return err
			}
			raw, ok := entries[stmt.Schema.Table]
			if !ok {
				return errors.New("backup table is missing")
			}
			slice := reflect.New(reflect.SliceOf(reflect.TypeOf(model).Elem())).Interface()
			if err := json.Unmarshal(raw, slice); err != nil {
				return errors.New("backup table content is invalid")
			}
			if rows, ok := slice.(*[]EntitlementCoverage); ok {
				ordered := []EntitlementCoverage{}
				pending := append([]EntitlementCoverage{}, (*rows)...)
				done := map[string]bool{}
				for len(pending) > 0 {
					remaining := []EntitlementCoverage{}
					for _, row := range pending {
						if row.BaseCoverageID == nil || done[*row.BaseCoverageID] {
							ordered = append(ordered, row)
							done[row.ID] = true
						} else {
							remaining = append(remaining, row)
						}
					}
					if len(remaining) == len(pending) {
						return errors.New("backup coverage dependencies are invalid")
					}
					pending = remaining
				}
				*rows = ordered
			}
			if reflect.ValueOf(slice).Elem().Len() > 0 {
				if err := tx.Omit(clause.Associations).CreateInBatches(slice, 100).Error; err != nil {
					return err
				}
			}
		}
		at := time.Now().UTC()
		if err := tx.Model(&Session{}).Where("revoked_at IS NULL").Update("revoked_at", at).Error; err != nil {
			return err
		}
		if err := tx.Model(&EmailCredential{}).Where("used_at IS NULL").Update("used_at", at).Error; err != nil {
			return err
		}
		// Keep payment, refund and authorization facts intact. The merchant must
		// reconcile transactions after the backup timestamp before resuming workers.
		return tx.Model(&InstanceSettings{}).Where("id = 1").Update("recovery_required", true).Error
	})
}

// Additive tables may be absent in older v1 archives. External tables are only
// optional when the archive contains no custom licensing state.
func snapshotEntries(snap Snapshot, tableNames []string) (map[string]json.RawMessage, error) {
	if snap.Format != "licentivo-launch-backup-v1" || len(snap.MasterKey) != 32 {
		return nil, errors.New("unsupported or incomplete backup")
	}
	known := make(map[string]bool, len(tableNames))
	missingExternal := false
	for _, name := range tableNames {
		known[name] = true
	}
	entries := make(map[string]json.RawMessage, len(tableNames))
	for _, table := range snap.Tables {
		if !known[table.Name] {
			return nil, errors.New("unknown backup table")
		}
		if _, exists := entries[table.Name]; exists {
			return nil, errors.New("duplicate backup table")
		}
		entries[table.Name] = table.Rows
	}
	for _, name := range tableNames {
		if _, exists := entries[name]; !exists {
			if name != "payment_reversals" && name != "external_licensing_connections" && name != "product_licensing_bindings" && name != "external_plan_mappings" {
				return nil, errors.New("backup table is missing")
			}
			entries[name] = json.RawMessage("[]")
			if name != "payment_reversals" {
				missingExternal = true
			}
		}
	}
	if missingExternal {
		for _, name := range []string{"external_licensing_connections", "product_licensing_bindings", "external_plan_mappings"} {
			var rows []json.RawMessage
			if raw := entries[name]; len(raw) > 0 {
				if json.Unmarshal(raw, &rows) != nil || len(rows) > 0 {
					return nil, errors.New("external licensing backup table is missing")
				}
			}
		}
		for _, name := range []string{"sync_tasks", "orders", "entitlements"} {
			var rows []struct {
				Provider            string
				Snapshot            json.RawMessage
				EntitlementSnapshot json.RawMessage
			}
			if raw := entries[name]; len(raw) > 0 {
				if json.Unmarshal(raw, &rows) != nil {
					return nil, errors.New("backup table content is invalid")
				}
			}
			for _, row := range rows {
				if row.Provider == "custom_http" {
					return nil, errors.New("external licensing backup table is missing")
				}
				for _, raw := range []json.RawMessage{row.Snapshot, row.EntitlementSnapshot} {
					if len(raw) == 0 {
						continue
					}
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil {
						return nil, errors.New("backup snapshot is invalid")
					}
					if value := fields["external_licensing"]; len(value) > 0 && string(value) != "null" {
						return nil, errors.New("external licensing backup table is missing")
					}
				}
			}
		}
	}
	return entries, nil
}
