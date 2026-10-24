package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type PaymentWebhookResponse struct {
	Body   []byte
	Header http.Header
}

func (s *Service) ReceivePaymentEvent(ctx context.Context, accountID string, headers http.Header, body []byte) error {
	_, err := s.ReceivePaymentWebhook(ctx, accountID, headers, body)
	return err
}
func (s *Service) ReceivePaymentWebhook(ctx context.Context, accountID string, headers http.Header, body []byte) (PaymentWebhookResponse, error) {
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&account, "id = ?", accountID).Error != nil {
		return PaymentWebhookResponse{}, ErrNotFound
	}
	client, err := s.paymentProvider(ctx, account)
	if err != nil {
		return PaymentWebhookResponse{}, ErrStorage
	}
	event, err := client.VerifyWebhook(ctx, headers, body)
	if err != nil {
		return PaymentWebhookResponse{}, problem(400, "WEBHOOK_INVALID", "通知验证未通过")
	}
	if string(event.Environment) != account.Environment || event.ID == "" {
		return PaymentWebhookResponse{}, ErrInvalid
	}
	sum := sha256.Sum256(body)
	err = databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous storage.PaymentEvent
		err := tx.First(&previous, "account_id = ? AND external_event_id = ?", accountID, event.ID).Error
		if err == nil {
			if !bytes.Equal(previous.BodyHash, sum[:]) {
				return problem(409, "EVENT_CONFLICT", "通知内容与原事件不一致")
			}
			// A channel resend can recover a previously unsupported reversal or
			// an exhausted read-only verification. Reuse the stored event identity;
			// successful canonical reversals remain protected by their receipt.
			if previous.Status == "review" && paymentReversalKind(event.Kind) {
				result := tx.Model(&storage.PaymentEvent{}).Where("id = ? AND status = 'review'", previous.ID).Updates(map[string]any{"status": "pending", "attempts": 0, "available_at": now(), "lease_token": nil, "lease_until": nil, "verified_payload": marshal(event)})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected > 0 {
					return audit(tx, "", "payment_event.redelivered", "payment_event", previous.ID, "原渠道重新验证并重发撤款通知")
				}
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		sealed, err := s.vault.Seal(body, "payment-event:"+id)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&storage.PaymentEvent{ID: id, AccountID: accountID, ExternalEventID: event.ID, Kind: event.Kind, BodyHash: sum[:], BodyCiphertext: sealed, VerifiedPayload: marshal(event), Status: "pending", VerifiedAt: now()}).Error; err != nil {
			return err
		}
		var stored storage.PaymentEvent
		if err := tx.First(&stored, "account_id = ? AND external_event_id = ?", accountID, event.ID).Error; err != nil {
			return err
		}
		if !bytes.Equal(stored.BodyHash, sum[:]) {
			return problem(409, "EVENT_CONFLICT", "通知内容与原事件不一致")
		}
		return nil
	}))
	if err != nil {
		return PaymentWebhookResponse{}, err
	}
	if acknowledger, ok := client.(interface {
		WebhookAcknowledgement() ([]byte, http.Header, error)
	}); ok {
		body, header, err := acknowledger.WebhookAcknowledgement()
		return PaymentWebhookResponse{body, header}, err
	}
	return PaymentWebhookResponse{}, nil
}
func (s *Service) ProcessPaymentEvent(ctx context.Context) (handled bool, processErr error) {
	var row storage.PaymentEvent
	lease, err := newID()
	if err != nil {
		return false, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status = 'pending' AND available_at <= ?) OR (status = 'running' AND lease_until < ?)", now(), now()).Order("received_at,id").First(&row).Error; err != nil {
			return err
		}
		row.Attempts++
		return tx.Model(&row).Updates(map[string]any{"status": "running", "lease_token": lease, "lease_until": now().Add(time.Minute), "attempts": row.Attempts}).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, ErrStorage
	}
	defer func() {
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		changes := map[string]any{"lease_token": nil, "lease_until": nil}
		if processErr != nil {
			state := "pending"
			if row.Attempts >= 12 {
				state = "review"
			}
			changes["status"] = state
			changes["available_at"] = now().Add(time.Duration(1<<min(row.Attempts, 10)) * time.Second)
		}
		_ = s.db.WithContext(finish).Model(&row).Where("lease_token = ?", lease).Updates(changes).Error
	}()
	var event ports.VerifiedEvent
	if json.Unmarshal(row.VerifiedPayload, &event) != nil {
		return true, databaseError(s.db.WithContext(ctx).Model(&row).Update("status", "review").Error)
	}
	switch event.Kind {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "CHECKOUT.ORDER.APPROVED", "PAYMENT.CAPTURE.COMPLETED", "PAYMENT_NOTIFICATION", "transaction.completed", "transaction.paid", "transaction.updated", "transaction.canceled":
	case "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed", "charge.dispute.funds_withdrawn", "charge.dispute.funds_reinstated", "PAYMENT.CAPTURE.REVERSED", "adjustment.created", "adjustment.updated":
		return true, s.processPaymentReversal(ctx, row, event)
	case "refund.created", "refund.updated", "PAYMENT.CAPTURE.REFUNDED", "REFUND_NOTIFICATION":
		return true, s.processRefundEvent(ctx, row, event)
	default:
		return true, databaseError(s.db.WithContext(ctx).Model(&row).Update("status", "review").Error)
	}
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&account, "id = ?", row.AccountID).Error != nil {
		return true, ErrStorage
	}
	client, err := s.paymentProvider(ctx, account)
	if err != nil {
		return true, ErrStorage
	}
	observed, err := client.QueryPayment(ctx, event.ResourceID)
	if err != nil {
		return true, problem(503, "PAYMENT_EVENT_PENDING", "支付通知已保存，正在核实")
	}
	var payment storage.Payment
	if s.db.WithContext(ctx).First(&payment, "order_id = ? AND account_id = ?", observed.OrderID, account.ID).Error != nil {
		return true, databaseError(s.db.WithContext(ctx).Model(&row).Update("status", "review").Error)
	}
	if payment.ExternalCheckoutID == nil {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current storage.Payment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", payment.ID).Error; err != nil {
				return err
			}
			if current.ExternalCheckoutID != nil && *current.ExternalCheckoutID != event.ResourceID {
				return ErrInvalid
			}
			return tx.Model(&current).Update("external_checkout_id", event.ResourceID).Error
		})
		if err != nil {
			return true, databaseError(err)
		}
	} else if *payment.ExternalCheckoutID != event.ResourceID {
		return true, databaseError(s.db.WithContext(ctx).Model(&row).Update("status", "review").Error)
	}
	if err := s.reconcilePayment(ctx, payment.OrderID); err != nil {
		return true, err
	}
	return true, databaseError(s.db.WithContext(ctx).Model(&row).Update("status", "processed").Error)
}

