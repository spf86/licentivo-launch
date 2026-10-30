package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"gorm.io/gorm"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/config"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/storage"
)

var version = "0.3.5-beta"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("launch stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	command, archivePath, configPath, err := parseCommand(os.Args[1:])
	if err != nil {
		return err
	}
	getenv := func(key string) string {
		if key == "LAUNCH_CONFIG_FILE" && configPath != "" {
			return configPath
		}
		return os.Getenv(key)
	}
	if command == "dev" {
		if getenv("LAUNCH_CONFIG_FILE") == "" {
			path, err := config.DefaultPath(".")
			if err != nil {
				return err
			}
			if path == "" {
				return errors.New("configuration is missing; run launch init and edit config.yaml")
			}
		}
		command = "serve"
	}
	if command == "init" {
		if err := initializeConfigurationAt(".", getenv("LAUNCH_CONFIG_FILE")); err != nil {
			return err
		}
		if getenv("LAUNCH_CONFIG_FILE") == "" {
			fmt.Fprintln(os.Stdout, "Created config.yaml with a persistent master key. Edit database_dsn in that file, then run: launch serve")
		} else {
			fmt.Fprintln(os.Stdout, "Created private YAML configuration with a persistent master key. Edit database_dsn, then start with the same --config path or LAUNCH_CONFIG_FILE.")
		}
		return nil
	}
	if command == "keygen" {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return errors.New("could not generate encryption key")
		}
		fmt.Fprintln(os.Stdout, base64.StdEncoding.EncodeToString(key[:]))
		return nil
	}
	if command == "version" {
		fmt.Fprintln(os.Stdout, version)
		return nil
	}
	lookup, err := config.Lookup(getenv)
	if err != nil {
		return err
	}
	if command == "backup" || command == "restore" {
		return runArchive(context.Background(), command, archivePath, lookup, logger)
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		return err
	}
	var db *gorm.DB
	if cfg.DatabaseDSN != "" {
		db, err = storage.Open(cfg.DatabaseDSN)
		if err != nil {
			return err
		}
		defer storage.Close(db)
	}
	if command == "check" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err = storage.Ready(ctx, db)
		if err != nil {
			return err
		}
		logger.Info("database command complete", "command", command)
		return nil
	}
	var service *app.Service
	var activeTLSCertificate atomic.Pointer[tls.Certificate]
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := storage.Initialize(ctx, db)
		cancel()
		if err != nil {
			if errors.Is(err, storage.ErrUnavailable) {
				return errors.New("could not connect to MySQL; check the configured database server and network")
			}
			return err
		}
		logger.Info("database tables initialized")
		service, err = app.New(context.Background(), db, app.Config{
			PublicURL:           cfg.PublicURL,
			UploadDir:           cfg.UploadDir,
			MasterKey:           cfg.MasterKey,
			TLSListenerEnabled:  cfg.HTTPSAddr != "",
			ApplyTLSCertificate: func(certificate *tls.Certificate) { activeTLSCertificate.Store(certificate) },
		})
		if err != nil {
			return err
		}
		created, err := service.EnsureInitialAdministrator(context.Background(), cfg.AdminEmail, cfg.AdminPassword)
		if err != nil {
			return err
		}
		if created {
			address, password := cfg.AdminEmail, cfg.AdminPassword
			if address == "" {
				address = app.DefaultAdminEmail
			}
			if password == "" {
				password = app.DefaultAdminPassword
			}
			fmt.Fprintf(os.Stdout, "Initial administrator account created.\nEmail: %s\nPassword: %s\nSign in at %s/#/login and change this password to unlock administration.\n", address, password, cfg.PublicURL)
		}
	}
	publicURLProvider := func() string { return cfg.PublicURL }
	if service != nil {
		publicURLProvider = service.PublicURL
	}
	applicationHandler := httpapi.Handler(httpapi.Options{Version: version, Logger: logger, App: service, PublicURL: cfg.PublicURL, PublicURLProvider: publicURLProvider, Ready: func(ctx context.Context) error { return storage.Ready(ctx, db) }})
	redirect := func(w http.ResponseWriter, r *http.Request, destination string) {
		// Keep the redirect reversible because an administrator can change the
		// selected protocol from System Settings while diagnosing a deployment.
		http.Redirect(w, r, destination+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicURL := publicURLProvider()
		if cfg.HTTPSAddr != "" && activeTLSCertificate.Load() != nil && strings.HasPrefix(publicURL, "https://") && r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			redirect(w, r, publicURL)
			return
		}
		applicationHandler.ServeHTTP(w, r)
	})
	httpsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicURL := publicURLProvider()
		if strings.HasPrefix(publicURL, "http://") && r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			redirect(w, r, publicURL)
			return
		}
		applicationHandler.ServeHTTP(w, r)
	})
	newServer := func(address string, handler http.Handler) *http.Server {
		return &http.Server{
			Addr: address, Handler: handler,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Minute, WriteTimeout: 15 * time.Minute,
			IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
		}
	}
	httpServer := newServer(cfg.HTTPAddr, httpHandler)
	servers := []*http.Server{httpServer}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if service != nil {
		go service.RunCommerce(ctx)
		go service.RunMail(ctx)
	}
	type listenerResult struct {
		name string
		err  error
	}
	errorsCh := make(chan listenerResult, 2)
	go func() { errorsCh <- listenerResult{name: "HTTP", err: httpServer.ListenAndServe()} }()
	logger.Info("server starting", "address", cfg.HTTPAddr, "environment", cfg.Environment, "version", version)
	if cfg.HTTPSAddr != "" {
		httpsServer := newServer(cfg.HTTPSAddr, httpsHandler)
		httpsServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				certificate := activeTLSCertificate.Load()
				if certificate == nil {
					return nil, errors.New("managed TLS certificate is not configured")
				}
				return certificate, nil
			},
		}
		servers = append(servers, httpsServer)
		go func() { errorsCh <- listenerResult{name: "HTTPS", err: httpsServer.ListenAndServeTLS("", "")} }()
		logger.Info("HTTPS listener starting", "address", cfg.HTTPSAddr, "certificate_configured", activeTLSCertificate.Load() != nil)
	}
	select {
	case result := <-errorsCh:
		for _, running := range servers {
			_ = running.Close()
		}
		if !errors.Is(result.err, http.ErrServerClosed) {
			return fmt.Errorf("%s listener failed; check bind address and port", result.name)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, running := range servers {
			if err := running.Shutdown(shutdownCtx); err != nil {
				for _, remaining := range servers {
					_ = remaining.Close()
				}
				return errors.New("server shutdown timed out")
			}
		}
		return nil
	}
}
