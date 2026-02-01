package db

import (
	"fmt"

	"gorm.io/gorm"
)

func applySQLitePragmas(gdb *gorm.DB, cfg SQLiteConfig) error {
	if gdb == nil {
		return fmt.Errorf("nil gorm db")
	}
	if cfg.WAL {
		if err := gdb.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
			return err
		}
	}
	if cfg.BusyTimeoutMs > 0 {
		if err := gdb.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d;", cfg.BusyTimeoutMs)).Error; err != nil {
			return err
		}
	}
	if cfg.ForeignKeys {
		if err := gdb.Exec("PRAGMA foreign_keys=ON;").Error; err != nil {
			return err
		}
	}
	return nil
}
