package ports

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const ExternalLicensingVersion = "launch-external-licensing-v1"

var ExternalReference = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type ExternalCapabilities struct {
	Protocol         string   `json:"protocol"`
	ConnectionRef    string   `json:"connection_ref"`
	Environment      string   `json:"environment"`
	PurchaseTypes    []string `json:"purchase_types"`
	DeliveryTypes    []string `json:"delivery_types"`
	Devices          bool     `json:"devices"`
	IdentityExchange bool     `json:"identity_exchange"`
}

func (c ExternalCapabilities) Validate() error {
	if c.Protocol != ExternalLicensingVersion || !ExternalReference.MatchString(c.ConnectionRef) || (c.Environment != "sandbox" && c.Environment != "live") || len(c.PurchaseTypes) == 0 || len(c.DeliveryTypes) == 0 || c.Devices || c.IdentityExchange {
		return errors.New("unsupported external licensing capabilities")
	}
	for _, t := range c.PurchaseTypes {
		if t != "perpetual" && t != "fixed_term" {
			return errors.New("invalid purchase type")
		}
	}
	for _, t := range c.DeliveryTypes {
		if t != "license_key" && t != "activation_url" && t != "instructions" {
			return errors.New("invalid delivery type")
		}
	}
	return nil
}

type ExternalSnapshot struct {
	Schema              string     `json:"schema"`
	EventID             string     `json:"event_id"`
	ConnectionRef       string     `json:"connection_ref"`
	Environment         string     `json:"environment"`
	Type                string     `json:"type"`
	ChangeReason        string     `json:"change_reason"`
	AccountRef          string     `json:"account_ref"`
	EntitlementRef      string     `json:"entitlement_ref"`
	LaunchProductID     string     `json:"launch_product_id"`
	LaunchPlanVersionID string     `json:"launch_plan_version_id"`
	ProductRef          string     `json:"product_ref"`
	PlanRef             string     `json:"plan_ref"`
	Version             uint64     `json:"version"`
	DesiredState        string     `json:"desired_state"`
	ValidFrom           time.Time  `json:"valid_from"`
	ValidUntil          *time.Time `json:"valid_until"`
}

func (s ExternalSnapshot) Validate() error {
	if s.Schema != ExternalLicensingVersion || s.Type != "entitlement.snapshot" || !uuidPattern.MatchString(s.EventID) || !uuidPattern.MatchString(s.LaunchProductID) || !uuidPattern.MatchString(s.LaunchPlanVersionID) || !ExternalReference.MatchString(s.ConnectionRef) || !ExternalReference.MatchString(s.ProductRef) || !ExternalReference.MatchString(s.PlanRef) || !launchReferencePattern.MatchString(s.AccountRef) || !launchReferencePattern.MatchString(s.EntitlementRef) || s.Version == 0 || (s.Environment != "sandbox" && s.Environment != "live") || s.ValidFrom.IsZero() || s.ValidFrom.Year() < 1970 || s.ValidFrom.Year() > 9999 || (s.ValidUntil != nil && (!s.ValidUntil.After(s.ValidFrom) || s.ValidUntil.Year() > 9999)) {
		return errors.New("invalid external purchase snapshot")
	}
	switch s.DesiredState {
	case "active", "suspended", "expired", "revoked":
	default:
		return errors.New("invalid desired state")
	}
	switch s.ChangeReason {
	case "purchase", "renewal", "upgrade", "refund", "reversal", "suspension", "resumption", "expiry", "reconciliation":
	default:
		return errors.New("invalid change reason")
	}
	return nil
}

type ExternalReceipt struct {
	EventID        string  `json:"event_id"`
	ConnectionRef  string  `json:"connection_ref"`
	Environment    string  `json:"environment"`
	EntitlementRef string  `json:"entitlement_ref"`
	Version        uint64  `json:"version"`
	PayloadSHA256  string  `json:"payload_sha256"`
	Status         string  `json:"status"`
	ErrorCode      *string `json:"error_code"`
}
type ExternalDeliveryValue struct {
	Type      string     `json:"type"`
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type ExternalDelivery struct {
	ConnectionRef  string                 `json:"connection_ref"`
	Environment    string                 `json:"environment"`
	EntitlementRef string                 `json:"entitlement_ref"`
	Version        uint64                 `json:"version"`
	Status         string                 `json:"status"`
	Delivery       *ExternalDeliveryValue `json:"delivery"`
}

// ExternalLicensing transports commercial state, never runtime permission.
type ExternalLicensing interface {
	Capabilities(context.Context) (ExternalCapabilities, error)
	Submit(context.Context, ExternalSnapshot) (ExternalReceipt, error)
	Event(context.Context, string) (ExternalReceipt, error)
	Delivery(context.Context, string) (ExternalDelivery, error)
}
