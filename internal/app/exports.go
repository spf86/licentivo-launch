package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"licentivo-launch/internal/storage"
	"strings"
	"time"
)

type ExportView struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Rows     int    `json:"rows"`
}

func csvCell(v string) string {
	trimmed := strings.TrimLeft(v, " \t\r\n\ufeff")
	if strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@") || strings.HasPrefix(v, "\t") || strings.HasPrefix(v, "\r") {
		return "'" + v
	}
	return v
}
func utc(v time.Time) string { return v.UTC().Format(time.RFC3339Nano) }
func (s *Service) Export(ctx context.Context, p Principal, kind, reason string) (ExportView, error) {
	if !p.IsAdmin() {
		return ExportView{}, ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return ExportView{}, ErrInvalid
	}
	db := s.db.WithContext(ctx).Limit(20001)
	var rows [][]string
	switch kind {
	case "users":
		var data []storage.User
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "email", "status", "verified", "created_at"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.Email, v.Status, fmt.Sprint(v.EmailVerifiedAt != nil), utc(v.CreatedAt)})
		}
	case "orders":
		var data []storage.Order
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "user_id", "product_id", "plan_version_id", "environment", "action", "status", "currency", "amount_minor", "created_at"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.UserID, v.ProductID, v.PlanVersionID, v.Environment, v.Action, v.Status, v.Currency, fmt.Sprint(v.AmountMinor), utc(v.CreatedAt)})
		}
	case "refunds":
		var data []storage.Refund
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "payment_id", "operation_id", "status", "amount_minor", "reason", "created_at"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.PaymentID, v.OperationID, v.Status, fmt.Sprint(v.AmountMinor), v.Reason, utc(v.CreatedAt)})
		}
	case "plans":
		var data []storage.PlanVersion
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "plan_id", "version", "currency", "amount_minor", "purchase_type", "snapshot"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.PlanID, fmt.Sprint(v.Version), v.Currency, fmt.Sprint(v.AmountMinor), v.PurchaseType, string(v.EntitlementSnapshot)})
		}
	case "entitlements":
		var data []storage.Entitlement
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "user_id", "product_id", "environment", "status", "manual_hold", "desired_version", "applied_version", "snapshot"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.UserID, v.ProductID, v.Environment, v.Status, fmt.Sprint(v.ManualHold), fmt.Sprint(v.DesiredVersion), fmt.Sprint(v.AppliedVersion), string(v.Snapshot)})
		}
	case "bindings":
		var data []storage.LicenseBinding
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "entitlement_id", "connection_id", "remote_license_id", "created_at"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.EntitlementID, v.ConnectionID, v.RemoteLicenseID, utc(v.CreatedAt)})
		}
	case "devices":
		var data []storage.DeviceOperation
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "entitlement_id", "status", "created_at"})
		for _, v := range data {
			rows = append(rows, []string{v.ID, v.EntitlementID, v.Status, utc(v.CreatedAt)})
		}
	case "audit":
		var data []storage.AuditEntry
		if db.Order("id").Find(&data).Error != nil {
			return ExportView{}, ErrStorage
		}
		rows = append(rows, []string{"id", "actor_id", "action", "object_type", "object_id", "created_at"})
		for _, v := range data {
			actor := ""
			if v.ActorID != nil {
				actor = *v.ActorID
			}
			rows = append(rows, []string{v.ID, actor, v.Action, v.ObjectType, v.ObjectID, utc(v.CreatedAt)})
		}
	default:
		return ExportView{}, ErrInvalid
	}
	if len(rows) > 20001 {
		return ExportView{}, problem(409, "EXPORT_LIMIT", "数据超过网页导出上限，请使用加密备份工具保存完整数据")
	}
	var out bytes.Buffer
	out.WriteString("\ufeff")
	writer := csv.NewWriter(&out)
	writer.UseCRLF = true
	for _, row := range rows {
		for i := range row {
			row[i] = csvCell(row[i])
		}
		if err := writer.Write(row); err != nil {
			return ExportView{}, ErrStorage
		}
	}
	writer.Flush()
	if writer.Error() != nil || out.Len() > 16<<20 {
		return ExportView{}, ErrStorage
	}
	if err := audit(s.db.WithContext(ctx), p.User.ID, "data.exported", "export", kind, reason); err != nil {
		return ExportView{}, ErrStorage
	}
	return ExportView{"launch-" + kind + "-" + now().Format("20060102T150405Z") + ".csv", out.String(), len(rows) - 1}, nil
}
