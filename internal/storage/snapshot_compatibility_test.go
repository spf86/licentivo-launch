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
