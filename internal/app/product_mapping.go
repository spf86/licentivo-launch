package app

import (
	"gorm.io/gorm"
	"licentivo-launch/internal/storage"
)

func productMappingConflict() error {
	return problem(409, "PRODUCT_MAPPING_CONFLICT", "同一 Launch 产品必须绑定同一 Licentivo 产品，请核对已有套餐映射")
}

// productRemoteID resolves public routing metadata. Existing purchases and
// published versions take precedence over drafts in databases predating the
// stable product binding. Ambiguous history requires explicit reconciliation.
// excludeDraft is only used while replacing that draft's mapping.
func productRemoteID(tx *gorm.DB, product storage.Product, excludeDraft string) (string, error) {
	if product.LicentivoProductID != nil {
		if !integrationUUIDPattern.MatchString(*product.LicentivoProductID) {
			return "", productMappingConflict()
		}
		return *product.LicentivoProductID, nil
	}
	base := func() *gorm.DB {
		return tx.Table("plan_license_mappings m").Joins("JOIN plan_versions v ON v.id = m.plan_version_id").Where("v.product_id = ?", product.ID).Distinct("m.remote_product_id")
	}
	queries := []*gorm.DB{
		base().Joins("JOIN orders o ON o.plan_version_id = v.id").Where("o.status IN ?", []string{"paid", "refunding", "refunded", "reversed"}),
		base().Where("v.published_at IS NOT NULL OR EXISTS (SELECT 1 FROM plan_publications p WHERE p.version_id = v.id)"),
	}
	drafts := base()
	if excludeDraft != "" {
		drafts = drafts.Where("v.id <> ?", excludeDraft)
	}
	queries = append(queries, drafts)
	for _, query := range queries {
		var ids []string
		if err := query.Pluck("m.remote_product_id", &ids).Error; err != nil {
			return "", err
		}
		if len(ids) > 1 || len(ids) == 1 && !integrationUUIDPattern.MatchString(ids[0]) {
			return "", productMappingConflict()
		}
		if len(ids) == 1 {
			return ids[0], nil
		}
	}
	return "", nil
}
