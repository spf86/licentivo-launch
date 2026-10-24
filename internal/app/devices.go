package app

import (
	"context"
	"licentivo-launch/internal/ports"
)

type DevicesView struct {
	Devices    []ports.Device        `json:"devices"`
	Remaining  uint64                `json:"remaining"`
	CanRelease bool                  `json:"can_release"`
	Operations []DeviceOperationView `json:"operations"`
}
type DeviceOperationView struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	ActivationID       string `json:"activation_id,omitempty"`
	ExpectedGeneration uint64 `json:"expected_generation,omitempty"`
}
type DeviceInput struct {
	DeviceID           string `json:"device_id"`
	IdempotencyKey     string `json:"idempotency_key"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}

func (s *Service) Devices(ctx context.Context, p Principal, product, environment string) (DevicesView, error) {
	purchase, trust, e := s.launchRouting(ctx, p, product, environment)
	if e != nil {
		return DevicesView{}, e
	}
	peer, e := s.launchBridge(ctx)
	if e != nil {
		return DevicesView{}, e
	}
	remote, e := peer.Devices(ctx, trust.RemoteID, purchaseReference(purchase), s.subject(p.User.ID), environment)
	if e != nil {
		return DevicesView{}, bridgePending()
	}
	operations := make([]DeviceOperationView, 0, len(remote.Operations))
	for _, op := range remote.Operations {
		id := op.OperationID
		if id == "" {
			id = op.ID
		}
		operations = append(operations, DeviceOperationView{ID: id, Status: op.Status, ActivationID: op.ActivationID, ExpectedGeneration: op.ExpectedGeneration})
	}
	if remote.Items == nil {
		remote.Items = []ports.Device{}
	}
	return DevicesView{remote.Items, remote.Remaining, remote.CanRelease, operations}, nil
}
func (s *Service) ReleaseDevice(ctx context.Context, p Principal, product, environment string, in DeviceInput) (DeviceOperationView, error) {
	if !integrationUUIDPattern.MatchString(in.DeviceID) || !integrationUUIDPattern.MatchString(in.IdempotencyKey) || in.ExpectedGeneration < 1 {
		return DeviceOperationView{}, ErrInvalid
	}
	purchase, trust, e := s.launchRouting(ctx, p, product, environment)
	if e != nil {
		return DeviceOperationView{}, e
	}
	peer, e := s.launchBridge(ctx)
	if e != nil {
		return DeviceOperationView{}, e
	}
	remote, e := peer.Release(ctx, trust.RemoteID, in.DeviceID, ports.LaunchDeviceRelease{OperationID: in.IdempotencyKey, AccountRef: s.subject(p.User.ID), EntitlementRef: purchaseReference(purchase), Environment: environment, ExpectedGeneration: in.ExpectedGeneration})
	if e != nil {
		return DeviceOperationView{}, bridgePending()
	}
	return DeviceOperationView{ID: remote.OperationID, Status: remote.Status, ActivationID: remote.ActivationID, ExpectedGeneration: remote.ExpectedGeneration}, nil
}

// Old local release jobs are historical records only. Licentivo owns execution.
func (s *Service) ProcessDeviceOperation(context.Context) (bool, error) { return false, nil }
