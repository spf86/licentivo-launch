package licentivo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"licentivo-launch/internal/ports"
)

var (
	ErrUnavailable = errors.New("licensing service unavailable")
	ErrCredentials = errors.New("licensing credentials rejected")
	ErrConflict    = errors.New("licensing operation conflicts with remote state")
)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Client struct {
	base, key string
	http      *http.Client
}
type ClientConfig struct {
	BaseURL, APIKey         string
	AllowPrivate, AllowHTTP bool
}

func NewClient(config ClientConfig) (*Client, error) {
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(config.AllowHTTP && u.Scheme == "http")) || !strings.HasPrefix(config.APIKey, "lv_api_") || len(config.APIKey) > 100 || strings.ContainsAny(config.APIKey, " \r\n\t") {
		return nil, ErrCredentials
	}
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrUnavailable
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !config.AllowPrivate && !publicIP(ip) {
				return nil, ErrUnavailable
			}
		}
		var dialErr error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			dialErr = err
		}
		if dialErr != nil {
			return nil, dialErr
		}
		return nil, ErrUnavailable
	}
	return &Client{base: strings.TrimRight(config.BaseURL, "/"), key: config.APIKey, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return ErrUnavailable
	}
	r.Header.Set("Authorization", "Bearer "+c.key)
	r.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(r)
	if err != nil {
		return transportError(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return transportError(err)
	}
	if len(data) > 1<<20 {
		return &APIError{Status: response.StatusCode, Code: "LICENTIVO_RESPONSE_INVALID", Message: "Licentivo response exceeds the supported size", cause: ErrUnavailable}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response.StatusCode, data, c.key)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(envelope.Data, out) != nil {
		return &APIError{Status: response.StatusCode, Code: "LICENTIVO_RESPONSE_INVALID", Message: "Licentivo returned an invalid response", cause: ErrUnavailable}
	}
	return nil
}
func (c *Client) Capabilities(ctx context.Context) (ports.LicensingCapabilities, error) {
	// Licentivo's public management API already provides idempotent batch
	// creation, renewal and policy application plus device release. Launch keeps
	// its own immutable sale ledger and adapts those concrete operations below.
	return ports.LicensingCapabilities{
		DeviceOperations: true, PreserveUsage: true, IdempotentRecovery: true,
		CompareAndSwap: true, ManagementAPI: true,
	}, nil
}

type nativeLicense struct {
	ID               string     `json:"id"`
	ProductID        string     `json:"product_id"`
	PolicyID         string     `json:"policy_id"`
	PolicyVersion    uint64     `json:"policy_version"`
	Mode             string     `json:"mode"`
	Status           string     `json:"status"`
	DurationDays     *int       `json:"duration_days"`
	FirstActivatedAt *time.Time `json:"first_activated_at"`
	ExpiresAt        *time.Time `json:"expires_at"`
	CreatedAt        time.Time  `json:"created_at"`
}
type nativePolicy struct {
	ID               string   `json:"id"`
	ProductID        string   `json:"product_id"`
	Name             string   `json:"name"`
	Mode             string   `json:"mode"`
	Version          uint64   `json:"version"`
	DurationDays     *int     `json:"duration_days"`
	MaxDevices       int      `json:"max_devices"`
	Features         []string `json:"features"`
	HeartbeatSeconds int      `json:"heartbeat_seconds"`
	OfflineMode      string   `json:"offline_mode"`
	OfflineSeconds   int      `json:"offline_seconds"`
}
type nativePolicyCapabilities struct {
	PolicyVersion    int    `json:"policy_version"`
	LicenseModel     string `json:"license_model"`
	SessionSeconds   int    `json:"session_seconds"`
	SelfReleaseLimit int    `json:"self_release_limit"`
}
type nativeBatchItem struct {
	ID         string `json:"id"`
	LicenseKey string `json:"license_key"`
	Operation  string `json:"operation"`
}
type nativeBatchResult struct {
	RequestID string            `json:"request_id"`
	Operation string            `json:"operation"`
	Items     []nativeBatchItem `json:"items"`
	Count     int               `json:"count"`
}

