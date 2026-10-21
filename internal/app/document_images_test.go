package app

import (
	"crypto/sha256"
	"strings"
	"testing"

	"licentivo-launch/internal/storage"
)

func TestDocumentImageNameValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{name: "安装步骤.png", valid: true},
		{name: "screen.JPEG", valid: true},
		{name: "animation.gif", valid: true},
		{name: "diagram.webp", valid: true},
		{name: "manual.svg", valid: false},
		{name: "../screen.png", valid: false},
		{name: "screen.png.exe", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := validateDocumentImageName(test.name)
			if (err == nil) != test.valid {
				t.Fatalf("validateDocumentImageName(%q) error = %v, valid = %v", test.name, err, test.valid)
			}
		})
	}
}

func TestProductDocumentImageView(t *testing.T) {
	checksum := sha256.Sum256([]byte("image"))
	view := productDocumentImageView(storage.ProductDocumentImage{ID: "image-id", FileName: "screen.png", ContentType: "image/png", SizeBytes: 5, SHA256: checksum[:]})
	if view.URL != "/api/v1/product-document-images/image-id" || view.FileName != "screen.png" || view.ContentType != "image/png" || view.SizeBytes != 5 || view.SHA256 != strings.ToLower(view.SHA256) || len(view.SHA256) != sha256.Size*2 {
		t.Fatalf("document image metadata was not exposed correctly: %+v", view)
	}
}
