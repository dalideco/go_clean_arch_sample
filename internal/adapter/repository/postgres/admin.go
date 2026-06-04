package postgres

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/dali/go_clean_arch_sample/internal/config"
)

// TruncateTables truncates the given tables in cfg.DBName with
// RESTART IDENTITY CASCADE. Empty input is a no-op. Destructive — intended
// for dev seed reset (`cli seed --reset`), not for any prod path.
func TruncateTables(cfg *config.Config, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	db, err := New(cfg)
	if err != nil {
		return err
	}
	defer closeAdmin(db)

	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = quoteIdent(t)
	}
	stmt := "TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("truncate %v: %w", tables, err)
	}
	return nil
}

// DropDatabase drops cfg.DBName if it exists. Connects to the "postgres"
// maintenance database because you can't drop the database you're currently
// connected to.
func DropDatabase(cfg *config.Config) error {
	db, err := openAdmin(cfg)
	if err != nil {
		return err
	}
	defer closeAdmin(db)

	if err := db.Exec("DROP DATABASE IF EXISTS " + quoteIdent(cfg.DBName)).Error; err != nil {
		return fmt.Errorf("drop database %s: %w", cfg.DBName, err)
	}
	return nil
}

// CreateDatabase creates cfg.DBName if it doesn't already exist. Safe to call
// repeatedly. Connects to the "postgres" maintenance database.
func CreateDatabase(cfg *config.Config) error {
	db, err := openAdmin(cfg)
	if err != nil {
		return err
	}
	defer closeAdmin(db)

	var exists bool
	if err := db.Raw(
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = ?)",
		cfg.DBName,
	).Scan(&exists).Error; err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if exists {
		return nil
	}

	if err := db.Exec("CREATE DATABASE " + quoteIdent(cfg.DBName)).Error; err != nil {
		return fmt.Errorf("create database %s: %w", cfg.DBName, err)
	}
	return nil
}

func openAdmin(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=postgres sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBSSLMode,
	)
	return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: newGormLogger()})
}

func closeAdmin(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// quoteIdent quotes a Postgres identifier (db/table/column name) for safe
// interpolation into SQL where parameter binding isn't supported (e.g.
// CREATE DATABASE). Doubles embedded double quotes per the SQL standard.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
