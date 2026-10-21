package ports

import (
	"context"
	"time"
)

type Device struct {
	ID             string     `json:"id"`
	LicenseID      string     `json:"license_id"`
	Name           string     `json:"device_name"`
	Status         string     `json:"status"`
	Generation     uint64     `json:"generation"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
}
type DeviceRelease struct {
	OperationID        string `json:"operation_id"`
	LicenseID          string `json:"license_id"`
	DeviceID           string `json:"activation_id"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}
type DeviceProvider interface {
	Devices(context.Context, string) ([]Device, error)
	ReleaseDevice(context.Context, DeviceRelease) error
}