func derivedOperationID(id, purpose string) string {
	sum := sha256.Sum256([]byte(id + "\n" + purpose))
	b := sum[:16]
	b[6] = (b[6] & 15) | 80
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func (c *Client) license(ctx context.Context, id string) (nativeLicense, error) {
	var result nativeLicense
	if !uuidPattern.MatchString(id) {
		return result, ErrConflict
	}
	err := c.call(ctx, "GET", "/api/v1/licenses/"+id, nil, &result)
	return result, err
}
func (c *Client) policy(ctx context.Context, id string) (nativePolicy, error) {
	var result nativePolicy
	if !uuidPattern.MatchString(id) {
		return result, ErrConflict
	}
	err := c.call(ctx, "GET", "/api/v1/policies/"+id, nil, &result)
	return result, err
}

// PolicyDefinition reads both Licentivo's core policy and its capability
// extension. Those two official resources are the source of truth for all
// entitlement behavior shown by Launch.
func (c *Client) PolicyDefinition(ctx context.Context, id string) (ports.PolicyDefinition, error) {
	policy, err := c.policy(ctx, id)
	if err != nil {
		return ports.PolicyDefinition{}, err
	}
	var capabilities nativePolicyCapabilities
	if err := c.call(ctx, "GET", "/api/v1/policies/"+id+"/capabilities", nil, &capabilities); err != nil {
		return ports.PolicyDefinition{}, err
	}
	if policy.ID != id || !uuidPattern.MatchString(policy.ProductID) || policy.Version == 0 || capabilities.PolicyVersion != int(policy.Version) || policy.MaxDevices < 1 || policy.MaxDevices > 10000 || policy.HeartbeatSeconds < 0 || policy.OfflineSeconds < 0 || capabilities.SessionSeconds < 0 || capabilities.SelfReleaseLimit < 0 || capabilities.SelfReleaseLimit > 100 || (capabilities.LicenseModel != "node_locked" && capabilities.LicenseModel != "floating") {
		return ports.PolicyDefinition{}, ErrConflict
	}
	if policy.OfflineMode != "none" && policy.OfflineMode != "limited" && policy.OfflineMode != "permanent" {
		return ports.PolicyDefinition{}, ErrConflict
	}
	return ports.PolicyDefinition{
		ID: policy.ID, ProductID: policy.ProductID, Name: policy.Name, Mode: policy.Mode,
		DurationDays: policy.DurationDays, MaxDevices: uint64(policy.MaxDevices),
		Features: append([]string(nil), policy.Features...), HeartbeatSeconds: uint64(policy.HeartbeatSeconds),
		OfflineMode: policy.OfflineMode, OfflineSeconds: uint64(policy.OfflineSeconds), Version: policy.Version,
		LicenseModel: capabilities.LicenseModel, SessionSeconds: uint64(capabilities.SessionSeconds),
		SelfReleaseLimit: uint64(capabilities.SelfReleaseLimit),
	}, nil
}
func validateBatch(result nativeBatchResult, requestID, operation, licenseID string) (nativeBatchItem, error) {
	if result.RequestID != requestID || result.Operation != operation || result.Count != 1 || len(result.Items) != 1 || !uuidPattern.MatchString(result.Items[0].ID) || licenseID != "" && result.Items[0].ID != licenseID {
		return nativeBatchItem{}, ErrConflict
	}
	return result.Items[0], nil
}

type remoteResult struct {
	OperationID string     `json:"operation_id"`
	LicenseID   string     `json:"license_id"`
	Version     uint64     `json:"applied_version"`
	From        time.Time  `json:"valid_from"`
	Until       *time.Time `json:"valid_until"`
	Status      string     `json:"status"`
	Key         string     `json:"license_key"`
}

func (r remoteResult) view() ports.EntitlementResult {
	return ports.EntitlementResult{OperationID: r.OperationID, LicenseID: r.LicenseID, Version: r.Version, ValidFrom: r.From, ValidUntil: r.Until, Status: r.Status, Secret: []byte(r.Key)}
}
func (c *Client) Apply(ctx context.Context, command ports.EntitlementCommand) (ports.EntitlementResult, error) {
	if command.Validate() != nil {
		return ports.EntitlementResult{}, ErrConflict
	}
	licenseID := command.LicenseID
	var secret []byte
	if licenseID == "" {
		requestID := derivedOperationID(command.OperationID, "create")
		body := map[string]any{
			"request_id": requestID, "operation": "create",
			"product_id": command.ProductID, "policy_id": command.PolicyID,
			"entries": []map[string]string{{"customer": command.CustomerRef}},
		}
		var batch nativeBatchResult
		if err := c.call(ctx, "POST", "/api/v1/licenses/batch", body, &batch); err != nil {
			return ports.EntitlementResult{}, err
		}
		item, err := validateBatch(batch, requestID, "create", "")
		if err != nil || !strings.HasPrefix(item.LicenseKey, "lv_lic_") || len(item.LicenseKey) > 100 {
			return ports.EntitlementResult{}, ErrConflict
		}
		licenseID, secret = item.ID, []byte(item.LicenseKey)
	}
	license, err := c.license(ctx, licenseID)
	if err != nil || license.ProductID != command.ProductID || license.Status == "revoked" {
		return ports.EntitlementResult{}, ErrConflict
	}
	if license.PolicyID != command.PolicyID || license.PolicyVersion != command.PolicyVersion {
		if err := c.applyPolicy(ctx, command, license); err != nil {
			return ports.EntitlementResult{}, err
		}
		license, err = c.license(ctx, licenseID)
		if err != nil || license.PolicyID != command.PolicyID || license.PolicyVersion != command.PolicyVersion {
			return ports.EntitlementResult{}, ErrConflict
		}
	}
	license, err = c.ensureValidity(ctx, command, license)
	if err != nil {
		return ports.EntitlementResult{}, err
	}
	if err := c.ensureStatus(ctx, license, command.DesiredStatus); err != nil {
		return ports.EntitlementResult{}, err
	}
	return ports.EntitlementResult{OperationID: command.OperationID, LicenseID: licenseID, Version: command.TargetVersion, ValidFrom: command.ValidFrom, ValidUntil: command.ValidUntil, Status: command.DesiredStatus, Secret: secret}, nil
}
func (c *Client) LookupOperation(ctx context.Context, id string) (ports.EntitlementResult, error) {
	return ports.EntitlementResult{}, ports.ErrContractUnsupported
}

func (c *Client) applyPolicy(ctx context.Context, command ports.EntitlementCommand, current nativeLicense) error {
	var preview struct {
		PolicyVersion uint64 `json:"policy_version"`
		PreviewToken  string `json:"preview_token"`
	}
	if err := c.call(ctx, "POST", "/api/v1/licenses/policy-preview", map[string]any{"ids": []string{current.ID}, "policy_id": command.PolicyID}, &preview); err != nil {
		return err
	}
	if preview.PolicyVersion != command.PolicyVersion || preview.PreviewToken == "" {
		return ErrConflict
	}
	requestID := derivedOperationID(command.OperationID, "apply-policy")
	body := map[string]any{"request_id": requestID, "operation": "apply_policy", "ids": []string{current.ID}, "policy_id": command.PolicyID, "preview_token": preview.PreviewToken}
	var batch nativeBatchResult
	if err := c.call(ctx, "POST", "/api/v1/licenses/batch", body, &batch); err != nil {
		// The batch endpoint is idempotent, but the one-time preview token makes
		// byte-for-byte replay undesirable. A retry first reads the license and
		// skips this call when the previous response was lost after commit.
		return err
	}
	_, err := validateBatch(batch, requestID, "apply_policy", current.ID)
	return err
}

func ceilDays(span time.Duration) int {
	if span <= 0 {
		return 0
	}
	day := 24 * time.Hour
	return int((span + day - 1) / day)
}
func (c *Client) ensureValidity(ctx context.Context, command ports.EntitlementCommand, current nativeLicense) (nativeLicense, error) {
	if command.ValidUntil == nil || current.Mode == "perpetual" {
		return current, nil
	}
	days := 0
	if current.FirstActivatedAt == nil && current.ExpiresAt == nil {
		wanted := ceilDays(command.ValidUntil.Sub(command.ValidFrom))
		have := 0
		if current.DurationDays != nil {
			have = *current.DurationDays
		}
		if wanted > have {
			days = wanted - have
		}
	} else {
		base := time.Now().UTC()
		if current.ExpiresAt != nil && current.ExpiresAt.After(base) {
			base = *current.ExpiresAt
		}
		days = ceilDays(command.ValidUntil.Sub(base))
	}
	if days == 0 {
		return current, nil
	}
	if days > 36500 {
		return current, ErrConflict
	}
	requestID := derivedOperationID(command.OperationID, "renew")
	var batch nativeBatchResult
	if err := c.call(ctx, "POST", "/api/v1/licenses/batch", map[string]any{"request_id": requestID, "operation": "renew", "ids": []string{current.ID}, "days": days}, &batch); err != nil {
		return current, err
	}
	if _, err := validateBatch(batch, requestID, "renew", current.ID); err != nil {
		return current, err
	}
	return c.license(ctx, current.ID)
}

func (c *Client) ensureStatus(ctx context.Context, current nativeLicense, desired string) error {
	if desired == "suspended" && (current.Status == "suspended" || current.Status == "expired") || desired == "active" && current.Status == "active" {
		return nil
	}
	if current.Status == "revoked" {
		return ErrConflict
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, "PATCH", "/api/v1/licenses/"+current.ID, map[string]string{"status": desired}, &result); err != nil {
		return err
	}
	if result.ID != current.ID {
		return ErrConflict
	}
	observed, err := c.license(ctx, current.ID)
	if err != nil || desired == "active" && observed.Status != "active" || desired == "suspended" && observed.Status != "suspended" && observed.Status != "expired" {
		return ErrConflict
	}
	return nil
}

func (c *Client) CheckPolicy(ctx context.Context, in ports.PolicyCheck) error {
	if !uuidPattern.MatchString(in.ProductID) || !uuidPattern.MatchString(in.PolicyID) || in.PolicyVersion == 0 || len(in.SnapshotHash) != 64 || in.LicenseID != "" && !uuidPattern.MatchString(in.LicenseID) {
		return ErrConflict
	}
	policy, err := c.policy(ctx, in.PolicyID)
	if err != nil {
		return err
	}
	if policy.ID != in.PolicyID || policy.ProductID != in.ProductID || policy.Version != in.PolicyVersion {
		return ErrConflict
	}
	// A timed Licentivo policy may intentionally omit its default duration.
	// Launch owns the paid sales term and extends the created license to the
	// calculated contract end date, so an empty duration is valid here.
	if in.PurchaseType == "perpetual" && policy.Mode != "perpetual" || in.PurchaseType == "fixed_term" && (policy.Mode != "timed" || policy.DurationDays != nil && *policy.DurationDays < 1) {
		return ErrConflict
	}
	if in.LicenseID != "" {
		license, err := c.license(ctx, in.LicenseID)
		if err != nil || license.ProductID != in.ProductID || license.Status == "revoked" {
			return ErrConflict
		}
		if license.PolicyID != in.PolicyID || license.PolicyVersion != in.PolicyVersion {
			var preview struct {
				PolicyVersion uint64 `json:"policy_version"`
				PreviewToken  string `json:"preview_token"`
			}
			if err := c.call(ctx, "POST", "/api/v1/licenses/policy-preview", map[string]any{"ids": []string{license.ID}, "policy_id": in.PolicyID}, &preview); err != nil || preview.PolicyVersion != in.PolicyVersion || preview.PreviewToken == "" {
				return ErrConflict
			}
		}
	}
	return nil
}

// Existing management endpoints can connect even while sales capabilities are absent.
func (c *Client) Products(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "GET", "/api/v1/products?limit=100", nil, &result)
	return result, err
}

