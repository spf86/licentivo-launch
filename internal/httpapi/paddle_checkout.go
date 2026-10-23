package httpapi

import (
	"html/template"
	"net/http"
)

func (a *application) registerPaddleCheckout(mux *http.ServeMux) {
	page := template.Must(template.ParseFS(assets, "web/paddle-checkout.html"))
	mux.HandleFunc("GET /pay/paddle/{account}", func(w http.ResponseWriter, r *http.Request) {
		if a.service == nil {
			http.Error(w, "Service unavailable", 503)
			return
		}
		view, err := a.service.PaddleCheckout(r.Context(), r.PathValue("account"), r.URL.Query().Get("_ptxn"))
		if err != nil {
			apiFail(w, r, err)
			return
		}
		// Paddle's overlay is isolated to this payment page; the rest of the
		// application keeps its strict, self-only content security policy.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://cdn.paddle.com; style-src 'self' 'unsafe-inline'; connect-src 'self' https://*.paddle.com; frame-src https://buy.paddle.com https://sandbox-buy.paddle.com; img-src 'self' data: https://*.paddle.com; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, view)
	})
}
