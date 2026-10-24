package ports

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const LaunchBridgeVersion = "licentivo-launch-account-v1"

type LaunchConnection struct {
	ID               string `json:"id"`
	Issuer           string `json:"issuer"`
	Audience         string `json:"audience"`
	Environment      string `json:"environment"`
	Enabled          bool   `json:"enabled"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	OfflineSeconds   int    `json:"offline_seconds"`
	SessionSeconds   int    `json:"session_seconds"`
}
type LaunchConnectionInput struct {
	Issuer           string            `json:"issuer"`
	Audience         string            `json:"audience"`
	Environment      string            `json:"environment"`
	PublicKeys       map[string]string `json:"public_keys"`
	Enabled          bool              `json:"enabled"`
	HeartbeatSeconds int               `json:"heartbeat_seconds,omitempty"`
	OfflineSeconds   int               `json:"offline_seconds"`
	SessionSeconds   int               `json:"session_seconds,omitempty"`
}
type LaunchPlanMapping struct {
	LaunchProductID string `json:"launch_product_id"`
	ProductID       string `json:"product_id"`
	PolicyID        string `json:"policy_id"`
	PolicyVersion   uint64 `json:"policy_version"`
}

// CommerceSnapshot is Launch's absolute purchase state, never an SDK grant.
type CommerceSnapshot struct {
	EventID         string     `json:"event_id"`
	Environment     string     `json:"environment"`
	Type            string     `json:"type"`
	EntitlementRef  string     `json:"entitlement_ref"`
	AccountRef      string     `json:"account_ref"`
	LaunchProductID string     `json:"launch_product_id"`
	PlanRef         string     `json:"plan_ref"`
	Version         uint64     `json:"version"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until"`
	LegacyLicenseID string     `json:"legacy_license_id,omitempty"`
}

var launchReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s CommerceSnapshot) Status() string {
	switch s.Type {
	case "entitlement.created", "entitlement.renewed", "entitlement.upgraded", "entitlement.resumed", "entitlement.migrated":
		return "active"
	case "entitlement.suspended":
		return "suspended"
	case "entitlement.expired":
		return "expired"
	case "entitlement.refunded":
		return "revoked"
	}
	return ""
}

func (s CommerceSnapshot) Validate() error {
	if !uuidPattern.MatchString(s.EventID) || !uuidPattern.MatchString(s.LaunchProductID) || !launchReferencePattern.MatchString(s.EntitlementRef) || !launchReferencePattern.MatchString(s.AccountRef) || !launchReferencePattern.MatchString(s.PlanRef) || s.Version == 0 || s.Status() == "" || s.ValidFrom.IsZero() || s.ValidFrom.Year() < 1970 || s.ValidFrom.Year() > 9999 || (s.Environment != "sandbox" && s.Environment != "live") || (s.ValidUntil != nil && (!s.ValidUntil.After(s.ValidFrom) || s.ValidUntil.Year() > 9999)) || (s.LegacyLicenseID != "" && (s.Type != "entitlement.migrated" || !uuidPattern.MatchString(s.LegacyLicenseID))) {
		return errors.New("invalid Launch purchase snapshot")
	}
	return nil
}

type LaunchEventStatus struct {
	ConnectionID string `json:"connection_id"`
	EventID      string `json:"event_id"`
	Status       string `json:"status"`
	Attempts     uint64 `json:"attempts"`
	ErrorCode    string `json:"error_code,omitempty"`
}
type LaunchDeviceOperation struct {
	ID                 string `json:"id"`
	OperationID        string `json:"operation_id"`
	Status             string `json:"status"`
	ActivationID       string `json:"activation_id"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}
type LaunchDevices struct {
	Items      []Device                `json:"items"`
	Remaining  uint64                  `json:"remaining"`
	CanRelease bool                    `json:"can_release"`
	Operations []LaunchDeviceOperation `json:"operations"`
}
type LaunchDeviceRelease struct {
	OperationID        string `json:"operation_id"`
	AccountRef         string `json:"account_ref"`
	EntitlementRef     string `json:"entitlement_ref"`
	Environment        string `json:"environment"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}
type LaunchBridge interface {
	Connections(context.Context) ([]LaunchConnection, error)
	SaveConnection(context.Context, string, LaunchConnectionInput) (LaunchConnection, error)
	MapPlan(context.Context, string, string, LaunchPlanMapping) error
	Deliver(context.Context, string, CommerceSnapshot) (LaunchEventStatus, error)
	Event(context.Context, string, string) (LaunchEventStatus, error)
	RetryEvent(context.Context, string, string) error
	Devices(context.Context, string, string, string, string) (LaunchDevices, error)
	Release(context.Context, string, string, LaunchDeviceRelease) (LaunchDeviceOperation, error)
	Operation(context.Context, string) (LaunchDeviceOperation, error)
}

type SessionRevocation struct {
	EventID     string `json:"event_id"`
	Environment string `json:"environment,omitempty"`
	AccountRef  string `json:"account_ref"`
	Scope       string `json:"scope"`
	SessionID   string `json:"sid,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type SessionRevocationReceipt struct {
	ConnectionID string `json:"connection_id"`
	SessionRevocation
	Status             string     `json:"status"`
	RevokedSessions    int64      `json:"revoked_sessions"`
	RevokedCredentials int64      `json:"revoked_credentials"`
	ReleasedDevices    int64      `json:"released_devices"`
	RevokedBefore      *time.Time `json:"revoked_before,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        time.Time  `json:"completed_at"`
}

func (r SessionRevocation) Validate() error {
	if !uuidPattern.MatchString(r.EventID) || !launchReferencePattern.MatchString(r.AccountRef) || (r.Environment != "sandbox" && r.Environment != "live") {
		return errors.New("invalid revocation scope")
	}
	if (r.Scope != "session" && r.Scope != "account") || (r.Scope == "session" && !launchReferencePattern.MatchString(r.SessionID)) || (r.Scope == "account" && r.SessionID != "") {
		return errors.New("invalid login revocation")
	}
	if r.Reason != "" && !(r.Reason == "forced_logout" && r.Scope == "session") && !((r.Reason == "password_changed" || r.Reason == "account_disabled") && r.Scope == "account") {
		return errors.New("invalid revocation reason")
	}
	return nil
}

type LaunchRevocationBridge interface {
	RevokeSessions(context.Context, string, SessionRevocation) (SessionRevocationReceipt, error)
	SessionRevocation(context.Context, string, string) (SessionRevocationReceipt, error)
}
