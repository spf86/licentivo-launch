package app

import (
	"context"
	"encoding/json"
	"testing"

	"licentivo-launch/internal/ports"
)

type contentTranslationStub struct{}

func (contentTranslationStub) Usage(context.Context) (ports.TranslationUsage, error) {
	return ports.TranslationUsage{}, nil
}

func (contentTranslationStub) Translate(_ context.Context, _, _ string, texts []string) ([]string, error) {
	translated := make([]string, len(texts))
	for index, value := range texts {
		translated[index] = "translated:" + value
	}
	return translated, nil
}

func TestTranslateProductDocumentsAndPlatformRequirements(t *testing.T) {
	source := TranslationPayload{
		PlatformRequirements: map[string]string{"windows": "Windows 11"},
		InstallationGuide:    "Install it\n![Setup screen](/api/v1/product-document-images/image-id)\nContinue",
		UpgradeGuide:         "Upgrade it",
		UninstallGuide:       "Remove it",
		TroubleshootingGuide: "Fix it",
		TermsOfService:       "Terms",
		PrivacyPolicy:        "Privacy",
		RefundPolicy:         "Refunds",
		InvoiceGuide:         "Invoices",
		LegalTaxNotice:       "Legal notice",
	}
	raw, err := translatePayload(context.Background(), contentTranslationStub{}, "ZH", "EN", marshal(source))
	if err != nil {
		t.Fatal(err)
	}
	var translated TranslationPayload
	if err := json.Unmarshal(raw, &translated); err != nil {
		t.Fatal(err)
	}
	if translated.PlatformRequirements["windows"] != "translated:Windows 11" || translated.InstallationGuide != "translated:Install it\n![Setup screen](/api/v1/product-document-images/image-id)\ntranslated:Continue" || translated.TermsOfService != "translated:Terms" || translated.InvoiceGuide != "translated:Invoices" || translated.LegalTaxNotice != "translated:Legal notice" {
		t.Fatalf("product documents were not translated: %+v", translated)
	}
}
