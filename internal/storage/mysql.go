package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrNotConfigured = errors.New("database is not configured")
	ErrUnavailable   = errors.New("database is unavailable")
	ErrSchema        = errors.New("database tables are not initialized")
)

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, ErrNotConfigured
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName == "" {
		// Driver parse errors can contain supplied configuration; do not expose.
		return nil, errors.New("invalid LAUNCH_DATABASE_DSN or missing database name")
	}
	cfg.ParseTime, cfg.Loc, cfg.MultiStatements = true, time.UTC, false
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, errors.New("invalid MySQL connection configuration")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(3 * time.Minute)
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger:               logger.Default.LogMode(logger.Silent),
		DisableAutomaticPing: true,
	})
	if err != nil {
		db.Close()
		return nil, ErrUnavailable
	}
	return orm, nil
}

func Ready(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrNotConfigured
	}
	pool, err := db.DB()
	if err != nil {
		return ErrUnavailable
	}
	if err := pool.PingContext(ctx); err != nil {
		return ErrUnavailable
	}
	for _, model := range models() {
		if !db.WithContext(ctx).Migrator().HasTable(model) {
			return ErrSchema
		}
	}
	return nil
}

// Initialize creates the new application's tables directly from Go models.
// There are no separate SQL files or manual migration commands.
func Initialize(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrNotConfigured
	}
	pool, err := db.DB()
	if err != nil {
		return ErrUnavailable
	}
	if err := pool.PingContext(ctx); err != nil {
		return ErrUnavailable
	}
	if err := db.WithContext(ctx).Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin").AutoMigrate(models()...); err != nil {
		return ErrSchema
	}
	return nil
}

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	pool, err := db.DB()
	if err != nil {
		return ErrUnavailable
	}
	return pool.Close()
}