func (c *Client) ProductPublicKeys(ctx context.Context, productID string) (map[string]string, error) {
	if !uuidPattern.MatchString(productID) {
		return nil, ErrConflict
	}
	var bundle struct {
		ProductID string `json:"product_id"`
		Keys      []struct {
			ID        string `json:"key_id"`
			Algorithm string `json:"algorithm"`
			PublicKey string `json:"public_key"`
			Active    bool   `json:"active"`
		} `json:"keys"`
	}
	if err := c.call(ctx, "GET", "/api/v1/products/"+productID+"/signing-keys", nil, &bundle); err != nil {
		return nil, err
	}
	if bundle.ProductID != productID || len(bundle.Keys) == 0 || len(bundle.Keys) > 32 {
		return nil, ErrConflict
	}
	keys := make(map[string]string, len(bundle.Keys))
	active := false
	for _, key := range bundle.Keys {
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
		if key.ID == "" || len(key.ID) > 128 || strings.IndexFunc(key.ID, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || key.Algorithm != "Ed25519" || err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != key.PublicKey {
			return nil, ErrConflict
		}
		if _, duplicate := keys[key.ID]; duplicate {
			return nil, ErrConflict
		}
		keys[key.ID] = key.PublicKey
		active = active || key.Active
	}
	if !active {
		return nil, ErrConflict
	}
	return keys, nil
}

func (c *Client) Policies(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "GET", "/api/v1/policies?limit=100", nil, &result)
	return result, err
}
func (c *Client) LicenseState(ctx context.Context, id string) (ports.LicenseState, error) {
	license, err := c.license(ctx, id)
	if err != nil {
		return ports.LicenseState{}, err
	}
	from := license.CreatedAt
	if license.FirstActivatedAt != nil {
		from = *license.FirstActivatedAt
	}
	return ports.LicenseState{LicenseID: license.ID, ProductID: license.ProductID, Status: license.Status, PolicyID: license.PolicyID, PolicyVersion: license.PolicyVersion, ValidFrom: from, ValidUntil: license.ExpiresAt, NativeManagement: true}, nil
}

