package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
	"time"
)

type DevicesView struct {
	Devices    []ports.Device        `json:"devices"`
	Remaining  uint64                `json:"remaining"`
	CanRelease bool                  `json:"can_release"`
	Operations []DeviceOperationView `json:"operations"`
}
type DeviceOperationView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
type DeviceInput struct {
	DeviceID       string `json:"device_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Service) ownedDeviceProvider(ctx context.Context, p Principal, product, environment string) (storage.Entitlement, storage.LicenseBinding, ports.LicensingProvider, error) {
	var e storage.Entitlement
	var binding storage.LicenseBinding
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&e, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, product, environment).Error != nil || s.db.WithContext(ctx).First(&binding, "entitlement_id = ? AND replaced_at IS NULL", e.ID).Error != nil {
		return e, binding, nil, ErrNotFound
	}
	if s.db.WithContext(ctx).First(&connection, "id = ?", binding.ConnectionID).Error != nil {
		return e, binding, nil, ErrStorage
	}
	provider, err := s.licensingProvider(ctx, connection)
	return e, binding, provider, err
}
func (s *Service) Devices(ctx context.Context, p Principal, product, environment string) (DevicesView, error) {
	e, binding, provider, err := s.ownedDeviceProvider(ctx, p, product, environment)
	if err != nil {
		return DevicesView{}, err
	}
	deviceProvider, ok := provider.(ports.DeviceProvider)
	if !ok {
		return DevicesView{}, problem(409, "DEVICE_CONTRACT_UNSUPPORTED", "授权服务暂不支持安全的设备自助操作")
	}
	devices, err := deviceProvider.Devices(ctx, binding.RemoteLicenseID)
	if err != nil {
		return DevicesView{}, problem(503, "DEVICE_QUERY_PENDING", "设备列表暂不可用，请稍后重试")
	}
	for _, d := range devices {
		if d.LicenseID != binding.RemoteLicenseID {
			return DevicesView{}, ErrStorage
		}
	}
	var rights PlanSnapshot
	if json.Unmarshal(e.Snapshot, &rights) != nil {
		return DevicesView{}, ErrStorage
	}
	var rows []storage.DeviceOperation
	if s.db.WithContext(ctx).Where("entitlement_id = ? AND created_at >= ?", e.ID, now().Add(-30*24*time.Hour)).Order("created_at DESC").Find(&rows).Error != nil {
		return DevicesView{}, ErrStorage
	}
	ops := make([]DeviceOperationView, 0, len(rows))
	for _, r := range rows {
		ops = append(ops, DeviceOperationView{r.ID, r.Status})
	}
	remaining := uint64(0)
	if rights.SelfReleaseLimit > uint64(len(rows)) {
		remaining = rights.SelfReleaseLimit - uint64(len(rows))
	}
	caps, err := provider.Capabilities(ctx)
	can := err == nil && caps.DeviceOperations && !e.ManualHold && e.Status != "revoked" && remaining > 0
	// Device objects deliberately omit hardware identifiers and private credentials.
	return DevicesView{devices, remaining, can, ops}, nil
}
func (s *Service) ReleaseDevice(ctx context.Context, p Principal, product, environment string, in DeviceInput) (DeviceOperationView, error) {
	if len(in.IdempotencyKey) < 16 || len(in.IdempotencyKey) > 80 || len(in.DeviceID) != 36 {
		return DeviceOperationView{}, ErrInvalid
	}
	e, binding, provider, err := s.ownedDeviceProvider(ctx, p, product, environment)
	if err != nil {
		return DeviceOperationView{}, err
	}
	var existing storage.DeviceOperation
	fingerprint := securityDigest(marshal(in))
	if err := s.db.WithContext(ctx).First(&existing, "entitlement_id = ? AND idempotency_key = ?", e.ID, in.IdempotencyKey).Error; err == nil {
		if !bytes.Equal(existing.RequestHash, fingerprint) {
			return DeviceOperationView{}, problem(409, "IDEMPOTENCY_CONFLICT", "请使用原设备操作参数")
		}
		return DeviceOperationView{existing.ID, existing.Status}, nil
	}
	caps, err := provider.Capabilities(ctx)
	if err != nil || !caps.DeviceOperations {
		return DeviceOperationView{}, problem(409, "DEVICE_CONTRACT_UNSUPPORTED", "授权服务尚未提供可恢复的设备操作")
	}
	deviceProvider, ok := provider.(ports.DeviceProvider)
	if !ok {
		return DeviceOperationView{}, ErrStorage
	}
	devices, err := deviceProvider.Devices(ctx, binding.RemoteLicenseID)
	if err != nil {
		return DeviceOperationView{}, problem(503, "DEVICE_QUERY_PENDING", "设备状态暂不可用")
	}
	var selected *ports.Device
	for i := range devices {
		d := &devices[i]
		if d.LicenseID != binding.RemoteLicenseID {
			return DeviceOperationView{}, ErrStorage
		}
		if d.ID == in.DeviceID && d.Status == "active" {
			selected = d
		}
	}
	if selected == nil {
		return DeviceOperationView{}, ErrNotFound
	}
	operation, err := newID()
	if err != nil {
		return DeviceOperationView{}, err
	}
	command := ports.DeviceRelease{OperationID: operation, LicenseID: binding.RemoteLicenseID, DeviceID: selected.ID, ExpectedGeneration: selected.Generation}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ? AND status = 'active'", p.User.ID).Error; err != nil {
			return ErrUnauthorized
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", e.ID).Error; err != nil {
			return err
		}
		if e.ManualHold || e.Status == "revoked" {
			return ErrForbidden
		}
		if err := tx.First(&existing, "entitlement_id = ? AND idempotency_key = ?", e.ID, in.IdempotencyKey).Error; err == nil {
			if !bytes.Equal(existing.RequestHash, fingerprint) {
				return problem(409, "IDEMPOTENCY_CONFLICT", "请使用原设备操作参数")
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var rights PlanSnapshot
		if json.Unmarshal(e.Snapshot, &rights) != nil {
			return ErrStorage
		}
		var used int64
		if err := tx.Model(&storage.DeviceOperation{}).Where("entitlement_id = ? AND created_at >= ?", e.ID, now().Add(-30*24*time.Hour)).Count(&used).Error; err != nil {
			return err
		}
		if uint64(used) >= rights.SelfReleaseLimit {
			return problem(409, "DEVICE_RELEASE_LIMIT", "最近 30 天的自助换机次数已用完，请联系商家")
		}
		existing = storage.DeviceOperation{ID: operation, EntitlementID: e.ID, IdempotencyKey: in.IdempotencyKey, RequestJSON: marshal(command), RequestHash: fingerprint, Status: "pending", CreatedAt: now(), AvailableAt: now()}
		if err := tx.Create(&existing).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "device.release_requested", "device_operation", existing.ID, "")
	})
	if err != nil {
		return DeviceOperationView{}, databaseError(err)
	}
	return DeviceOperationView{existing.ID, existing.Status}, nil
}
func (s *Service) ProcessDeviceOperation(ctx context.Context) (bool, error) {
	var op storage.DeviceOperation
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status = 'pending' AND available_at <= ?) OR (status = 'running' AND lease_until < ?)", now(), now()).Order("created_at").First(&op).Error; err != nil {
			return err
		}
		op.Attempts++
		return tx.Model(&op).Updates(map[string]any{"status": "running", "lease_token": lease, "lease_until": now().Add(time.Minute), "attempts": op.Attempts}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	var command ports.DeviceRelease
	var e storage.Entitlement
	var binding storage.LicenseBinding
	var connection storage.LicentivoConnection
	state := "pending"
	if json.Unmarshal(op.RequestJSON, &command) != nil || s.db.WithContext(ctx).First(&e, "id = ?", op.EntitlementID).Error != nil || s.db.WithContext(ctx).First(&binding, "entitlement_id = ? AND replaced_at IS NULL", e.ID).Error != nil || command.LicenseID != binding.RemoteLicenseID || s.db.WithContext(ctx).First(&connection, "id = ?", binding.ConnectionID).Error != nil {
		state = "manual_review"
	} else {
		provider, err := s.licensingProvider(ctx, connection)
		if dp, ok := provider.(ports.DeviceProvider); err == nil && ok {
			if err := dp.ReleaseDevice(ctx, command); err == nil {
				state = "succeeded"
			} else if errors.Is(err, ports.ErrContractUnsupported) {
				state = "manual_review"
			}
		}
	}
	if op.Attempts >= 12 && state == "pending" {
		state = "manual_review"
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return true, databaseError(s.db.WithContext(finish).Model(&op).Where("lease_token = ?", lease).Updates(map[string]any{"status": state, "lease_token": nil, "lease_until": nil, "available_at": now().Add(time.Duration(1<<min(op.Attempts, 10)) * time.Second)}).Error)
}
