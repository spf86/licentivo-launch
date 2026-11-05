package app

import "testing"

func TestSEODefaultsStayWithinTheirLanguage(t *testing.T) {
	legacy := readSiteSEO([]byte(`{"defaults":{"product_title_template":"{product} 正版软件","description":"中文介绍","keywords":["软件授权"],"share_image_url":"https://example.test/image.png"}}`))
	if got := seoDefaultsForLocale(legacy, "zh-CN", "zh-CN"); len(got.Keywords) != 1 || got.Description != "中文介绍" {
		t.Fatal("legacy source-language SEO was lost")
	}
	if got := seoDefaultsForLocale(legacy, "ja", "zh-CN"); len(got.Keywords) != 0 || got.Description != "" || got.ProductTitleTemplate != "{product} | {brand}" || got.ShareImageURL == "" {
		t.Fatal("source-language SEO leaked into another market")
	}
	legacy.LocaleDefaults["ja"] = SEODefaults{Description: "日本語の説明", Keywords: []string{"正規ライセンス"}, ProductTitleTemplate: "{product} | {brand}"}
	if got := seoDefaultsForLocale(legacy, "ja", "zh-CN"); got.Description != "日本語の説明" || got.Keywords[0] != "正規ライセンス" {
		t.Fatal("language-specific defaults were ignored")
	}
	if got := seoDefaultsForLocale(legacy, "en", "zh-CN"); len(got.Keywords) != 0 || got.Description != "" {
		t.Fatal("saving Japanese defaults changed English defaults")
	}
}

func TestSEORejectsUnsafeURLsAndNormalizesKeywordPlanning(t *testing.T) {
	for _, p := range []PageSEO{{CanonicalPath: "//attacker.invalid/page"}, {CanonicalPath: "https://attacker.invalid"}, {CanonicalPath: "/page?session=secret"}, {ShareImageURL: "javascript:alert(1)"}, {ShareImageURL: "https://user:pass@example.com/image"}, {Title: "One\nTwo"}, {Keywords: []string{"one\ntwo"}}} {
		if _, err := normalizeSEOPage(p); err == nil {
			t.Fatalf("unsafe metadata accepted: %+v", p)
		}
	}
	p, err := normalizeSEOPage(PageSEO{Title: "  My App  ", CanonicalPath: "/en/products/my-app", Keywords: []string{" Export ", "export", "Reports", ""}})
	if err != nil || p.Title != "My App" || len(p.Keywords) != 2 || p.Keywords[0] != "Export" {
		t.Fatalf("normalization failed: %+v %v", p, err)
	}
}
