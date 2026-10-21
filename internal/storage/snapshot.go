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
	if snap.Format != "licentivo-launch-backup-v1" || len(snap.MasterKey) != 32 || len(snap.Tables) != len(models()) {
		return errors.New("unsupported or incomplete backup")
	}
	entries := map[string]json.RawMessage{}
	for _, table := range snap.Tables {
		if _, ok := entries[table.Name]; ok {
			return errors.New("duplicate backup table")
		}
		entries[table.Name] = table.Rows
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
