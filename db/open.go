package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func Open(ctx context.Context, cfg Config) (*gorm.DB, error) {
	_ = ctx
	if strings.TrimSpace(cfg.Driver) == "" {
		cfg.Driver = "sqlite"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "sqlite":
		dsn, err := ResolveSQLiteDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, err
		}
		if err := applySQLitePragmas(gdb, cfg.SQLite); err != nil {
			return nil, err
		}

		sqlDB, err := gdb.DB()
		if err != nil {
			return nil, err
		}
		if cfg.Pool.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(cfg.Pool.MaxOpenConns)
		}
		if cfg.Pool.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.Pool.MaxIdleConns)
		}
		if cfg.Pool.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(cfg.Pool.ConnMaxLifetime)
		}
		return gdb, nil
	default:
		return nil, fmt.Errorf("unsupported db.driver: %s (only sqlite is implemented in Phase 1)", cfg.Driver)
	}
}
