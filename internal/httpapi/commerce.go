package httpapi

import (
	"io"
	"net/http"

	"licentivo-launch/internal/app"
)

func (a *application) registerCommerce(mux *http.ServeMux) {
	a.route(mux, "POST /api/v1/webhooks/licentivo/{connection}", false, 1200, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, app.ErrInvalid
		}
		return map[string]bool{"received": true}, a.service.ReceiveLicenseEvent(r.Context(), r.PathValue("connection"), r.Header, body)
	})
	a.route(mux, "POST /api/v1/admin/entitlements/{id}/reconcile", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		if !p.CanManage() {
			return nil, app.ErrForbidden
		}
		return map[string]bool{"checked": true}, a.service.ReconcileEntitlement(r.Context(), r.PathValue("id"))
	})
	a.route(mux, "GET /api/v1/admin/recovery", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		if !p.IsAdmin() {
			return nil, app.ErrForbidden
		}
		return map[string]bool{"review_required": a.service.RecoveryRequired(r.Context())}, nil
	})
	a.route(mux, "POST /api/v1/admin/recovery/complete", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Password string `json:"password"`
			Reason   string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"resumed": true}, a.service.FinishRecovery(r.Context(), p, in.Password, in.Reason)
	})
	a.route(mux, "POST /api/v1/admin/exports", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.Export(r.Context(), p, in.Kind, in.Reason)
	})
	a.route(mux, "POST /api/v1/admin/staff", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SetStaffRole(r.Context(), p, in.UserID, in.Role, in.Reason)
	})
	a.route(mux, "GET /api/v1/admin/staff", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Staff(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/staff/create", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.CreateStaffInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"created": true}, a.service.CreateStaff(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/staff/{id}/status", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SetStaffStatus(r.Context(), p, r.PathValue("id"), in.Status, in.Reason)
	})
	a.route(mux, "GET /api/v1/me/products/{id}/devices", true, 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Devices(r.Context(), p, r.PathValue("id"), r.URL.Query().Get("environment"))
	})
	a.route(mux, "POST /api/v1/me/products/{id}/devices/release", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			app.DeviceInput
			Environment string `json:"environment"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.ReleaseDevice(r.Context(), p, r.PathValue("id"), in.Environment, in.DeviceInput)
	})
	a.route(mux, "GET /api/v1/me/products/{id}/plans", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.OwnedPlans(r.Context(), p, r.PathValue("id"), r.URL.Query().Get("environment"), requestLocale(r))
	})
	a.route(mux, "GET /api/v1/admin/orders", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AdminOrders(r.Context(), p)
	})
	a.route(mux, "GET /api/v1/admin/operations", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Operations(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/orders/{id}/reconcile", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"checked": true}, a.service.AdminReconcile(r.Context(), p, r.PathValue("id"), in.Reason)
	})
	a.route(mux, "POST /api/v1/admin/entitlements/{id}/hold", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Reason string `json:"reason"`
			Hold   bool   `json:"hold"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"queued": true}, a.service.SetEntitlementHold(r.Context(), p, r.PathValue("id"), in.Reason, in.Hold)
	})
	a.route(mux, "POST /api/v1/quotes", true, 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.QuoteInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.Quote(r.Context(), p, in, requestLocale(r))
	})
	a.route(mux, "POST /api/v1/orders/{id}/refund", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.RequestRefund(r.Context(), p, r.PathValue("id"), in.Reason)
	})
	a.route(mux, "GET /api/v1/me/refunds", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Refunds(r.Context(), p, false)
	})
	a.route(mux, "GET /api/v1/me/invoices", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Invoices(r.Context(), p, false)
	})
	a.route(mux, "POST /api/v1/orders/{id}/invoice", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.InvoiceInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.RequestInvoice(r.Context(), p, r.PathValue("id"), in)
	})
	a.route(mux, "GET /api/v1/admin/invoices", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Invoices(r.Context(), p, true)
	})
	a.route(mux, "POST /api/v1/admin/invoices/{id}", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.InvoiceDecision
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.DecideInvoice(r.Context(), p, r.PathValue("id"), in)
	})
	a.route(mux, "GET /api/v1/admin/refunds", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Refunds(r.Context(), p, true)
	})
	a.route(mux, "POST /api/v1/admin/refunds/{id}/approve", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"queued": true}, a.service.ApproveRefund(r.Context(), p, r.PathValue("id"), in.Reason)
	})
	a.route(mux, "POST /api/v1/orders", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.CheckoutInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CreateOrder(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/orders/{id}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Order(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "POST /api/v1/orders/{id}/reconcile", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.ReconcileOrder(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "GET /api/v1/me/products", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.MyProducts(r.Context(), p, requestLocale(r))
	})
	a.route(mux, "POST /api/v1/software/products/{id}/credential", true, 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Environment string `json:"environment"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SoftwareCredential(r.Context(), p, r.PathValue("id"), in.Environment)
	})
	a.route(mux, "GET /api/v1/admin/connections", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Connections(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/connections/payments", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.PaymentSettings
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SavePaymentSettings(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/connections/licentivo", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.LicenseSettings
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveLicenseSettings(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/connections/mappings", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.MappingInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SaveMapping(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/authorizations/{id}/retry", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"queued": true}, a.service.RetryAuthorization(r.Context(), p, r.PathValue("id"), in.Reason)
	})
	a.route(mux, "POST /api/v1/webhooks/payments/{account}", false, 1200, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, app.ErrInvalid
		}
		return map[string]bool{"received": true}, a.service.ReceivePaymentEvent(r.Context(), r.PathValue("account"), r.Header, body)
	})
}
