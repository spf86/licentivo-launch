package httpapi

import (
	"net/http"

	"licentivo-launch/internal/app"
)

func (a *application) registerStorePolicies(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/admin/policies", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		return a.service.StorePolicies(r.Context(), p)
	})
	for _, action := range []struct {
		pattern string
		publish bool
	}{{"PUT /api/v1/admin/policies/{key}", false}, {"POST /api/v1/admin/policies/{key}/publish", true}} {
		a.route(mux, action.pattern, true, 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
			var in app.PolicyInput
			if err := inputLimit(w, r, &in, 128<<10); err != nil {
				return nil, err
			}
			return a.service.SaveStorePolicy(r.Context(), p, r.PathValue("key"), in, action.publish)
		})
	}
	a.route(mux, "GET /api/v1/policies/{key}", false, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		return a.service.PublishedPolicy(r.Context(), r.PathValue("key"))
	})
}
