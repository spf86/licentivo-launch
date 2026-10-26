package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"time"
)

// HTTP application tests use fixed remote results. Actual asynchronous delivery,
// restart, ordering and signed SDK behavior have dedicated integration suites.
type accountBridgeDouble struct {
	connections                                []ports.LaunchConnection
	events                                     map[string]ports.CommerceSnapshot
	view                                       ports.LaunchDevices
	lastRelease                                ports.LaunchDeviceRelease
	lastAccount, lastPurchase, lastEnvironment string
	revocations                                map[string]ports.SessionRevocationReceipt
}

func (b *accountBridgeDouble) Connections(context.Context) ([]ports.LaunchConnection, error) {
	return b.connections, nil
}
func (b *accountBridgeDouble) SaveConnection(_ context.Context, id string, in ports.LaunchConnectionInput) (ports.LaunchConnection, error) {
	if id == "" {
		id, _ = security.ID()
	}
	out := ports.LaunchConnection{ID: id, Issuer: in.Issuer, Audience: in.Audience, Environment: in.Environment, Enabled: in.Enabled, HeartbeatSeconds: 600, SessionSeconds: 86400}
	for i, old := range b.connections {
		if old.ID == id {
			b.connections[i] = out
			return out, nil
		}
	}
	b.connections = append(b.connections, out)
	return out, nil
}
func (*accountBridgeDouble) MapPlan(context.Context, string, string, ports.LaunchPlanMapping) error {
	return nil
}
func (b *accountBridgeDouble) Deliver(_ context.Context, connection string, in ports.CommerceSnapshot) (ports.LaunchEventStatus, error) {
	if err := in.Validate(); err != nil {
		return ports.LaunchEventStatus{}, err
	}
	if b.events == nil {
		b.events = map[string]ports.CommerceSnapshot{}
	}
	if previous, ok := b.events[in.EventID]; ok {
		a, _ := json.Marshal(previous)
		c, _ := json.Marshal(in)
		if string(a) != string(c) {
			return ports.LaunchEventStatus{}, errors.New("changed event")
		}
	}
	b.events[in.EventID] = in
	return ports.LaunchEventStatus{ConnectionID: connection, EventID: in.EventID, Status: "applied"}, nil
}
func (*accountBridgeDouble) Event(_ context.Context, connection, event string) (ports.LaunchEventStatus, error) {
	return ports.LaunchEventStatus{ConnectionID: connection, EventID: event, Status: "applied"}, nil
}
func (*accountBridgeDouble) RetryEvent(context.Context, string, string) error { return nil }
func (b *accountBridgeDouble) Devices(_ context.Context, _ string, purchase, account, environment string) (ports.LaunchDevices, error) {
	b.lastAccount, b.lastPurchase, b.lastEnvironment = account, purchase, environment
	return b.view, nil
}
func (b *accountBridgeDouble) Release(_ context.Context, _ string, device string, in ports.LaunchDeviceRelease) (ports.LaunchDeviceOperation, error) {
	b.lastRelease = in
	return ports.LaunchDeviceOperation{OperationID: in.OperationID, ActivationID: device, ExpectedGeneration: in.ExpectedGeneration, Status: "pending"}, nil
}
func (*accountBridgeDouble) Operation(_ context.Context, id string) (ports.LaunchDeviceOperation, error) {
	return ports.LaunchDeviceOperation{OperationID: id, Status: "succeeded"}, nil
}

func (b *accountBridgeDouble) SessionRevocation(_ context.Context, connection, event string) (ports.SessionRevocationReceipt, error) {
	if out, ok := b.revocations[connection+":"+event]; ok {
		return out, nil
	}
	return ports.SessionRevocationReceipt{}, ports.ErrContractUnsupported
}
func (b *accountBridgeDouble) RevokeSessions(_ context.Context, connection string, in ports.SessionRevocation) (ports.SessionRevocationReceipt, error) {
	if err := in.Validate(); err != nil {
		return ports.SessionRevocationReceipt{}, err
	}
	if b.revocations == nil {
		b.revocations = map[string]ports.SessionRevocationReceipt{}
	}
	key := connection + ":" + in.EventID
	if previous, ok := b.revocations[key]; ok {
		if previous.SessionRevocation != in {
			return ports.SessionRevocationReceipt{}, errors.New("changed revocation")
		}
		return previous, nil
	}
	at := time.Now().UTC()
	out := ports.SessionRevocationReceipt{ConnectionID: connection, SessionRevocation: in, Status: "succeeded", CreatedAt: at, CompletedAt: at}
	if in.Scope == "account" {
		out.RevokedBefore = &at
	}
	b.revocations[key] = out
	return out, nil
}
