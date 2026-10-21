package ports

import (
	"context"
	"time"
)

type LicenseState struct {
	LicenseID     string     `json:"license_id"`
	ProductID     string     `json:"product_id"`
	Status        string     `json:"status"`
	Version       uint64     `json:"applied_version"`
	PolicyID      string     `json:"policy_id"`
	PolicyVersion uint64     `json:"policy_version"`
	SnapshotHash  string     `json:"snapshot_hash"`
	ValidFrom     time.Time  `json:"valid_from"`
	ValidUntil    *time.Time `json:"valid_until"`
	// NativeManagement means the remote API has no monotonic entitlement
	// version or Launch snapshot hash. Identity, policy and status are still
	// verified; the local immutable ledger remains the date authority.
	NativeManagement bool `json:"native_management,omitempty"`
}
type LicenseStateProvider interface {
	LicenseState(context.Context, string) (LicenseState, error)
}
