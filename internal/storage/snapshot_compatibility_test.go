package storage

import (
	"encoding/json"
	"testing"
)

func TestSnapshotBeforeReversalTableRemainsRestorable(t *testing.T) {
	base := Snapshot{Format: "licentivo-launch-backup-v1", MasterKey: make([]byte, 32), Tables: []SnapshotTable{{Name: "payments", Rows: json.RawMessage("[]")}, {Name: "orders", Rows: json.RawMessage("[]")}}}
	names := []string{"orders", "payments", "payment_reversals"}
	entries, err := snapshotEntries(base, names)
	if err != nil || string(entries["payment_reversals"]) != "[]" {
		t.Fatal("pre-reversal v1 archive rejected", err)
	}
	for _, tables := range [][]SnapshotTable{base.Tables[:1], append(append([]SnapshotTable{}, base.Tables...), base.Tables[0]), append(append([]SnapshotTable{}, base.Tables...), SnapshotTable{Name: "unknown", Rows: json.RawMessage("[]")})} {
		bad := base
		bad.Tables = tables
		if _, err := snapshotEntries(bad, names); err == nil {
			t.Fatal("incomplete, duplicate or unknown archive accepted")
		}
	}
}

func TestExternalTablesOptionalOnlyForLegacyArchives(t *testing.T) {
	base := Snapshot{Format: "licentivo-launch-backup-v1", MasterKey: make([]byte, 32), Tables: []SnapshotTable{{Name: "orders", Rows: json.RawMessage(`[]`)}, {Name: "sync_tasks", Rows: json.RawMessage(`[]`)}, {Name: "entitlements", Rows: json.RawMessage(`[]`)}}}
	names := []string{"orders", "sync_tasks", "entitlements", "external_licensing_connections", "product_licensing_bindings", "external_plan_mappings"}
	if _, err := snapshotEntries(base, names); err != nil {
		t.Fatal("legacy archive rejected", err)
	}
	for _, table := range []SnapshotTable{{Name: "sync_tasks", Rows: json.RawMessage(`[{"Provider":"custom_http"}]`)}, {Name: "orders", Rows: json.RawMessage(`[{"EntitlementSnapshot":{"external_licensing":{"connection_id":"saved"}}}]`)}, {Name: "entitlements", Rows: json.RawMessage(`[{"Snapshot":{"external_licensing":{"connection_id":"saved"}}}]`)}} {
		bad := base
		bad.Tables = append([]SnapshotTable{}, base.Tables...)
		for i := range bad.Tables {
			if bad.Tables[i].Name == table.Name {
				bad.Tables[i] = table
			}
		}
		if _, err := snapshotEntries(bad, names); err == nil {
			t.Fatalf("incomplete external archive accepted for %s", table.Name)
		}
	}
	partial := base
	partial.Tables = append(append([]SnapshotTable{}, base.Tables...), SnapshotTable{Name: "external_licensing_connections", Rows: json.RawMessage(`[{"ID":"saved"}]`)})
	if _, err := snapshotEntries(partial, names); err == nil {
		t.Fatal("partially missing external tables accepted")
	}
}
