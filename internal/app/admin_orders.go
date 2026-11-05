package app

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

type AdminOrderQuery struct {
	Query, Status, Environment string
	NeedsReview                bool
	Limit, Offset              int
}

type AdminOrderView struct {
	OrderView
	CustomerEmail     string `json:"customer_email"`
	FulfillmentReview bool   `json:"fulfillment_review"`
}

type AdminOrderPage struct {
	Items  []AdminOrderView `json:"items"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

// SearchAdminOrders searches the complete order history, rather than filtering
// the legacy endpoint's latest 100 orders in the browser.
func (s *Service) SearchAdminOrders(ctx context.Context, p Principal, in AdminOrderQuery) (AdminOrderPage, error) {
	if p.Role != "admin" && p.Role != "operations" && p.Role != "support" {
		return AdminOrderPage{}, ErrForbidden
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Limit == 0 {
		in.Limit = 25
	}
	if len(in.Query) > 254 || in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || in.Offset > 1000000 {
		return AdminOrderPage{}, ErrInvalid
	}
	switch in.Status {
	case "", "pending", "closed", "paid", "refunding", "refunded", "reversed":
	default:
		return AdminOrderPage{}, ErrInvalid
	}
	if in.Environment != "" && in.Environment != "sandbox" && in.Environment != "live" {
		return AdminOrderPage{}, ErrInvalid
	}
	query := func() *gorm.DB {
		q := s.db.WithContext(ctx).Table("orders AS o").
			Joins("JOIN users AS u ON u.id = o.user_id").
			Joins("JOIN products AS p ON p.id = o.product_id").
			Joins("JOIN plan_versions AS pv ON pv.id = o.plan_version_id").
			Joins("JOIN plans AS pl ON pl.id = pv.plan_id")
		if in.Query != "" {
			literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(in.Query))
			pattern := "%" + literal + "%"
			q = q.Where("(LOWER(CONVERT(o.id USING utf8mb4)) LIKE ? ESCAPE '!' OR u.email_key LIKE ? ESCAPE '!' OR LOWER(p.name) LIKE ? ESCAPE '!')", pattern, pattern, pattern)
		}
		if in.Status != "" {
			q = q.Where("o.status = ?", in.Status)
		}
		if in.Environment != "" {
			q = q.Where("o.environment = ?", in.Environment)
		}
		if in.NeedsReview {
			q = q.Where("(o.fulfillment_review = TRUE OR EXISTS (SELECT 1 FROM payments AS pay WHERE pay.order_id = o.id AND pay.status IN ('creating','pending','unknown')))")
		}
		return q
	}
	out := AdminOrderPage{Items: make([]AdminOrderView, 0), Limit: in.Limit, Offset: in.Offset}
	if err := query().Count(&out.Total).Error; err != nil {
		return out, ErrStorage
	}
	err := query().Select("o.id, o.product_id, p.name AS product_name, p.slug AS product_slug, o.plan_version_id, pl.name AS plan_name, pv.version AS plan_version, o.action, o.status, o.currency, o.amount_minor, o.purchased_periods AS purchase_periods, o.created_at, o.paid_at, o.environment, u.email AS customer_email, o.fulfillment_review").
		Order("o.created_at DESC, o.id DESC").Limit(in.Limit).Offset(in.Offset).Scan(&out.Items).Error
	if err != nil {
		return out, ErrStorage
	}
	return out, nil
}

type ReconcileOrdersInput struct {
	OrderIDs []string `json:"order_ids"`
	Reason   string   `json:"reason"`
}

type ReconcileOrderResult struct {
	OrderID string `json:"order_id"`
	Checked bool   `json:"checked"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Small batches keep external requests bounded and let the UI show completed
// results between batches. A failed order does not hide successful checks.
func (s *Service) ReconcileOrders(ctx context.Context, p Principal, in ReconcileOrdersInput) ([]ReconcileOrderResult, error) {
	if !p.CanManage() {
		return nil, ErrForbidden
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.OrderIDs) < 1 || len(in.OrderIDs) > 5 || len(in.Reason) < 2 || len(in.Reason) > 500 {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool)
	for _, id := range in.OrderIDs {
		if len(id) != 36 || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	results := make([]ReconcileOrderResult, 0, len(in.OrderIDs))
	for _, id := range in.OrderIDs {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result := ReconcileOrderResult{OrderID: id, Checked: true, Message: "已核对，请以订单最新状态为准"}
		if err := s.AdminReconcile(ctx, p, id, in.Reason); err != nil {
			result.Checked = false
			result.Code, result.Message = "STORAGE_UNAVAILABLE", "服务暂不可用，请稍后重试"
			var public *Error
			if errors.As(err, &public) {
				result.Code, result.Message = public.Code, public.Message
			}
		}
		results = append(results, result)
	}
	return results, nil
}
