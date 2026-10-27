package httpapi

import (
	"licentivo-launch/internal/app"
	"net/http"
)

func (a *application) registerIntegration(mux *http.ServeMux) {
	a.registerExternalLicensing(mux)
	a.route(mux, "GET /api/v1/auth/identity-keys", false, 60, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		return a.service.IdentityKeys(), nil
	})
	a.route(mux, "POST /api/v1/software/products/{id}/identity", true, 60, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			Environment string `json:"environment"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		if err := a.service.RequireSDKTransport(r.Context(), in.Environment); err != nil {
			return nil, err
		}
		return a.service.Identity(r.Context(), p, r.PathValue("id"), in.Environment)
	})
	a.route(mux, "GET /api/v1/admin/integration/context", true, 60, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		if !p.IsAdmin() {
			return nil, app.ErrForbidden
		}
		allowed, err := a.service.SDKHTTPEnabled(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"merchant_origin": a.service.PublicURL(), "licensing_origin": app.LicentivoServiceURL, "allow_http": allowed}, nil
	})
	a.route(mux, "GET /api/v1/software/products/{id}/status", true, 60, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		environment := r.URL.Query().Get("environment")
		if environment == "" {
			environment = "live"
		}
		if err := a.service.RequireSDKTransport(r.Context(), environment); err != nil {
			return nil, err
		}
		return a.service.SoftwareEntitlementStatus(r.Context(), p, r.PathValue("id"), environment)
	})
	a.route(mux, "POST /api/v1/admin/integration/check", true, 12, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		var in app.IntegrationInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CheckIntegration(r.Context(), p, in)
	})
}
