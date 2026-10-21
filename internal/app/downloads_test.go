package app

import (
	"crypto/sha256"
	"strings"
	"testing"

	"licentivo-launch/internal/storage"
)

func TestProductUploadValidation(t *testing.T) {
	for _, test := range []struct {
		platform string
		name     string
		valid    bool
	}{
		{platform: "windows", name: "Setup.exe", valid: true},
		{platform: "windows", name: "Setup.msi", valid: true},
		{platform: "macos", name: "Editor.dmg", valid: true},
		{platform: "ios", name: "Editor.ipa", valid: true},
		{platform: "android", name: "Editor.aab", valid: true},
		{platform: "android", name: "Editor.exe", valid: false},
		{platform: "windows", name: "../Setup.exe", valid: false},
		{platform: "macos", name: "Editor.app", valid: false},
	} {
		t.Run(test.platform+"/"+test.name, func(t *testing.T) {
			if err := validateProductPlatform(test.platform); err != nil {
				t.Fatal(err)
			}
			_, _, err := validateUploadName(test.platform, test.name)
			if (err == nil) != test.valid {
				t.Fatalf("validateUploadName(%q, %q) error = %v, valid = %v", test.platform, test.name, err, test.valid)
			}
		})
	}
	if err := validateProductPlatform("linux"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestProductDownloadVersionValidation(t *testing.T) {
	version, err := validateProductVersion(" 2.4.1 ")
	if err != nil || version != "2.4.1" {
		t.Fatalf("version was not normalized: %q, %v", version, err)
	}
	if _, err := validateProductVersion(""); err == nil {
		t.Fatal("blank platform version accepted")
	}
}

func TestProductDownloadChecksumValidationAndView(t *testing.T) {
	value := strings.Repeat("a1", sha256.Size)
	decoded, err := validateDownloadSHA256("  " + strings.ToUpper(value) + " ")
	if err != nil || len(decoded) != sha256.Size {
		t.Fatalf("valid checksum was rejected: %v", err)
	}
	if _, err := validateDownloadSHA256("not-a-sha256"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	view := productDownloadView(storage.ProductDownload{Source: "external", SHA256: decoded, SizeBytes: 123})
	if view.SHA256 != value || view.SizeBytes != 123 {
		t.Fatalf("download metadata was not exposed: %+v", view)
	}
}
