package app

import (
	"context"
	"encoding/hex"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func checkPolicy(ctx context.Context, provider ports.LicensingProvider, version storage.PlanVersion, mapping storage.PlanLicenseMapping, license string) error {
	check := ports.PolicyCheck{ProductID: mapping.RemoteProductID, PolicyID: mapping.RemotePolicyID, PolicyVersion: mapping.RemotePolicyVersion, SnapshotHash: hex.EncodeToString(version.SnapshotHash), Snapshot: version.EntitlementSnapshot, LicenseID: license, PurchaseType: version.PurchaseType}
	if version.TermUnit != nil {
		check.TermUnit = *version.TermUnit
	}
	if version.TermCount != nil {
		check.TermCount = *version.TermCount
	}
	if err := provider.CheckPolicy(ctx, check); err != nil {
		return problem(409, "POLICY_PREFLIGHT_FAILED", "所选 Licentivo 策略与套餐权益不匹配，请重新选择")
	}
	return nil
}
