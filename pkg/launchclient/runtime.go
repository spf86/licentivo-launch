package launchclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"licentivo-launch/pkg/launchclient/internal/licentivo"
)

// ProductConfig is the public runtime configuration; no separate SDK import is required.
type ProductConfig = licentivo.Config
type RuntimeAPIError = licentivo.APIError

// Connect initializes Launch directly from public parameters. The merchant origin
// is mandatory; the independent licensing service is managed inside the package.
func Connect(merchantOrigin, productID, licentivoProductID string, trustedKeys map[string]string, options Config) (*Application, error) {
	options.Origin, options.ProductID = merchantOrigin, productID
	if options.Environment == "" {
		options.Environment = "live"
	}
	options.AllowLocalHTTP = options.Environment == "sandbox"
	options.Product = ProductConfig{BaseURL: "https://www.licentivo.com", ProductID: licentivoProductID, TrustedKeys: trustedKeys}
	return NewApplication(options)
}

type officialRuntime struct{ client *licentivo.Client }

func (r *officialRuntime) Start(ctx context.Context, changed func(error)) error {
	_, err := r.client.Start(ctx, changed)
	return err
}
func (r *officialRuntime) RunFeature(feature string, action func() error) error {
	return r.client.RunFeature(feature, action)
}
func (r *officialRuntime) Deactivate(ctx context.Context) error {
	result, err := r.client.Logout(ctx)
	if err != nil {
		return err
	}
	if result.RevocationPending {
		return ErrRevocationPending
	}
	return nil
}

func (r *officialRuntime) Close() { r.client.Close() }

// NewRuntime owns the official SDK adapter used by desktop and trusted backend applications.
func NewRuntime(product ProductConfig, runtime RuntimeConfig) (Runtime, error) {
	if runtime.Identity.LicentivoProductID != product.ProductID || runtime.Identity.Environment != runtime.Environment {
		return nil, errors.New("identity routing context does not match public product configuration")
	}
	product.LicenseKey = ""
	product.IdentityToken = runtime.Identity.IdentityToken
	product.LaunchConnectionID = runtime.Identity.LaunchConnectionID
	product.Environment = runtime.Environment
	product.CachePath = runtime.CachePath
	if runtime.Environment == "live" {
		product.AllowHTTP = false
	}
	client, err := licentivo.New(product)
	if err != nil {
		return nil, err
	}
	return &officialRuntime{client: client}, nil
}

// NewApplicationFromFiles is the normal application entry point. The wrapper
// reads both public files, verifies their mapping and owns the SDK adapter.
func NewApplicationFromFiles(launchPath, productPath string, options Config) (*Application, error) {
	launch, err := LoadPublicConfig(launchPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(productPath)
	if err != nil {
		return nil, errors.New("could not read public product configuration")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 64<<10 {
		return nil, errors.New("invalid public product configuration size")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var product ProductConfig
	var extra any
	if decoder.Decode(&product) != nil || decoder.Decode(&extra) != io.EOF || product.ProductID != launch.LicentivoProductID || product.LicenseKey != "" || product.CachePath != "" || product.IdentityToken != "" || product.LaunchConnectionID != "" || product.Environment != "" || product.DeviceID != "" || product.DeviceName != "" {
		return nil, errors.New("invalid or mismatched public product configuration")
	}
	options.Origin, options.ProductID, options.Environment = launch.MerchantOrigin, launch.ProductID, launch.Environment
	options.AllowHTTP = launch.AllowHTTP
	options.AllowLocalHTTP, options.Product = launch.Environment == "sandbox", product
	if options.CacheDir == "" {
		root, err := os.UserCacheDir()
		if err != nil {
			return nil, errors.New("could not resolve private application cache")
		}
		options.CacheDir = filepath.Join(root, "LicentivoLaunch", "runtime")
	}
	return NewApplication(options)
}
