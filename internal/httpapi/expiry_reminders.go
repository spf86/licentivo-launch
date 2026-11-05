package httpapi

import (
	"net/http"
	"strconv"

	"licentivo-launch/internal/app"
)

func (a *application) registerExpiryReminders(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/admin/mail/quotas", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.MailQuotas(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/mail/quotas", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.MailQuotaInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveMailQuotas(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/admin/expiry-reminders/settings", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.ExpiryReminderSettings(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/expiry-reminders/settings", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ExpiryReminderSettingsInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveExpiryReminderSettings(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/admin/expiry-reminders/preview", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.PreviewExpiryReminder(r.Context(), p, r.URL.Query().Get("locale"), r.URL.Query().Get("entitlement_id"))
	})
	a.route(mux, "POST /api/v1/admin/expiry-reminders/test", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ExpiryReminderTestInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.TestExpiryReminder(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/admin/expiry-reminders/jobs", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		limit, offset := 25, 0
		var err error
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil {
				return nil, app.ErrInvalid
			}
		}
		if raw := r.URL.Query().Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
			if err != nil {
				return nil, app.ErrInvalid
			}
		}
		return a.service.ExpiryReminderJobs(r.Context(), p, limit, offset)
	})
	a.route(mux, "GET /api/v1/admin/entitlements/{id}/expiry-reminder", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.ExpiryReminderPreference(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "POST /api/v1/admin/entitlements/{id}/expiry-reminder", true, 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ExpiryReminderPreferenceInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveExpiryReminderPreference(r.Context(), p, r.PathValue("id"), in)
	})
}
