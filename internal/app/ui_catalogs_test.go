package app

import "testing"

func TestBuiltInUICatalogsCoverSupportedLocales(t *testing.T) {
	expectedProducts := map[string]string{
		"zh-CN": "产品",
		"en":    "Products",
		"ja":    "製品",
		"ko":    "제품",
		"de":    "Produkte",
		"fr":    "Produits",
		"es":    "Productos",
		"pt-BR": "Produtos",
		"it":    "Prodotti",
		"ru":    "Продукты",
	}
	for _, locale := range localeOptions {
		ui := builtInUIStrings(locale.Code)
		if len(ui) != len(siteUIStrings) {
			t.Fatalf("%s UI catalog has %d entries, want %d", locale.Code, len(ui), len(siteUIStrings))
		}
		for key := range siteUIStrings {
			if ui[key] == "" {
				t.Fatalf("%s UI catalog is missing %q", locale.Code, key)
			}
		}
		if ui["nav.products"] != expectedProducts[locale.Code] {
			t.Fatalf("%s nav.products = %q", locale.Code, ui["nav.products"])
		}
	}
}
