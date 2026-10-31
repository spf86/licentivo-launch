package app

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

type CustomerOrderPage struct {
	Items  []OrderView `json:"items"`
	Total  int64       `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

type UserQuery struct {
	Query, Status string
	Limit, Offset int
}

type UserPage struct {
	Items  []ManagedUser `json:"items"`
	Total  int64         `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

func listBounds(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return 0, ErrInvalid
	}
	return limit, nil
}

func literalSearch(value string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(value))
}

// Customer order queries always scope the complete history to the authenticated
// user, including the count. A UUID or search term cannot widen that scope.
func (s *Service) SearchOrders(ctx context.Context, p Principal, in AdminOrderQuery) (CustomerOrderPage, error) {
	limit, err := listBounds(in.Limit, in.Offset)
	if err != nil {
		return CustomerOrderPage{}, err
	}
	in.Query = strings.TrimSpace(in.Query)
	if len(in.Query) > 254 || in.NeedsReview {
		return CustomerOrderPage{}, ErrInvalid
	}
	switch in.Status {
	case "", "pending", "closed", "paid", "refunding", "refunded", "reversed":
	default:
		return CustomerOrderPage{}, ErrInvalid
	}
	if in.Environment != "" && in.Environment != "sandbox" && in.Environment != "live" {
		return CustomerOrderPage{}, ErrInvalid
	}
	query := func() *gorm.DB {
		q := s.db.WithContext(ctx).Table("orders AS o").
			Joins("JOIN products AS p ON p.id=o.product_id").
			Joins("JOIN plan_versions AS pv ON pv.id=o.plan_version_id").
			Joins("JOIN plans AS pl ON pl.id=pv.plan_id").Where("o.user_id=?", p.User.ID)
		if in.Query != "" {
			pattern := "%" + literalSearch(in.Query) + "%"
			q = q.Where("(LOWER(o.id) LIKE ? ESCAPE '!' OR LOWER(p.name) LIKE ? ESCAPE '!' OR LOWER(pl.name) LIKE ? ESCAPE '!')", pattern, pattern, pattern)
		}
		if in.Status != "" {
			q = q.Where("o.status=?", in.Status)
		}
		if in.Environment != "" {
			q = q.Where("o.environment=?", in.Environment)
		}
		return q
	}
	out := CustomerOrderPage{Items: make([]OrderView, 0), Limit: limit, Offset: in.Offset}
	if err = query().Count(&out.Total).Error; err != nil {
		return out, ErrStorage
	}
	if err = query().Select("o.id,o.product_id,p.name AS product_name,p.slug AS product_slug,o.plan_version_id,pl.name AS plan_name,pv.version AS plan_version,o.action,o.status,o.currency,o.amount_minor,o.purchased_periods AS purchase_periods,o.created_at,o.paid_at,o.environment").Order("o.created_at DESC,o.id DESC").Limit(limit).Offset(in.Offset).Scan(&out.Items).Error; err != nil {
		return out, ErrStorage
	}
	return out, nil
}

func (s *Service) SearchUsers(ctx context.Context, p Principal, in UserQuery) (UserPage, error) {
	if p.Role != "admin" && p.Role != "operations" && p.Role != "support" {
		return UserPage{}, ErrForbidden
	}
	limit, err := listBounds(in.Limit, in.Offset)
	if err != nil {
		return UserPage{}, err
	}
	in.Query = strings.TrimSpace(in.Query)
	if len(in.Query) > 254 {
		return UserPage{}, ErrInvalid
	}
	switch in.Status {
	case "", "active", "disabled", "closed":
	default:
		return UserPage{}, ErrInvalid
	}
	query := func() *gorm.DB {
		q := s.db.WithContext(ctx).Table("users AS u").Where("NOT EXISTS (SELECT 1 FROM staff_accounts s WHERE s.user_id=u.id)")
		if in.Query != "" {
			q = q.Where("u.email_key LIKE ? ESCAPE '!'", "%"+literalSearch(in.Query)+"%")
		}
		if in.Status != "" {
			q = q.Where("u.status=?", in.Status)
		}
		return q
	}
	out := UserPage{Items: make([]ManagedUser, 0), Limit: limit, Offset: in.Offset}
	if err = query().Count(&out.Total).Error; err != nil {
		return out, ErrStorage
	}
	// Read only public management fields; password hashes never enter the view.
	if err = query().Select("u.id,u.email,(u.email_verified_at IS NOT NULL) AS verified,'customer' AS role,FALSE AS full_access,u.password_change_required,u.status,u.created_at").Order("u.created_at DESC,u.id DESC").Limit(limit).Offset(in.Offset).Scan(&out.Items).Error; err != nil {
		return out, ErrStorage
	}
	return out, nil
}
