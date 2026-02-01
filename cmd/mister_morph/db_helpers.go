package main

import (
	"github.com/quailyquaily/mister_morph/db"
	"github.com/spf13/viper"
)

func dbConfigFromViper() db.Config {
	cfg := db.DefaultConfig()

	cfg.Driver = viper.GetString("db.driver")
	cfg.DSN = viper.GetString("db.dsn")
	cfg.AutoMigrate = viper.GetBool("db.automigrate")

	cfg.Pool.MaxOpenConns = viper.GetInt("db.pool.max_open_conns")
	cfg.Pool.MaxIdleConns = viper.GetInt("db.pool.max_idle_conns")
	cfg.Pool.ConnMaxLifetime = viper.GetDuration("db.pool.conn_max_lifetime")
	if cfg.Pool.ConnMaxLifetime < 0 {
		cfg.Pool.ConnMaxLifetime = 0
	}

	cfg.SQLite.BusyTimeoutMs = viper.GetInt("db.sqlite.busy_timeout_ms")
	cfg.SQLite.WAL = viper.GetBool("db.sqlite.wal")
	cfg.SQLite.ForeignKeys = viper.GetBool("db.sqlite.foreign_keys")

	// Ensure reasonable defaults even if config has zeros.
	if cfg.Pool.MaxOpenConns <= 0 {
		cfg.Pool.MaxOpenConns = 1
	}
	if cfg.Pool.MaxIdleConns <= 0 {
		cfg.Pool.MaxIdleConns = 1
	}
	if cfg.SQLite.BusyTimeoutMs <= 0 {
		cfg.SQLite.BusyTimeoutMs = 5000
	}

	return cfg
}
