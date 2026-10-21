package app

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
	"strings"
)

func (s *Service) RecoveryRequired(ctx context.Context) bool {
	var v storage.InstanceSettings
	return s.db.WithContext(ctx).First(&v, "id = 1").Error != nil || v.RecoveryRequired
}
func (s *Service) FinishRecovery(ctx context.Context, p Principal, password, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 10 || len(reason) > 500 {
		return problem(400, "RECONCILIATION_REQUIRED", "请记录备份时刻以后支付、退款和授权的核对结果")
	}
	if !security.VerifyPassword(p.User.PasswordHash, password) {
		return problem(401, "PASSWORD_INCORRECT", "当前密码不正确")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		if !settings.RecoveryRequired {
			return nil
		}
		if err := tx.Model(&settings).Update("recovery_required", false).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "recovery.completed", "instance", "1", reason)
	}))
}
