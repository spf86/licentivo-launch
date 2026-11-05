package httpapi

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"licentivo-launch/internal/app"
)

type publicLink struct{ Name, URL, Locale string }
type publicPlan struct {
	Name, Summary, Price, Term, BuyURL string
	Highlights                         []string
	CanBuy                             bool
}
type publicDocument struct{ Key, Title, Body, URL string }
type publicPageView struct {
	Site                                                                                app.SiteView
	SEO                                                                                 app.PublicSEOMetadata
	Kind, Language, Home, ProductURL, DownloadURL, DocsURL, DocumentTitle, DocumentBody string
	Links                                                                               []publicLink
	Plans                                                                               []publicPlan
	Documents                                                                           []publicDocument
	JSONLD                                                                              template.JS
}

var publicTemplate = template.Must(template.New("storefront").Funcs(template.FuncMap{"lowerLocale": app.PublicLocalePath}).ParseFS(assets, "web/public.html"))

func publicPath(locale, kind, slug, doc string) string {
	base := "/" + app.PublicLocalePath(locale) + "/"
	if kind == "home" {
		return base
	}
	base += "products/" + url.PathEscape(slug)
	if kind == "download" {
		base += "/download"
	}
	if kind == "docs" {
		base += "/docs"
		if doc != "" {
			base += "/" + url.PathEscape(doc)
		}
	}
	return base
}

func productDocuments(site app.SiteView, locale, slug string) []publicDocument {
	if site.Product == nil {
		return nil
	}
	c := site.Product.Content
	requirements := c.Requirements
	platforms := []string{}
	for platform := range c.PlatformRequirements {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		if text := strings.TrimSpace(c.PlatformRequirements[platform]); text != "" {
			requirements += "\n\n" + platform + "\n" + text
		}
	}
	entries := []struct{ key, label, body string }{
		{"requirements", "product.requirements", requirements},
		{"installation", "product.installation", c.InstallationGuide}, {"upgrade", "product.upgrade", c.UpgradeGuide}, {"uninstall", "product.uninstall", c.UninstallGuide}, {"troubleshooting", "product.troubleshooting", c.TroubleshootingGuide},
		{"terms", "product.terms", c.TermsOfService}, {"privacy", "product.privacy", c.PrivacyPolicy}, {"refunds", "product.refunds", c.RefundPolicy}, {"legal", "product.legal_tax", c.LegalTaxNotice},
	}
	out := []publicDocument{}
	for _, v := range entries {
		title := site.UI[v.label]
		for _, policy := range site.Policies {
			if policy.Key == v.key {
				v.body = policy.Body
				title = policy.Title
			}
		}
		if strings.TrimSpace(v.body) != "" {
			out = append(out, publicDocument{Key: v.key, Title: title, Body: v.body, URL: publicPath(locale, "docs", slug, v.key)})
		}
	}
	return out
}

func publicPrice(plan app.PlanView) (int64, string) {
	amount := plan.AmountMinor
	periods := uint64(1)
	for _, option := range plan.PurchaseOptions {
		if option.Enabled {
			amount = option.AmountMinor
			periods = option.Periods
			break
		}
	}
	exponent := 2
	switch plan.Currency {
	case "JPY", "KRW", "VND", "CLP", "ISK":
		exponent = 0
	case "BHD", "JOD", "KWD", "OMR", "TND":
		exponent = 3
	}
	divisor := int64(1)
	for range exponent {
		divisor *= 10
	}
	price := fmt.Sprintf("%s %d", plan.Currency, amount/divisor)
	if exponent > 0 {
		price += fmt.Sprintf(".%0*d", exponent, amount%divisor)
	}
	term := ""
	if plan.PurchaseType == "perpetual" {
		term = "permanent"
	} else if plan.TermUnit != nil && plan.TermCount != nil {
		term = fmt.Sprintf("%d %s", periods*uint64(*plan.TermCount), *plan.TermUnit)
	}
	return amount, price + " / " + term
}

func (a *application) registerPublicStorefront(mux *http.ServeMux) {
	for _, locale := range []string{"zh-cn", "en", "ja", "ko", "de", "fr", "es", "pt-br", "it", "ru"} {
		for _, suffix := range []string{"/{$}", "/products/{slug}", "/products/{slug}/download", "/products/{slug}/docs", "/products/{slug}/docs/{document}"} {
			mux.HandleFunc("GET /"+locale+suffix, func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("locale", locale)
				a.publicStorefront(w, r)
			})
		}
	}
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /api/\nDisallow: /admin\nDisallow: /account\nDisallow: /setup\nDisallow: /paddle-checkout\n")
		if a.service != nil && a.service.PublicURL() != "" {
			_, _ = fmt.Fprintf(w, "Sitemap: %s/sitemap.xml\n", a.service.PublicURL())
		}
	})
	mux.HandleFunc("GET /sitemap.xml", a.publicSitemap)
}

