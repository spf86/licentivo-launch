package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/term"
	"licentivo-launch/pkg/launchclient"
	licentivo "licentivo/sdk/go"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: go run . MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live] [--sign-out]")
	}
	origin, product, path := os.Args[1], os.Args[2], os.Args[3]
	environment := "live"
	if len(os.Args) > 4 {
		environment = os.Args[4]
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Email: ")
	email, _ := reader.ReadString('\n')
	fmt.Print("Password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("use an interactive terminal to enter the password")
	}
	defer clear(password)
	merchant, err := launchclient.New(origin, environment == "sandbox")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	user, err := merchant.Login(ctx, strings.TrimSpace(email), string(password))
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = merchant.SignOut(cleanup)
	}()
	credential, err := merchant.Credential(ctx, product, environment)
	if err != nil {
		return fmt.Errorf("%w; view purchases and authorization status at %s/#/account", err, origin)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg licentivo.Config
	if json.Unmarshal(raw, &cfg) != nil {
		return fmt.Errorf("invalid public product.json")
	}
	cfg.LicenseKey = credential.LicenseKey
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	scope := sha256.Sum256([]byte(origin + "\n" + user.ID + "\n" + product + "\n" + environment))
	dir := filepath.Join(cacheRoot, "LicentivoLaunch", hex.EncodeToString(scope[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cfg.CachePath = filepath.Join(dir, "lease.json")
	sdk, err := licentivo.New(cfg)
	if err != nil {
		return err
	}
	defer sdk.Close()
	if _, err := sdk.Start(context.Background(), func(error) { fmt.Fprintln(os.Stderr, "Authorization changed; refresh the application status") }); err != nil {
		return err
	}
	if err := sdk.RunFeature("export", func() error { return os.WriteFile("licensed-report.txt", []byte("Protected report\n"), 0600) }); err != nil {
		return err
	}
	fmt.Println("Protected export completed. The license key stays inside this process.")
	if len(os.Args) > 5 && os.Args[5] == "--sign-out" {
		sdk.Close()
		for _, file := range []string{cfg.CachePath, cfg.CachePath + ".credential.json"} {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
