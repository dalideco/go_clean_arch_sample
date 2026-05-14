package main

import (
	"github.com/spf13/cobra"

	"github.com/dali/go_project_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
)

// newDBSetupCmd brings the database to a usable state. Today that's just
// "create the database if it doesn't exist"; migrations (Step 4) and seed
// (Step 5) will be added here when they land. Dev-only — registered
// conditionally from main() based on cfg.Env.
func newDBSetupCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "db_setup",
		Short: "Create database (and later: run migrations, apply seed) — dev only",
		RunE: func(*cobra.Command, []string) error {
			return runDBSetup(cfg)
		},
	}
}

// newDBResetCmd drops the database, then runs db_setup. Destructive —
// dev-only by gating in main(); we don't want this anywhere near staging
// or prod data.
func newDBResetCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "db_reset",
		Short: "Drop database, then re-run db_setup (dev only)",
		RunE: func(*cobra.Command, []string) error {
			if err := runDBDrop(cfg); err != nil {
				return err
			}
			return runDBSetup(cfg)
		},
	}
}

func runDBSetup(cfg *config.Config) error {
	if err := postgres.CreateDatabase(cfg); err != nil {
		return err
	}
	if err := postgres.AutoMigrate(cfg); err != nil {
		return err
	}
	log.Info("db_setup: database ready", "db", cfg.DBName)
	return nil
}

func runDBDrop(cfg *config.Config) error {
	log.Warn("db_reset: dropping database", "db", cfg.DBName)
	return postgres.DropDatabase(cfg)
}
