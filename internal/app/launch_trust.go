package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

// Licentivo owns these connections and all runtime settings. Launch only
// registers identity trust and preserves remote runtime values on key updates.
func (s *Service) syncLaunchTrust(ctx context.Context, key string) error {
	peer, err := s.bridgeWithKey(key)
	if err != nil {
		return bridgePending()
	}
	rows, err := peer.Connections(ctx)
	if err != nil {
		return bridgePending()
	}
	keys := map[string]string{s.identityKeyID(): base64.RawURLEncoding.EncodeToString(s.identityPrivateKey().Public().(ed25519.PublicKey))}
	for _, environment := range []string{"sandbox", "live"} {
		var selected ports.LaunchConnection
		for _, row := range rows {
			if row.Issuer == s.PublicURL() && row.Audience == IdentityAudience && row.Environment == environment {
				if selected.ID != "" {
					return problem(409, "LAUNCH_TRUST_AMBIGUOUS", "Licentivo 中存在重复的账号信任连接，请先核对")
				}
				selected = row
			}
		}
		input := ports.LaunchConnectionInput{Issuer: s.PublicURL(), Audience: IdentityAudience, Environment: environment, PublicKeys: keys, Enabled: true}
		if selected.ID != "" {
			input.Enabled = selected.Enabled
			input.HeartbeatSeconds = selected.HeartbeatSeconds
			input.OfflineSeconds = selected.OfflineSeconds
			input.SessionSeconds = selected.SessionSeconds
		}
		remote, err := peer.SaveConnection(ctx, selected.ID, input)
		if err != nil {
			return bridgePending()
		}
		if remote.ID == "" || remote.Environment != environment || remote.Issuer != s.PublicURL() || remote.Audience != IdentityAudience {
			return ErrStorage
		}
		row := storage.LaunchTrustConnection{Environment: environment, RemoteID: remote.ID, Issuer: remote.Issuer, Audience: remote.Audience, UpdatedAt: now()}
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return ErrStorage
		}
	}
	return nil
}

func (s *Service) launchRouting(ctx context.Context, p Principal, product, environment string) (storage.Entitlement, storage.LaunchTrustConnection, error) {
	var purchase storage.Entitlement
	var trust storage.LaunchTrustConnection
	if err := s.rejectExternalRuntime(ctx, product, environment); err != nil {
		return purchase, trust, err
	}
	if !integrationUUIDPattern.MatchString(product) || (environment != "sandbox" && environment != "live") {
		return purchase, trust, ErrInvalid
	}
	err := s.db.WithContext(ctx).First(&purchase, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, product, environment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return purchase, trust, ErrNotFound
	}
	if err != nil {
		return purchase, trust, ErrStorage
	}
	if err := s.db.WithContext(ctx).First(&trust, "environment = ?", environment).Error; err != nil || trust.Issuer != s.PublicURL() {
		return purchase, trust, bridgePending()
	}
	// Purchase reference is routing context, never a local runtime permission.
	return purchase, trust, nil
}

// Register opaque commercial plan references. All policy compatibility and
// runtime rules are checked by Licentivo, never reconstructed by Launch.
func (s *Service) registerLaunchPlan(ctx context.Context, environment string, version storage.PlanVersion, mapping storage.PlanLicenseMapping, requireEnabled bool) error {
	if environment != "live" && environment != "sandbox" {
		return ErrInvalid
	}
	var trust storage.LaunchTrustConnection
	if s.db.WithContext(ctx).First(&trust, "environment = ?", environment).Error != nil || trust.Issuer != s.PublicURL() || trust.Audience != IdentityAudience {
		return bridgePending()
	}
	peer, err := s.bridgeForConnection(ctx, mapping.ConnectionID)
	if err != nil {
		return err
	}
	policy, err := s.mappingPolicy(ctx, mapping)
	if err != nil {
		return err
	}
	// Validate null versus absolute purchased expiry before accepting payment.
	// Device, feature and runtime permission remain entirely in Licentivo.
	if (version.PurchaseType == "perpetual") != (policy.Mode == "perpetual") {
		return problem(409, "PURCHASE_POLICY_MODE_MISMATCH", "销售套餐的永久或限时购买类型与 Licentivo 策略不匹配")
	}
	if requireEnabled {
		connections, err := peer.Connections(ctx)
		if err != nil {
			return bridgePending()
		}
		ready := false
		for _, remote := range connections {
			if remote.ID == trust.RemoteID && remote.Environment == environment && remote.Issuer == trust.Issuer && remote.Audience == trust.Audience && remote.Enabled {
				ready = true
				break
			}
		}
		if !ready {
			return problem(409, "LICENTIVO_CONNECTION_DISABLED", "请先在 Licentivo 启用对应环境的账号连接")
		}
	}
	if err := peer.MapPlan(ctx, trust.RemoteID, version.ID, ports.LaunchPlanMapping{LaunchProductID: version.ProductID, ProductID: mapping.RemoteProductID, PolicyID: mapping.RemotePolicyID, PolicyVersion: mapping.RemotePolicyVersion}); err != nil {
		return problem(409, "LICENSE_POLICY_CHANGED", "Licentivo 未接受套餐映射，请刷新策略目录并核对对应版本")
	}
	return nil
}

func (s *Service) mappingPolicy(ctx context.Context, mapping storage.PlanLicenseMapping) (ports.PolicyDefinition, error) {
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&connection, "id = ?", mapping.ConnectionID).Error != nil {
		return ports.PolicyDefinition{}, bridgePending()
	}
	provider, err := s.licensingProvider(ctx, connection)
	if err != nil {
		return ports.PolicyDefinition{}, bridgePending()
	}
	reader, ok := provider.(ports.PolicyCatalogProvider)
	if !ok {
		return ports.PolicyDefinition{}, problem(409, "LICENSE_POLICY_DETAILS_UNAVAILABLE", "无法读取 Licentivo 策略，请刷新连接后重试")
	}
	policy, err := reader.PolicyDefinition(ctx, mapping.RemotePolicyID)
	if err != nil || policy.ID != mapping.RemotePolicyID || policy.ProductID != mapping.RemoteProductID || policy.Version != mapping.RemotePolicyVersion {
		return ports.PolicyDefinition{}, problem(409, "LICENSE_POLICY_CHANGED", "Licentivo 策略已更新，请刷新目录并核对套餐版本")
	}
	return policy, nil
}

// Read-only policy description for storefront display and purchase history.
// No Launch permission check or identity assertion consumes these fields.
func policyDisplaySnapshot(policy ports.PolicyDefinition) PlanSnapshot {
	features := append([]string{}, policy.Features...)
	return PlanSnapshot{OfflineMode: policy.OfflineMode, HeartbeatSeconds: policy.HeartbeatSeconds, OfflineSeconds: policy.OfflineSeconds, SessionSeconds: policy.SessionSeconds, SelfReleaseLimit: policy.SelfReleaseLimit, Features: features, MaxDevices: policy.MaxDevices, LicenseModel: policy.LicenseModel}
}
