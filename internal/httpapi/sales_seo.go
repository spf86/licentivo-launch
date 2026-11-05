package httpapi

import (
	"licentivo-launch/internal/app"
	"net/http"
)

func (a *application) registerSalesSEO(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/admin/sales/transactions", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		limit, offset, err := listPagination(r)
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return a.service.SalesTransactions(r.Context(), p, app.SalesQuery{From: q.Get("from"), To: q.Get("to"), TimeZone: q.Get("time_zone"), Environment: q.Get("environment"), Currency: q.Get("currency"), ProductID: q.Get("product_id"), PlanID: q.Get("plan_id")}, q.Get("kind"), limit, offset)
	})
	a.route(mux, "GET /api/v1/admin/sales", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		q := r.URL.Query()
		return a.service.SalesAnalytics(r.Context(), p, app.SalesQuery{From: q.Get("from"), To: q.Get("to"), TimeZone: q.Get("time_zone"), Environment: q.Get("environment"), Currency: q.Get("currency"), ProductID: q.Get("product_id"), PlanID: q.Get("plan_id")})
	})
	a.route(mux, "GET /api/v1/admin/seo", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.SEOSettings(r.Context(), p, r.URL.Query().Get("product_id"), r.URL.Query().Get("locale"))
	})
	a.route(mux, "PUT /api/v1/admin/seo", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.SEOInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveSEOSettings(r.Context(), p, in)
	})
	a.registerPublicStorefront(mux)
}
