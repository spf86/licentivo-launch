// A bearer-authenticated backend, with real SDK checks before business work.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"licentivo-launch/pkg/launchclient"
	licentivo "licentivo/sdk/go"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	origin, product := os.Getenv("MERCHANT_ORIGIN"), os.Getenv("LAUNCH_PRODUCT_ID")
	environment := os.Getenv("LAUNCH_EXAMPLE_ENV")
	if environment == "" {
		environment = "live"
	}
	merchant, err := launchclient.New(origin, environment == "sandbox")
	if err != nil {
		panic(err)
	}
	public, err := os.ReadFile(os.Getenv("PRODUCT_CONFIG"))
	if err != nil {
		panic("set PRODUCT_CONFIG to the public product.json")
	}
	var template licentivo.Config
	if json.Unmarshal(public, &template) != nil {
		panic("invalid public product config")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) > 256 {
			http.Error(w, "Bearer authentication required", 401)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		customer := merchant.WithToken(strings.TrimPrefix(authorization, "Bearer "))
		user, err := customer.CurrentUser(ctx)
		if err != nil {
			http.Error(w, "Authentication failed", 401)
			return
		}
		credential, err := customer.Credential(ctx, product, environment)
		if err != nil {
			http.Error(w, "Purchased entitlement is unavailable", 403)
			return
		}
		// Identity comes from merchant authentication, never a user_id in the body.
		cfg := template
		cfg.LicenseKey = credential.LicenseKey
		scope := sha256.Sum256([]byte(origin + "\n" + user.ID + "\n" + product + "\n" + environment))
		root, err := os.UserCacheDir()
		if err != nil {
			http.Error(w, "Private cache unavailable", 503)
			return
		}
		dir := filepath.Join(root, "LaunchWebExample", hex.EncodeToString(scope[:]))
		if os.MkdirAll(dir, 0700) != nil {
			http.Error(w, "Private cache unavailable", 503)
			return
		}
		cfg.CachePath = filepath.Join(dir, "lease.json")
		sdk, err := licentivo.New(cfg)
		if err != nil {
			http.Error(w, "Authorization unavailable", 503)
			return
		}
		defer sdk.Close()
		if _, err := sdk.Start(ctx, nil); err != nil {
			http.Error(w, "Authorization denied", 403)
			return
		}
		err = sdk.RunFeature("export", func() error {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, err := fmt.Fprintln(w, "Protected report")
			return err
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			http.Error(w, "Feature denied", 403)
		}
	})
	server := &http.Server{Addr: "127.0.0.1:8090", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}
	fmt.Println("Example backend at http://127.0.0.1:8090; POST /export with the caller's Launch desktop bearer token")
	if err := server.ListenAndServe(); err != nil {
		panic("example HTTP server stopped")
	}
}
