package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

type InvoiceInput struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	TaxID string `json:"tax_id,omitempty"`
	Email string `json:"email"`
}

type InvoiceView struct {
	ID             string     `json:"id"`
	OrderID        string     `json:"order_id"`
	UserID         string     `json:"user_id,omitempty"`
	CustomerEmail  string     `json:"customer_email,omitempty"`
	InvoiceType    string     `json:"type"`
	Title          string     `json:"title"`
	TaxID          string     `json:"tax_id,omitempty"`
	RecipientEmail string     `json:"email"`
	Status         string     `json:"status"`
	InvoiceURL     string     `json:"invoice_url,omitempty"`
	AdminNote      string     `json:"admin_note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	IssuedAt       *time.Time `json:"issued_at,omitempty"`
}

func invoiceView(row storage.InvoiceRequest, admin bool) InvoiceView {
	v := InvoiceView{ID: row.ID, OrderID: row.OrderID, InvoiceType: row.InvoiceType, Title: row.Title, RecipientEmail: row.RecipientEmail, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, IssuedAt: row.IssuedAt}
	if row.TaxNumber != nil {
		v.TaxID = *row.TaxNumber
	}
	if row.InvoiceURL != nil {
		v.InvoiceURL = *row.InvoiceURL
	}
	if row.AdminNote != nil {
		v.AdminNote = *row.AdminNote
	}
	if admin {
		v.UserID = row.UserID
		if row.User != nil {
			v.CustomerEmail = row.User.Email
		}
	}
	return v
}

func (s *Service) RequestInvoice(ctx context.Context, p Principal, orderID string, in InvoiceInput) (InvoiceView, error) {
	in.Type, in.Title, in.TaxID, in.Email = strings.TrimSpace(in.Type), strings.TrimSpace(in.Title), strings.TrimSpace(in.TaxID), strings.TrimSpace(in.Email)
	if in.Type != "personal" && in.Type != "company" || len(in.Title) < 2 || len(in.Title) > 200 || len(in.TaxID) > 64 {
		return InvoiceView{}, ErrInvalid
	}
	if in.Type == "company" && len(in.TaxID) < 8 {
		return InvoiceView{}, problem(400, "TAX_ID_REQUIRED", "企业发票请填写纳税人识别号")
	}
	recipient, err := email(in.Email)
	if err != nil {
		return InvoiceView{}, problem(400, "INVOICE_EMAIL_INVALID", "请填写接收发票的有效邮箱")
	}
	var row storage.InvoiceRequest
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order storage.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ? AND user_id = ?", orderID, p.User.ID).Error; err != nil {
			return ErrNotFound
		}
		if order.Status != "paid" || order.PaidAt == nil || order.Environment != "live" {
			return problem(409, "INVOICE_NOT_AVAILABLE", "只有已付款的正式订单可以申请发票")
		}
		if err := tx.First(&row, "order_id = ?", order.ID).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		at := now()
		var taxID *string
		if in.TaxID != "" {
			taxID = &in.TaxID
		}
		row = storage.InvoiceRequest{ID: id, OrderID: order.ID, UserID: p.User.ID, InvoiceType: in.Type, Title: in.Title, TaxNumber: taxID, RecipientEmail: recipient, Status: "requested", CreatedAt: at, UpdatedAt: at}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "invoice.requested", "invoice", row.ID, "")
	})
	if err != nil {
		return InvoiceView{}, databaseError(err)
	}
	return invoiceView(row, false), nil
}

func (s *Service) Invoices(ctx context.Context, p Principal, all bool) ([]InvoiceView, error) {
	if all && !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.InvoiceRequest
	query := s.db.WithContext(ctx).Preload("User").Order("created_at DESC").Limit(200)
	if !all {
		query = query.Where("user_id = ?", p.User.ID)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, ErrStorage
	}
	out := make([]InvoiceView, 0, len(rows))
	for _, row := range rows {
		out = append(out, invoiceView(row, all))
	}
	return out, nil
}

type InvoiceDecision struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Note   string `json:"note,omitempty"`
}

func (s *Service) DecideInvoice(ctx context.Context, p Principal, id string, in InvoiceDecision) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	in.Status, in.URL, in.Note = strings.TrimSpace(in.Status), strings.TrimSpace(in.URL), strings.TrimSpace(in.Note)
	if in.Status != "issued" && in.Status != "rejected" || len(in.Note) > 500 {
		return ErrInvalid
	}
	if in.Status == "issued" {
		u, err := url.Parse(in.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return problem(400, "INVOICE_URL_INVALID", "请填写客户可安全下载的 HTTPS 发票地址")
		}
	} else if len(in.Note) < 2 {
		return problem(400, "INVOICE_NOTE_REQUIRED", "拒绝开票时请填写原因")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row storage.InvoiceRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("User").First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status != "requested" {
			return problem(409, "INVOICE_ALREADY_HANDLED", "该发票申请已经处理")
		}
		at := now()
		changes := map[string]any{"status": in.Status, "admin_note": in.Note, "updated_at": at}
		if in.Status == "issued" {
			changes["invoice_url"], changes["issued_at"] = in.URL, at
		}
		if err := tx.Model(&row).Updates(changes).Error; err != nil {
			return err
		}
		subject, body := "发票申请处理结果", "你的发票申请未能开具："+in.Note
		if in.Status == "issued" {
			subject, body = "你的发票已开具", "发票已经开具，请登录客户中心下载：\n"+in.URL
		}
		if err := s.queueMail(tx, MailMessage{To: row.RecipientEmail, Subject: subject, Body: body, IdempotencyKey: "invoice/" + row.ID + "/" + in.Status}); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "invoice."+in.Status, "invoice", row.ID, in.Note)
	}))
}
