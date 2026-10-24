package app

import (
	"context"
	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func bridgePending() error {
	return problem(503, "LICENTIVO_BRIDGE_UNAVAILABLE", "Licentivo 账号授权连接暂不可用，请稍后重试或联系商家")
}
func (s *Service) bridgeWithKey(key string) (ports.LaunchBridge, error) {
	if s.config.BridgeFactory != nil {
		return s.config.BridgeFactory(key)
	}
	return licentivo.NewBridgeClient(LicentivoServiceURL, key, false)
}
func (s *Service) launchBridge(ctx context.Context) (ports.LaunchBridge, error) {
	var c storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&c, "contract_version = ?", ports.LaunchBridgeVersion).Error != nil {
		return nil, bridgePending()
	}
	return s.bridgeForConnection(ctx, c.ID)
}

func (s *Service) bridgeForConnection(ctx context.Context, id string) (ports.LaunchBridge, error) {
	var c storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&c, "id = ? AND contract_version = ?", id, ports.LaunchBridgeVersion).Error != nil {
		return nil, bridgePending()
	}
	key, e := s.readSecret(ctx, c.CredentialSecretID)
	if e != nil {
		return nil, e
	}
	return s.bridgeWithKey(string(key))
}

func purchaseReference(e storage.Entitlement) string {
	if e.LaunchPurchaseRef != nil {
		return *e.LaunchPurchaseRef
	}
	return e.ID
}
