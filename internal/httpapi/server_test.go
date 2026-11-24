package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/storage"
)

func TestReadinessIsNotLivenessAndDoesNotExposeDatabaseErrors(t *testing.T) {
	for _, databaseErr := range []error{storage.ErrNotConfigured, storage.ErrSchema, errors.New("dsn:user:secret@internal-host")} {
		h := Handler(Options{Ready: func(context.Context) error { return databaseErr }})
		for _, path := range []string{"/readyz", "/healthz"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			want := 503
			if path == "/healthz" {
				want = 200
			}
			if w.Code != want || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("unsafe or incorrect response: %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request correlation")
			}
		}
	}
}

func TestRenderedCheckoutLinksRespectStorefrontEnvironment(t *testing.T) {
	view := publicPageView{
		Language: "en", Kind: "product", Home: "/en/", ProductURL: "/en/products/docs-tool",
		Site:  app.SiteView{Product: &app.ProductView{Slug: "docs-tool"}},
		Plans: []publicPlan{{CanBuy: true, BuyURL: "/#/checkout/docs-tool/plan-v1"}},
	}
	for _, env := range []string{"live", "sandbox"} {
		view.Site.Environment = env
		var page strings.Builder
		if err := publicTemplate.ExecuteTemplate(&page, "public.html", view); err != nil {
			t.Fatal(err)
		}
		path := "/#/checkout/docs-tool/plan-v1"
		if env == "sandbox" {
			path = "/?preview=sandbox#/checkout/docs-tool/plan-v1"
		}
		if !strings.Contains(page.String(), `href="`+path+`"`) {
			t.Fatalf("missing %s purchase link %s", env, path)
		}
	}
}

func TestUnimplementedSalesDoesNotAcceptPayments(t *testing.T) {
	h := Handler(Options{})
	for _, path := range []string{"/api/v1/orders", "/api/v1/webhooks/stripe", "/api/v1/webhooks/paypal", "/api/v1/webhooks/licentivo"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if w.Code >= 200 && w.Code < 300 {
			t.Fatalf("unimplemented mutation acknowledged at %s", path)
		}
	}
}
