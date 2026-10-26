package httpapi

import (
	publicdocs "licentivo-launch/docs"
	"licentivo-launch/internal/app"
	"net/http"
	"strings"
	"time"
)

func (a *application) registerExternalLicensing(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/licensing/documentation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.ServeContent(w, r, "external-licensing.md", time.Time{}, strings.NewReader(publicdocs.ExternalLicensingGuide))
	})
	mux.HandleFunc("GET /api/v1/licensing/protocol", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeContent(w, r, "external-licensing-openapi.json", time.Time{}, strings.NewReader(publicdocs.ExternalLicensingProtocol))
	})
	a.route(mux, "GET /api/v1/admin/licensing/connections", true, 60, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.LicensingConnections(r.Context(), p)
	})
	a.route(mux, "PUT /api/v1/admin/licensing/provider", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.LicensingProviderInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SaveLicensingProvider(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/licensing/connections", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ExternalConnectionInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveExternalConnection(r.Context(), p, "", in)
	})
	a.route(mux, "PUT /api/v1/admin/licensing/connections/{id}", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ExternalConnectionInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveExternalConnection(r.Context(), p, r.PathValue("id"), in)
	})
	a.route(mux, "POST /api/v1/admin/licensing/connections/{id}/check", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.CheckExternalConnection(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "PUT /api/v1/admin/products/{id}/licensing", true, 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ProductLicensingInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"saved": true}, a.service.SaveProductLicensing(r.Context(), p, r.PathValue("id"), in)
	})
	a.route(mux, "GET /api/v1/me/products/{id}/delivery", true, 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		return a.service.CustomerDelivery(r.Context(), p, r.PathValue("id"), r.URL.Query().Get("environment"))
	})
}