type ProductEntitlement struct {
	ProductID    string                `json:"product_id"`
	ProductName  string                `json:"product_name"`
	ProductSlug  string                `json:"product_slug"`
	DownloadURL  string                `json:"download_url,omitempty"`
	Version      string                `json:"version,omitempty"`
	Downloads    []ProductDownloadView `json:"downloads"`
	Status       string                `json:"status"`
	ValidFrom    *time.Time            `json:"valid_from"`
	ValidUntil   *time.Time            `json:"valid_until"`
	Snapshot     json.RawMessage       `json:"snapshot"`
	Environment  string                `json:"environment"`
	Synchronized bool                  `json:"synchronized"`
}

func (s *Service) MyProducts(ctx context.Context, p Principal, requestedLocale string) ([]ProductEntitlement, error) {
	var rows []storage.Entitlement
	if s.db.WithContext(ctx).Where("user_id = ?", p.User.ID).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	source, locale := requestedContentLocale(s.db.WithContext(ctx), requestedLocale)
	views := make([]ProductEntitlement, 0, len(rows))
	for _, row := range rows {
		var product storage.Product
		if s.db.WithContext(ctx).First(&product, "id = ?", row.ProductID).Error != nil {
			return nil, ErrStorage
		}
		var content ProductContent
		if json.Unmarshal(product.Content, &content) != nil {
			return nil, ErrStorage
		}
		productView, _ := s.localizedProductView(ctx, ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, Content: content}, locale, source)
		if err := s.attachProductDownloads(ctx, &productView); err != nil {
			return nil, err
		}
		status := row.Status
		if row.ManualHold {
			status = "suspended"
		}
		if row.ValidUntil != nil && !row.ValidUntil.After(now()) {
			status = "expired"
		}
		views = append(views, ProductEntitlement{
			ProductID:    row.ProductID,
			ProductName:  productView.Name,
			ProductSlug:  productView.Slug,
			DownloadURL:  productView.Content.DownloadURL,
			Version:      productView.Content.Version,
			Downloads:    productView.Downloads,
			Status:       status,
			ValidFrom:    row.ValidFrom,
			ValidUntil:   row.ValidUntil,
			Snapshot:     row.Snapshot,
			Environment:  row.Environment,
			Synchronized: row.LaunchPurchaseRef != nil && row.AppliedVersion > 0 && row.AppliedVersion == row.DesiredVersion,
		})
	}
	return views, nil
}
func (s *Service) SoftwareCredential(ctx context.Context, p Principal, productID, environment string) (map[string]any, error) {
	return nil, problem(410, "LICENSE_CREDENTIAL_REMOVED", "请升级接入程序，使用短期账号身份凭据连接 Licentivo")
}
func (s *Service) RetryAuthorization(ctx context.Context, p Principal, id, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(reason) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task storage.SyncTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "operation_id = ?", id).Error; err != nil {
			return err
		}
		if task.Status == "succeeded" || task.Status == "running" {
			return problem(409, "TASK_STATE_CONFLICT", "当前任务无需重试或正在执行")
		}
		var command ports.CommerceSnapshot
		if task.LaunchConnectionID == nil || json.Unmarshal(task.RequestJSON, &command) != nil || command.Validate() != nil {
			return problem(409, "TASK_NEEDS_COMPENSATION", "原任务参数不完整，需要对账后补偿")
		}
		if err := tx.Model(&task).Updates(map[string]any{"status": "pending", "attempts": 0, "available_at": now(), "last_error_code": nil, "retry_requested": task.RemoteAcceptedAt != nil}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "authorization.retry", "sync_task", id, reason)
	}))
}
