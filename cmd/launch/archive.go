package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"licentivo-launch/internal/backup"
	"licentivo-launch/internal/config"
	"licentivo-launch/internal/storage"
	"log/slog"
	"os"
	"time"
)

func privateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("could not create output file; use a new path in a private directory")
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errors.New("could not write output file")
	}
	if file.Sync() != nil {
		file.Close()
		return errors.New("could not persist output file")
	}
	return file.Close()
}
func runArchive(parent context.Context, command, path string, lookup func(string) string, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	passphrase := lookup("LAUNCH_BACKUP_PASSPHRASE")
	if len(passphrase) < 16 || len(passphrase) > 256 {
		return errors.New("set LAUNCH_BACKUP_PASSPHRASE to a separate 16–256 character passphrase")
	}
	var snap storage.Snapshot
	if command == "restore" {
		file, err := os.Open(path)
		if err != nil {
			return errors.New("could not read backup file")
		}
		sealed, err := io.ReadAll(io.LimitReader(file, backup.MaxSize+1))
		file.Close()
		if err != nil {
			return backup.ErrArchive
		}
		plain, err := backup.Open(sealed, passphrase)
		if err != nil {
			return err
		}
		defer clear(plain)
		if json.Unmarshal(plain, &snap) != nil {
			return backup.ErrArchive
		}
	}
	load := lookup
	if command == "restore" {
		load = func(key string) string {
			if key == "LAUNCH_MASTER_KEY" && lookup(key) == "" {
				return base64.StdEncoding.EncodeToString(snap.MasterKey)
			}
			return lookup(key)
		}
	}
	cfg, err := config.Load(load)
	if err != nil {
		return err
	}
	if cfg.DatabaseDSN == "" {
		return errors.New("backup and restore require LAUNCH_DATABASE_DSN")
	}
	if command == "restore" && !bytes.Equal(cfg.MasterKey, snap.MasterKey) {
		return errors.New("configured master key differs from the backup; use the original key or an empty LAUNCH_MASTER_KEY for recovery")
	}
	db, err := storage.Open(cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	defer storage.Close(db)
	if command == "backup" {
		configuration := map[string]string{}
		for _, key := range []string{"LAUNCH_ENV", "LAUNCH_HTTP_ADDR", "LAUNCH_HTTPS_ADDR", "LAUNCH_PUBLIC_URL", "LAUNCH_DATABASE_DSN", "LAUNCH_UPLOAD_DIR", "LAUNCH_ADMIN_EMAIL", "LAUNCH_ADMIN_PASSWORD"} {
			configuration[key] = lookup(key)
		}
		configuration["LAUNCH_MASTER_KEY"] = base64.StdEncoding.EncodeToString(cfg.MasterKey)
		snap, err = storage.MakeSnapshot(ctx, db, cfg.MasterKey, configuration)
		if err != nil {
			return errors.New("could not read a consistent backup snapshot")
		}
		plain, err := json.Marshal(snap)
		if err != nil {
			return backup.ErrArchive
		}
		defer clear(plain)
		sealed, err := backup.Seal(plain, passphrase)
		if err != nil {
			return err
		}
		if err := privateFile(path, sealed); err != nil {
			return err
		}
		checked := time.Now().UTC().Truncate(time.Microsecond)
		_ = db.WithContext(ctx).Model(&storage.RuntimeStatus{}).Where("id = ?", 1).Updates(map[string]any{"last_backup_at": checked, "updated_at": checked}).Error
		logger.Info("encrypted backup created", "tables", len(snap.Tables), "created_at", snap.CreatedAt)
		return nil
	}
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		return errors.New("restore requires a new database with no tables; existing schema was preserved")
	}
	if err := storage.Initialize(ctx, db); err != nil {
		return err
	}
	recovered := snap.Configuration
	if recovered == nil {
		recovered = map[string]string{}
	}
	recovered["LAUNCH_MASTER_KEY"] = base64.StdEncoding.EncodeToString(snap.MasterKey)
	recovered["LAUNCH_DATABASE_DSN"] = cfg.DatabaseDSN
	raw, err := config.MarshalStartup(recovered)
	if err != nil {
		return backup.ErrArchive
	}
	defer clear(raw)
	// Preserve recovery material even if a subsequent database operation fails.
	if err := privateFile(path+".secrets.yaml", raw); err != nil {
		return err
	}
	if err := storage.RestoreSnapshot(ctx, db, snap); err != nil {
		return err
	}
	logger.Info("backup restored; review transactions after the backup timestamp before resuming commerce", "backup_created_at", snap.CreatedAt, "configuration_file", path+".secrets.yaml")
	return nil
}
