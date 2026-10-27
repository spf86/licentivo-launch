package httpapi_test

import (
	"context"
	"errors"
	"strings"
	"sync"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/storage"
)

func testStorePolicies(a *testApp, admin, customer *browser) {
	t := a.t
	a.expect(a.request("GET", "/api/v1/admin/policies", nil, nil), 401)
	a.expect(a.request("GET", "/api/v1/admin/policies", nil, customer), 403)
	in := app.PolicyInput{PolicyContent: app.PolicyContent{Title: "Custom terms", Body: "PRIVATE_DRAFT_ONE"}}
	a.expect(a.request("PUT", "/api/v1/admin/policies/terms", in, customer), 403)
	a.expect(a.request("POST", "/api/v1/admin/policies/terms/publish", in, customer), 403)
	a.expect(a.request("PUT", "/api/v1/admin/policies/unknown", in, admin), 400)
	a.expect(a.request("GET", "/api/v1/policies/unknown", nil, nil), 404)
	views := data[[]app.StorePolicyView](t, a.request("GET", "/api/v1/admin/policies", nil, admin))
	if len(views) != 3 {
		t.Fatal("three independent policy drafts were not initialized")
	}
	draft := data[app.StorePolicyView](t, a.request("PUT", "/api/v1/admin/policies/terms", in, admin))
	if draft.Version != 1 || !draft.HasDraftChanges || draft.Published != nil {
		t.Fatal("saving a draft published it", draft)
	}
	a.expect(a.request("GET", "/api/v1/policies/terms", nil, nil), 404)
	if strings.Contains(a.request("GET", "/api/v1/site", nil, nil).Body.String(), in.Body) {
		t.Fatal("a private draft leaked through the site endpoint")
	}
	a.expect(a.request("POST", "/api/v1/admin/policies/terms/publish", in, admin), 409)
	in.Version = draft.Version
	pub := data[app.StorePolicyView](t, a.request("POST", "/api/v1/admin/policies/terms/publish", in, admin))
	if pub.HasDraftChanges || pub.Published == nil || pub.PublishedAt == nil {
		t.Fatal("publication did not freeze the current draft", pub)
	}
	public := data[app.PublishedPolicy](t, a.request("GET", "/api/v1/policies/terms", nil, nil))
	if public.Body != in.Body || public.Title != in.Title || public.Version != 1 {
		t.Fatal("published policy differs from the reviewed draft", public)
	}
	in.Body = "PRIVATE_DRAFT_TWO"
	draft = data[app.StorePolicyView](t, a.request("PUT", "/api/v1/admin/policies/terms", in, admin))
	if draft.Version != 2 || !draft.HasDraftChanges || draft.Published.Body == in.Body {
		t.Fatal("new draft overwrote the published policy", draft)
	}
	for _, path := range []string{"/api/v1/site", "/api/v1/policies/terms"} {
		if strings.Contains(a.request("GET", path, nil, nil).Body.String(), in.Body) {
			t.Fatal("unpublished changes leaked", path)
		}
	}
	invalid := app.PolicyInput{PolicyContent: app.PolicyContent{Title: "Terms", Body: ""}, Version: draft.Version}
	a.expect(a.request("POST", "/api/v1/admin/policies/terms/publish", invalid, admin), 400)
	invalid.Body = strings.Repeat("字", 16001)
	a.expect(a.request("PUT", "/api/v1/admin/policies/terms", invalid, admin), 400)

	ctx := context.Background()
	service, err := app.New(ctx, a.db, app.Config{PublicURL: "https://merchant.example", MasterKey: a.key, UploadDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var owner storage.User
	if err := a.db.First(&owner, "email = ?", "owner@example.invalid").Error; err != nil {
		t.Fatal(err)
	}
	var saves sync.WaitGroup
	results := make(chan error, 2)
	for _, body := range []string{"concurrent administrator one", "concurrent administrator two"} {
		saves.Add(1)
		go func(body string) {
			defer saves.Done()
			_, err := service.SaveStorePolicy(ctx, app.Principal{User: owner, Role: "admin"}, "terms", app.PolicyInput{PolicyContent: app.PolicyContent{Title: "Terms", Body: body}, Version: draft.Version}, false)
			results <- err
		}(body)
	}
	saves.Wait()
	close(results)
	saved, conflicted := 0, 0
	for err := range results {
		var business *app.Error
		if err == nil {
			saved++
		} else if errors.As(err, &business) && business.Code == "POLICY_VERSION_CONFLICT" {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if saved != 1 || conflicted != 1 {
		t.Fatal("concurrent edits silently overwrote one another", saved, conflicted)
	}
	a.handler = httpapi.Handler(httpapi.Options{App: service, PublicURL: "https://merchant.example", SecureCookies: true})
	views = data[[]app.StorePolicyView](t, a.request("GET", "/api/v1/admin/policies", nil, admin))
	if views[0].Version != 3 || views[0].PublishedVersion != 1 || views[1].Version != 0 || views[2].Version != 0 {
		t.Fatal("restart lost draft/published separation or changed another document", views)
	}
	privacy := app.PolicyInput{PolicyContent: app.PolicyContent{Title: "Privacy", Body: "Published privacy"}}
	a.expect(a.request("POST", "/api/v1/admin/policies/privacy/publish", privacy, admin), 200)
	site := data[app.SiteView](t, a.request("GET", "/api/v1/site", nil, nil))
	if len(site.Policies) != 2 || site.Policies[0].Key != "terms" || site.Policies[1].Key != "privacy" || site.Policies[0].Body != public.Body {
		t.Fatal("public policy catalog did not retain the independent published versions", site.Policies)
	}
}
