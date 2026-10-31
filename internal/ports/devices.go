package ports

import (
	"context"
	"time"
)

type Device struct {
	ID             string     `json:"id"`
	LicenseID      string     `json:"license_id"`
	Name           string     `json:"device_name"`
	Alias          string     `json:"device_alias,omitempty"`
	Code           string     `json:"device_code,omitempty"`
	Platform       string     `json:"platform,omitempty"`
	OSVersion      string     `json:"os_version,omitempty"`
	CPU            string     `json:"cpu,omitempty"`
	MemoryMB       int64      `json:"memory_mb,omitempty"`
	GPU            string     `json:"gpu,omitempty"`
	SystemType     string     `json:"system_type,omitempty"`
	StorageGB      int64      `json:"storage_gb,omitempty"`
	AppVersion     string     `json:"app_version,omitempty"`
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