func (a *application) publicStorefront(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")
	if a.service == nil {
		http.Error(w, "Storefront is not configured", 503)
		return
	}
	locale := app.PublicLocale(r.PathValue("locale"))
	if locale == "" {
		http.NotFound(w, r)
		return
	}
	kind, slug, document := "home", r.PathValue("slug"), r.PathValue("document")
	if slug != "" {
		kind = "product"
		if strings.HasSuffix(r.URL.Path, "/download") {
			kind = "download"
		}
		if strings.Contains(r.URL.Path, "/docs") {
			kind = "docs"
		}
	}
	if slug != "" {
		resolved, err := a.service.ResolvePublicSlug(r.Context(), slug)
		if err != nil {
			publicError(w, r, err)
			return
		}
		if resolved != slug {
			target := publicPath(locale, kind, resolved, document)
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
	}
	canonicalPath := publicPath(locale, kind, slug, document)
	if r.URL.Path != canonicalPath && r.URL.Path != "/" {
		target := canonicalPath
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	environment := "live"
	if r.URL.Query().Get("preview") == "sandbox" {
		environment = "sandbox"
	}
	site, err := a.service.Site(r.Context(), environment, slug, locale)
	if err != nil {
		publicError(w, r, err)
		return
	}
	if !site.Installed {
		http.Error(w, "Storefront is not configured", 503)
		return
	}
	seo, err := a.service.PublicSEOMetadata(r.Context(), site, kind, document)
	if err != nil {
		publicError(w, r, err)
		return
	}
	view := publicPageView{Site: site, SEO: seo, Kind: kind, Language: locale, Home: publicPath(locale, "home", "", ""), Plans: []publicPlan{}, Links: []publicLink{}}
	if site.Product != nil {
		view.ProductURL, view.DownloadURL, view.DocsURL = publicPath(locale, "product", site.Product.Slug, ""), publicPath(locale, "download", site.Product.Slug, ""), publicPath(locale, "docs", site.Product.Slug, "")
		view.Documents = productDocuments(site, locale, site.Product.Slug)
		if kind == "docs" && document != "" {
			for _, d := range view.Documents {
				if d.Key == document {
					view.DocumentTitle, view.DocumentBody = d.Title, d.Body
				}
			}
			if view.DocumentTitle == "" {
				http.NotFound(w, r)
				return
			}
			view.SEO.Title = view.DocumentTitle + " · " + site.Product.Name + " | " + site.Brand.Name
		}
		for _, plan := range site.Plans {
			_, price := publicPrice(plan)
			view.Plans = append(view.Plans, publicPlan{Name: plan.Name, Summary: plan.Summary, Price: price, Highlights: plan.Highlights, CanBuy: plan.AllowPurchase && !plan.PreviewOnly, BuyURL: "/#/checkout/" + site.Product.Slug + "/" + plan.VersionID})
		}
	}
	// Only advertise translations whose actual public content is ready.
	for _, candidate := range site.SupportedLocales {
		other := site
		if candidate.Code != locale {
			other, err = a.service.Site(r.Context(), "live", slug, candidate.Code)
			if err != nil {
				publicError(w, r, err)
				return
			}
		}
		if !other.TranslationReady && candidate.Code != other.SourceLocale {
			continue
		}
		metadata, e := a.service.PublicSEOMetadata(r.Context(), other, kind, document)
		if e != nil {
			publicError(w, r, e)
			return
		}
		if metadata.NoIndex {
			continue
		}
		view.Links = append(view.Links, publicLink{Name: candidate.Name, Locale: candidate.Code, URL: a.service.PublicURL() + publicPath(candidate.Code, kind, slug, document)})
	}
	if !view.SEO.NoIndex {
		w.Header().Del("X-Robots-Tag")
	}
	if site.Product != nil && kind == "product" {
		data := map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": site.Product.Name, "description": view.SEO.Description, "url": view.SEO.Canonical}
		offers := []map[string]any{}
		for _, plan := range site.Plans {
			if plan.AllowPurchase && !plan.PreviewOnly {
				amount, _ := publicPrice(plan)
				offers = append(offers, map[string]any{"@type": "Offer", "name": plan.Name, "priceCurrency": plan.Currency, "price": publicAmount(amount, plan.Currency), "url": view.SEO.Canonical})
			}
		}
		if len(offers) > 0 {
			data["offers"] = offers
		}
		raw, _ := json.Marshal(data)
		view.JSONLD = template.JS(raw)
	}
	var body bytes.Buffer
	if err := publicTemplate.ExecuteTemplate(&body, "public.html", view); err != nil {
		http.Error(w, "Page unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func publicAmount(amount int64, currency string) string {
	exp := 2
	switch currency {
	case "JPY", "KRW", "VND", "CLP", "ISK":
		exp = 0
	case "BHD", "JOD", "KWD", "OMR", "TND":
		exp = 3
	}
	div := int64(1)
	for range exp {
		div *= 10
	}
	if exp == 0 {
		return fmt.Sprint(amount)
	}
	return fmt.Sprintf("%d.%0*d", amount/div, exp, amount%div)
}

func publicError(w http.ResponseWriter, r *http.Request, err error) {
	var business *app.Error
	if errors.As(err, &business) && business.Status == 404 {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "Storefront temporarily unavailable", http.StatusServiceUnavailable)
}

type sitemapURL struct {
	Loc        string             `xml:"loc"`
	Alternates []sitemapAlternate `xml:"xhtml:link,omitempty"`
}
type sitemapAlternate struct {
	Rel    string `xml:"rel,attr"`
	Locale string `xml:"hreflang,attr"`
	Href   string `xml:"href,attr"`
}
type sitemapDocument struct {
	XMLName   xml.Name     `xml:"urlset"`
	Namespace string       `xml:"xmlns,attr"`
	XHTML     string       `xml:"xmlns:xhtml,attr"`
	URLs      []sitemapURL `xml:"url"`
}

func (a *application) publicSitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")
	if a.service == nil {
		http.Error(w, "Storefront unavailable", 503)
		return
	}
	base, err := a.service.Site(r.Context(), "live", "", "")
	if err != nil || !base.Installed {
		http.Error(w, "Storefront unavailable", 503)
		return
	}
	if a.service.PublicURL() == "" {
		http.Error(w, "Public URL is not configured", 503)
		return
	}
	groups := map[string][]sitemapURL{}
	for _, locale := range base.SupportedLocales {
		for _, slug := range append([]string{""}, publicProductSlugs(base)...) {
			site, e := a.service.Site(r.Context(), "live", slug, locale.Code)
			if e != nil {
				publicError(w, r, e)
				return
			}
			if !site.TranslationReady && site.Locale != site.SourceLocale {
				continue
			}
			pages := []struct{ kind, document string }{{"home", ""}}
			if slug != "" {
				pages = []struct{ kind, document string }{{"product", ""}}
				if site.Product != nil && len(site.Product.Downloads) > 0 {
					pages = append(pages, struct{ kind, document string }{"download", ""})
				}
				docs := productDocuments(site, locale.Code, slug)
				if len(docs) > 0 {
					pages = append(pages, struct{ kind, document string }{"docs", ""})
					for _, d := range docs {
						pages = append(pages, struct{ kind, document string }{"docs", d.Key})
					}
				}
			}
			for _, page := range pages {
				metadata, e := a.service.PublicSEOMetadata(r.Context(), site, page.kind, page.document)
				if e != nil {
					publicError(w, r, e)
					return
				}
				if metadata.NoIndex {
					continue
				}
				loc := a.service.PublicURL() + publicPath(locale.Code, page.kind, slug, page.document)
				if metadata.Canonical != loc {
					continue
				}
				key := slug + "|" + page.kind + "|" + page.document
				groups[key] = append(groups[key], sitemapURL{Loc: loc, Alternates: []sitemapAlternate{{Rel: "alternate", Locale: locale.Code, Href: loc}}})
			}
		}
	}
	doc := sitemapDocument{Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9", XHTML: "http://www.w3.org/1999/xhtml"}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		links := []sitemapAlternate{}
		for _, row := range groups[key] {
			links = append(links, row.Alternates...)
		}
		for _, row := range groups[key] {
			row.Alternates = links
			doc.URLs = append(doc.URLs, row)
		}
	}
	if len(doc.URLs) > 50000 {
		http.Error(w, "Sitemap exceeds the supported size", 503)
		return
	}
	raw, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		http.Error(w, "Sitemap unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(append([]byte(xml.Header), raw...))
}

func publicProductSlugs(site app.SiteView) []string {
	out := []string{}
	for _, p := range site.Products {
		out = append(out, p.Slug)
	}
	return out
}
