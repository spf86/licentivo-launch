package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
	"licentivo-launch/pkg/launchclient"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var positional []string
	var launchPath string
	remember, noRemember, logout, release := false, false, false, false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--remember":
			remember = true
		case "--no-remember":
			noRemember = true
		case "--logout", "--sign-out":
			logout = true
		case "--release-device":
			release = true
		case "--config":
			i++
			if i >= len(os.Args) {
				return errors.New("--config needs a launch-config.json path")
			}
			launchPath = os.Args[i]
		default:
			positional = append(positional, os.Args[i])
		}
	}
	if remember && noRemember || logout && release {
		return errors.New("choose one persistence mode and one account action")
	}
	var origin, product, productPath, runtimeProduct string
	environment := "live"
	if launchPath != "" {
		if len(positional) != 1 {
			return errors.New("usage: go run . --config launch-config.json product.json [--remember|--no-remember] [--logout|--release-device]")
		}
		config, err := launchclient.LoadPublicConfig(launchPath)
		if err != nil {
			return err
		}
		origin, product, environment, runtimeProduct = config.MerchantOrigin, config.ProductID, config.Environment, config.LicentivoProductID
		productPath = positional[0]
	} else {
		if len(positional) < 3 || len(positional) > 4 {
			return errors.New("usage: go run . MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live] [--remember|--no-remember] [--logout|--release-device]")
		}
		origin, product, productPath = positional[0], positional[1], positional[2]
		if len(positional) == 4 {
			environment = positional[3]
		}
	}
	public, err := os.ReadFile(productPath)
	if err != nil {
		return err
	}
	var template launchclient.ProductConfig
	if json.Unmarshal(public, &template) != nil {
		return errors.New("invalid public product.json")
	}
	if runtimeProduct != "" && template.ProductID != runtimeProduct {
		return errors.New("product.json does not match launch-config.json's Licentivo product")
	}
	if environment == "live" {
		template.AllowHTTP = false
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	root = filepath.Join(root, "LicentivoLaunch")
	var store launchclient.SessionStore = &launchclient.MemoryStore{}
	if !noRemember {
		store, err = launchclient.NewSystemSessionStore(filepath.Join(root, "sessions"))
		if err != nil {
			if remember || !errors.Is(err, launchclient.ErrSecureStoreUnavailable) {
				return err
			}
			fmt.Fprintln(os.Stderr, "Remembered login unavailable on this OS; using memory-only login. Supply a Keychain/Secret Service SessionStore to enable it.")
			store = &launchclient.MemoryStore{}
		}
	}
	app, err := launchclient.NewApplication(launchclient.Config{
		Origin: origin, ProductID: product, Environment: environment, AllowLocalHTTP: environment == "sandbox",
		Store: store, CacheDir: filepath.Join(root, "runtime"), Product: template,
		OnRuntimeError: func(error) {
			fmt.Fprintln(os.Stderr, "Authorization changed; the next protected operation will check the SDK again.")
		},
	})
	if err != nil {
		return err
	}
	defer app.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if logout {
		cleanup, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		if err := app.Logout(cleanup); err != nil {
			return fmt.Errorf("sign-out or cleanup was not fully confirmed: %w", err)
		}
		fmt.Println("Signed out; the official SDK handled device revocation and the Launch account was cleared.")
		return nil
	}
	_, err = app.Restore(ctx)
	if errors.Is(err, launchclient.ErrNoSession) || errors.Is(err, launchclient.ErrSessionExpired) {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Email: ")
		email, readErr := reader.ReadString('\n')
		if readErr != nil {
			return readErr
		}
		fmt.Print("Password: ")
		password, readErr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if readErr != nil {
			return errors.New("use an interactive terminal to enter the password")
		}
		_, err = app.Login(ctx, strings.TrimSpace(email), string(password))
		clear(password)
	}
	if err != nil {
		return explain(err, origin)
	}
	if release {
		cleanup, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		if err := app.ReleaseDevice(cleanup); err != nil {
			return fmt.Errorf("device release or local cleanup was not fully confirmed: %w", err)
		}
		fmt.Println("Device released and signed out.")
		return nil
	}
	if err := app.RunFeature("export", func() error { return os.WriteFile("licensed-report.txt", []byte("Protected report\n"), 0600) }); err != nil {
		return err
	}
	fmt.Println("Protected export completed. Close keeps remembered login; use --logout or --release-device for explicit SDK logout.")
	return nil
}

func explain(err error, origin string) error {
	var api *launchclient.APIError
	if errors.As(err, &api) {
		message := map[string]string{"ENTITLEMENT_PENDING": "Payment received; authorization is still being synchronized. Retry shortly.", "ENTITLEMENT_EXPIRED": "Authorization expired; renew your purchase.", "ENTITLEMENT_SUSPENDED": "Authorization is suspended; contact the merchant.", "ENTITLEMENT_NOT_PURCHASED": "This account has not purchased this product."}[api.Code]
		if message != "" {
			return fmt.Errorf("%s Account: %s/#/account (%w)", message, strings.TrimRight(origin, "/"), err)
		}
	}
	return err
}
