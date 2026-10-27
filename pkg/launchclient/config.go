package launchclient

import (
	"encoding/json"
	"errors"
	"os"
)

// PublicConfig is the non-secret launch-config.json exported by the integration wizard.
// ProductID names Launch's catalog product; LicentivoProductID is the distinct
// runtime product and must match the official product.json.
type PublicConfig struct {
	SchemaVersion      int    `json:"schema_version"`
	MerchantOrigin     string `json:"merchant_origin"`
	ProductID          string `json:"launch_product_id"`
	LicentivoProductID string `json:"licentivo_product_id"`
	Environment        string `json:"environment"`
	AllowHTTP          bool   `json:"allow_http,omitempty"`
}

func LoadPublicConfig(path string) (PublicConfig, error) {
	var config PublicConfig
	info, err := os.Stat(path)
	if err != nil {
		return config, err
	}
	if info.Size() > 64<<10 {
		return config, errors.New("launch-config.json is too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if json.Unmarshal(raw, &config) != nil || config.SchemaVersion != 1 || !productIDPattern.MatchString(config.ProductID) || !productIDPattern.MatchString(config.LicentivoProductID) || config.Environment != "sandbox" && config.Environment != "live" {
		return PublicConfig{}, errors.New("invalid public launch-config.json")
	}
	if config.AllowHTTP && config.Environment != "sandbox" {
		return PublicConfig{}, errors.New("HTTP opt-in is restricted to sandbox")
	}
	if _, err = newClient(config.MerchantOrigin, config.Environment == "sandbox", config.AllowHTTP); err != nil {
		return PublicConfig{}, err
	}
	return config, nil
}
