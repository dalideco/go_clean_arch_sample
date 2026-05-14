package postgres

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/config"
)

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

// AutoMigrate applies the current persistence-model schema to cfg.DBName
// using gorm.AutoMigrate. This is a *temporary* schema bring-up: it exists
// so dev can run `cli db_setup` and get a working schema before Step 4
// (golang-migrate) lands. Once real migrations exist, this function and
// its caller in cmd/cli/db.go are removed.
//
// Add new persistence models to the list below as features are introduced.
// NOT to be called from cmd/api — schema state must not be mutated at app
// boot time.
func AutoMigrate(cfg *config.Config) error {
	db, err := New(cfg)
	if err != nil {
		return err
	}
	defer closeAdmin(db)

	if err := db.AutoMigrate(&userModel{}); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
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
