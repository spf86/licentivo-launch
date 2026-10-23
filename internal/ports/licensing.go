package ports

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var (
	ErrContractUnsupported = errors.New("licensing backend does not implement the sales entitlement contract")
	uuidPattern            = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	referencePattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)
)

type LicensingCapabilities struct {
	DeviceOperations   bool `json:"device_operations"`
	AbsoluteValidity   bool `json:"absolute_validity"`
	PreserveUsage      bool `json:"preserve_usage"`
	IdempotentRecovery bool `json:"idempotent_recovery"`
	CompareAndSwap     bool `json:"compare_and_swap"`
	ManagementAPI      bool `json:"management_api"`
}

func (c LicensingCapabilities) ValidateSales() error {
	// Current Licentivo releases expose an idempotent management API for
	// create/renew/policy operations. Launch adapts that API to its local
	// target-state ledger and keeps absolute sale dates locally.
	if c.ManagementAPI && c.PreserveUsage && c.IdempotentRecovery && c.CompareAndSwap {
		return nil
	}
	if !c.AbsoluteValidity || !c.PreserveUsage || !c.IdempotentRecovery || !c.CompareAndSwap {
		return ErrContractUnsupported
	}
	return nil
}

// EntitlementCommand contains no customer email, password, price or order body.
// It is a target-state command with compare-and-swap, never "add N days".
type EntitlementCommand struct {
	OperationID     string     `json:"operation_id"`
	CustomerRef     string     `json:"customer_ref"`
	ProductID       string     `json:"product_id"`
	PolicyID        string     `json:"policy_id"`
	LicenseID       string     `json:"license_id,omitempty"`
	ExpectedVersion uint64     `json:"expected_version"`
	TargetVersion   uint64     `json:"target_version"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until"`
	DesiredStatus   string     `json:"desired_status"`
	PolicyVersion   uint64     `json:"policy_version"`
	SnapshotHash    string     `json:"snapshot_hash"`
}

func (c EntitlementCommand) Validate() error {
	if !uuidPattern.MatchString(c.OperationID) || !uuidPattern.MatchString(c.ProductID) || !uuidPattern.MatchString(c.PolicyID) || !referencePattern.MatchString(c.CustomerRef) || (c.LicenseID != "" && !uuidPattern.MatchString(c.LicenseID)) {
		return errors.New("invalid licensing identity")
	}
	if c.ExpectedVersion == ^uint64(0) || c.TargetVersion != c.ExpectedVersion+1 || c.PolicyVersion == 0 || (c.LicenseID == "" && c.ExpectedVersion != 0) {
		return errors.New("invalid licensing version transition")
	}
	if c.ValidFrom.IsZero() || (c.ValidUntil != nil && !c.ValidFrom.Before(*c.ValidUntil)) {
		return errors.New("invalid absolute validity")
	}
	if c.DesiredStatus != "active" && c.DesiredStatus != "suspended" {
		return errors.New("sales synchronization cannot permanently revoke a license")
	}
	if len(c.SnapshotHash) != 64 {
		return errors.New("snapshot_hash must be a lowercase SHA-256 hex digest")
	}
	for _, r := range c.SnapshotHash {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return errors.New("snapshot_hash must be a lowercase SHA-256 hex digest")
		}
	}
	return nil
}

type EntitlementResult struct {
	OperationID string
	LicenseID   string
	Version     uint64
	ValidFrom   time.Time
	ValidUntil  *time.Time
	Status      string
	// Secret is returned only to the server and must be encrypted before storage.
	Secret []byte
}

type LicensingProvider interface {
	CheckPolicy(context.Context, PolicyCheck) error
	Capabilities(context.Context) (LicensingCapabilities, error)
	Apply(context.Context, EntitlementCommand) (EntitlementResult, error)
	LookupOperation(context.Context, string) (EntitlementResult, error)
}

// PolicyDefinition is the authoritative entitlement configuration read from
// Licentivo. Launch stores a snapshot with each local sales-plan version so
// order history remains reproducible, but administrators never configure
// these licensing values twice.
type PolicyDefinition struct {
	ID               string   `json:"id"`
	ProductID        string   `json:"product_id"`
	Name             string   `json:"name"`
	Mode             string   `json:"mode"`
	DurationDays     *int     `json:"duration_days"`
	MaxDevices       uint64   `json:"max_devices"`
	Features         []string `json:"features"`
	HeartbeatSeconds uint64   `json:"heartbeat_seconds"`
	OfflineMode      string   `json:"offline_mode"`
	OfflineSeconds   uint64   `json:"offline_seconds"`
	Version          uint64   `json:"version"`
	LicenseModel     string   `json:"license_model"`
	SessionSeconds   uint64   `json:"session_seconds"`
	SelfReleaseLimit uint64   `json:"self_release_limit"`
}

// PolicyCatalogProvider is implemented by Licentivo management clients that
// can read the complete policy and its capability extension.
type PolicyCatalogProvider interface {
	PolicyDefinition(context.Context, string) (PolicyDefinition, error)
}

// ProductPublicKeyProvider reads only the mapped product's signing public keys.
// API credentials and signing private keys never enter application integrations.
type ProductPublicKeyProvider interface {
	ProductPublicKeys(context.Context, string) (map[string]string, error)
}

type PolicyCheck struct {
	ProductID     string          `json:"product_id"`
	PolicyID      string          `json:"policy_id"`
	PolicyVersion uint64          `json:"policy_version"`
	SnapshotHash  string          `json:"snapshot_hash"`
	Snapshot      json.RawMessage `json:"snapshot"`
	LicenseID     string          `json:"license_id,omitempty"`
	PurchaseType  string          `json:"purchase_type,omitempty"`
	TermUnit      string          `json:"term_unit,omitempty"`
	TermCount     uint64          `json:"term_count,omitempty"`
}
