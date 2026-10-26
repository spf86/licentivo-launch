package licentivo

import (
	"context"
	"licentivo-launch/internal/ports"
	"net/url"
	"regexp"
)

type BridgeClient struct{ client *Client }

var launchRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func NewBridgeClient(origin, key string, allowLocalHTTP bool) (*BridgeClient, error) {
	c, e := NewClient(ClientConfig{BaseURL: origin, APIKey: key, AllowHTTP: allowLocalHTTP, AllowPrivate: allowLocalHTTP})
	if e != nil {
		return nil, e
	}
	return &BridgeClient{c}, nil
}
func (c *BridgeClient) Connections(ctx context.Context) ([]ports.LaunchConnection, error) {
	var out struct {
		Items []ports.LaunchConnection `json:"items"`
	}
	e := c.client.call(ctx, "GET", "/api/v1/launch/connections", nil, &out)
	return out.Items, e
}
func (c *BridgeClient) SaveConnection(ctx context.Context, id string, in ports.LaunchConnectionInput) (ports.LaunchConnection, error) {
	method, p := "POST", "/api/v1/launch/connections"
	if id != "" {
		if !uuidPattern.MatchString(id) {
			return ports.LaunchConnection{}, ErrConflict
		}
		method, p = "PUT", p+"/"+id
	}
	var out ports.LaunchConnection
	e := c.client.call(ctx, method, p, in, &out)
	if e == nil && (!uuidPattern.MatchString(out.ID) || out.Issuer != in.Issuer || out.Audience != in.Audience || out.Environment != in.Environment) {
		e = ErrConflict
	}
	return out, e
}
func (c *BridgeClient) MapPlan(ctx context.Context, connection, plan string, in ports.LaunchPlanMapping) error {
	if !uuidPattern.MatchString(connection) || !launchRef.MatchString(plan) {
		return ErrConflict
	}
	var out struct {
		Saved bool `json:"saved"`
	}
	e := c.client.call(ctx, "PUT", "/api/v1/launch/connections/"+connection+"/plans/"+plan, in, &out)
	if e == nil && !out.Saved {
		return ErrConflict
	}
	return e
}
func (c *BridgeClient) Deliver(ctx context.Context, connection string, in ports.CommerceSnapshot) (ports.LaunchEventStatus, error) {
	if !uuidPattern.MatchString(connection) || in.Validate() != nil {
		return ports.LaunchEventStatus{}, ErrConflict
	}
	var out ports.LaunchEventStatus
	e := c.client.call(ctx, "POST", "/api/v1/launch/connections/"+connection+"/events", in, &out)
	if e == nil && (out.ConnectionID != connection || out.EventID != in.EventID) {
		e = ErrConflict
	}
	return out, e
}
func (c *BridgeClient) Event(ctx context.Context, connection, id string) (ports.LaunchEventStatus, error) {
	if !uuidPattern.MatchString(connection) || !uuidPattern.MatchString(id) {
		return ports.LaunchEventStatus{}, ErrConflict
	}
	var out ports.LaunchEventStatus
	e := c.client.call(ctx, "GET", "/api/v1/launch/connections/"+connection+"/events/"+id, nil, &out)
	if e == nil && (out.ConnectionID != connection || out.EventID != id) {
		e = ErrConflict
	}
	return out, e
}
func (c *BridgeClient) RetryEvent(ctx context.Context, connection, id string) error {
	if !uuidPattern.MatchString(connection) || !uuidPattern.MatchString(id) {
		return ErrConflict
	}
	var out struct {
		Queued bool `json:"queued"`
	}
	if err := c.client.call(ctx, "POST", "/api/v1/launch/connections/"+connection+"/events/"+id+"/retry", struct{}{}, &out); err != nil {
		return err
	}
	if !out.Queued {
		return ErrConflict
	}
	return nil
}
func (c *BridgeClient) Devices(ctx context.Context, connection, entitlement, account, environment string) (ports.LaunchDevices, error) {
	if !uuidPattern.MatchString(connection) || !launchRef.MatchString(entitlement) || !launchRef.MatchString(account) || (environment != "live" && environment != "sandbox") {
		return ports.LaunchDevices{}, ErrConflict
	}
	q := url.Values{"account_ref": {account}, "environment": {environment}}
	var out ports.LaunchDevices
	e := c.client.call(ctx, "GET", "/api/v1/launch/connections/"+connection+"/entitlements/"+entitlement+"/devices?"+q.Encode(), nil, &out)
	return out, e
}
func (c *BridgeClient) Release(ctx context.Context, connection, device string, in ports.LaunchDeviceRelease) (ports.LaunchDeviceOperation, error) {
	if !uuidPattern.MatchString(connection) || !uuidPattern.MatchString(device) || !uuidPattern.MatchString(in.OperationID) || in.ExpectedGeneration < 1 {
		return ports.LaunchDeviceOperation{}, ErrConflict
	}
	var out ports.LaunchDeviceOperation
	e := c.client.call(ctx, "POST", "/api/v1/launch/connections/"+connection+"/devices/"+device+"/release", in, &out)
	if e == nil && out.OperationID != in.OperationID {
		e = ErrConflict
	}
	return out, e
}
func (c *BridgeClient) Operation(ctx context.Context, id string) (ports.LaunchDeviceOperation, error) {
	if !uuidPattern.MatchString(id) {
		return ports.LaunchDeviceOperation{}, ErrConflict
	}
	var out ports.LaunchDeviceOperation
	e := c.client.call(ctx, "GET", "/api/v1/launch/device-operations/"+id, nil, &out)
	if e == nil && out.OperationID != id {
		e = ErrConflict
	}
	return out, e
}

func (c *BridgeClient) RevokeSessions(ctx context.Context, connection string, in ports.SessionRevocation) (ports.SessionRevocationReceipt, error) {
	if !uuidPattern.MatchString(connection) || in.Validate() != nil {
		return ports.SessionRevocationReceipt{}, ErrConflict
	}
	var out ports.SessionRevocationReceipt
	err := c.client.call(ctx, "POST", "/api/v1/launch/connections/"+connection+"/session-revocations", in, &out)
	if err == nil && (!validRevocationReceipt(out, connection, in.EventID) || out.SessionRevocation != in) {
		err = ErrConflict
	}
	return out, err
}

func (c *BridgeClient) SessionRevocation(ctx context.Context, connection, event string) (ports.SessionRevocationReceipt, error) {
	if !uuidPattern.MatchString(connection) || !uuidPattern.MatchString(event) {
		return ports.SessionRevocationReceipt{}, ErrConflict
	}
	var out ports.SessionRevocationReceipt
	err := c.client.call(ctx, "GET", "/api/v1/launch/connections/"+connection+"/session-revocations/"+event, nil, &out)
	if err == nil && !validRevocationReceipt(out, connection, event) {
		err = ErrConflict
	}
	return out, err
}

func validRevocationReceipt(out ports.SessionRevocationReceipt, connection, event string) bool {
	return out.ConnectionID == connection && out.EventID == event && out.Status == "succeeded" && out.SessionRevocation.Validate() == nil && out.RevokedSessions >= 0 && out.RevokedCredentials >= 0 && out.ReleasedDevices >= 0 && !out.CreatedAt.IsZero() && !out.CompletedAt.IsZero() && (out.Scope != "account" || out.RevokedBefore != nil && !out.RevokedBefore.IsZero())
}