func (c *Client) Devices(ctx context.Context, license string) ([]ports.Device, error) {
	if !uuidPattern.MatchString(license) {
		return nil, ErrConflict
	}
	out := []ports.Device{}
	for offset := 0; offset < 10000; offset += 100 {
		var result struct {
			Items []ports.Device `json:"items"`
			Total int            `json:"total"`
		}
		if err := c.call(ctx, "GET", "/api/v1/activations?license_id="+license+"&limit=100&offset="+strconv.Itoa(offset), nil, &result); err != nil {
			return nil, err
		}
		for _, d := range result.Items {
			if d.LicenseID != license || !uuidPattern.MatchString(d.ID) {
				return nil, ErrConflict
			}
			out = append(out, d)
		}
		if offset+len(result.Items) >= result.Total {
			return out, nil
		}
		if len(result.Items) == 0 {
			return nil, ErrUnavailable
		}
	}
	return nil, ErrUnavailable
}
func (c *Client) ReleaseDevice(ctx context.Context, in ports.DeviceRelease) error {
	if !uuidPattern.MatchString(in.OperationID) || !uuidPattern.MatchString(in.LicenseID) || !uuidPattern.MatchString(in.DeviceID) {
		return ErrConflict
	}
	devices, err := c.Devices(ctx, in.LicenseID)
	if err != nil {
		return err
	}
	var selected *ports.Device
	for i := range devices {
		if devices[i].ID == in.DeviceID {
			selected = &devices[i]
			break
		}
	}
	if selected == nil || selected.LicenseID != in.LicenseID {
		return ErrConflict
	}
	if selected.Status == "released" {
		return nil
	}
	if selected.Status != "active" || selected.Generation != in.ExpectedGeneration {
		return ErrConflict
	}
	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := c.call(ctx, "POST", "/api/v1/activations/"+in.DeviceID+"/release", map[string]any{}, &result); err != nil {
		return err
	}
	if result.ID != in.DeviceID || result.Status != "released" {
		return ErrConflict
	}
	return nil
}
