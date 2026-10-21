package config

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// Config is deliberately not serializable: it contains database credentials.
type Config struct {
	Environment   string
	HTTPAddr      string
	HTTPSAddr     string
	PublicURL     string
	DatabaseDSN   string
	MasterKey     []byte
	AdminEmail    string
	AdminPassword string
	UploadDir     string
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Environment:   value(getenv("LAUNCH_ENV"), "development"),
		HTTPAddr:      value(getenv("LAUNCH_HTTP_ADDR"), "127.0.0.1:8080"),
		HTTPSAddr:     strings.TrimSpace(getenv("LAUNCH_HTTPS_ADDR")),
		PublicURL:     value(getenv("LAUNCH_PUBLIC_URL"), "http://localhost:8080"),
		DatabaseDSN:   getenv("LAUNCH_DATABASE_DSN"),
		AdminEmail:    getenv("LAUNCH_ADMIN_EMAIL"),
		AdminPassword: getenv("LAUNCH_ADMIN_PASSWORD"),
	}
	uploadDir, err := filepath.Abs(value(getenv("LAUNCH_UPLOAD_DIR"), filepath.Join(".local", "uploads")))
	if err != nil {
		return c, errors.New("LAUNCH_UPLOAD_DIR could not be resolved")
	}
	c.UploadDir = uploadDir
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return c, errors.New("LAUNCH_ENV must be development, test or production")
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return c, errors.New("LAUNCH_HTTP_ADDR must be host:port")
	}
	if c.HTTPSAddr != "" {
		if _, _, err := net.SplitHostPort(c.HTTPSAddr); err != nil {
			return c, errors.New("LAUNCH_HTTPS_ADDR must be host:port")
		}
		if c.HTTPSAddr == c.HTTPAddr {
			return c, errors.New("LAUNCH_HTTPS_ADDR must differ from LAUNCH_HTTP_ADDR")
		}
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, errors.New("LAUNCH_PUBLIC_URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if key := getenv("LAUNCH_MASTER_KEY"); key != "" {
		c.MasterKey, err = base64.StdEncoding.Strict().DecodeString(key)
		if err != nil || len(c.MasterKey) != 32 {
			return c, errors.New("LAUNCH_MASTER_KEY must be a base64-encoded 32-byte key")
		}
	}
	if c.Environment == "production" {
		if c.DatabaseDSN == "" || len(c.MasterKey) != 32 || u.Scheme != "https" {
			return c, errors.New("production requires LAUNCH_DATABASE_DSN, LAUNCH_MASTER_KEY and an HTTPS LAUNCH_PUBLIC_URL")
		}
	}
	if c.DatabaseDSN != "" && len(c.MasterKey) != 32 {
		return c, errors.New("LAUNCH_MASTER_KEY is required when the database is configured; generate it with launch keygen")
	}
	return c, nil
}

func value(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
