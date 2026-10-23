package httpapi

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/storage"
)

//go:embed web/*
var assets embed.FS

type Options struct {
	Version           string
	Logger            *slog.Logger
	Ready             func(context.Context) error
	App               *app.Service
	PublicURL         string
	PublicURLProvider func() string
	SecureCookies     bool
}

func Handler(options Options) http.Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	mux := http.NewServeMux()
	publicURLProvider := options.PublicURLProvider
	if publicURLProvider == nil {
		publicURLProvider = func() string { return options.PublicURL }
	}
	application := &application{service: options.App, publicURL: publicURLProvider, forceSecureCookies: options.SecureCookies}
	application.register(mux)
	application.registerPaddleCheckout(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		err := storage.ErrNotConfigured
		if options.Ready != nil {
			err = options.Ready(ctx)
		}
		if err != nil {
			code := "DATABASE_UNAVAILABLE"
			if errors.Is(err, storage.ErrNotConfigured) {
				code = "DATABASE_NOT_CONFIGURED"
			}
			if errors.Is(err, storage.ErrSchema) {
				code = "DATABASE_SCHEMA_NOT_READY"
			}
			respond(w, r, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "code": code})
			return
		}
		respond(w, r, http.StatusOK, map[string]any{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]any{
			"product": "Licentivo Launch", "version": options.Version, "phase": "local-development",
			"sales_enabled": false, "sandbox_checkout_implemented": true, "implemented": []string{"health", "gorm_models", "embedded_web", "setup", "accounts", "catalog", "payments", "renewal", "upgrade", "refunds", "devices", "encrypted_backup"},
		})
	})
	webFS, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(webFS)))
	pageHandler := func(w http.ResponseWriter, r *http.Request) {
		page, err := assets.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "页面暂不可用", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}
	for _, path := range []string{"GET /{$}", "GET /account", "GET /admin", "GET /setup"} {
		mux.HandleFunc(path, pageHandler)
	}
	return requestBoundary(mux, options.Logger)
}

func respond(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "request_id": w.Header().Get("X-Request-ID")})
}

func requestBoundary(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			http.Error(w, "服务暂不可用", 503)
			return
		}
		requestID := hex.EncodeToString(id[:])
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		defer func() {
			if recover() != nil {
				// Intentionally exclude panic text, URL/query, cookies and headers.
				logger.Error("request failed", "request_id", requestID)
				respond(w, r, 500, map[string]any{"code": "INTERNAL_ERROR", "message": "服务暂不可用"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
