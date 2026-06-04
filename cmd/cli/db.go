package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/log"
	"github.com/dali/go_clean_arch_sample/internal/seeds"
)

// newDBSetupCmd brings the database to a usable state: create the database if
// it doesn't exist, run migrations, then apply seeds (see runDBSetup).
// Dev-only — registered conditionally from main() based on cfg.Env.
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
	if err := postgres.Migrate(cfg); err != nil {
		return err
	}
	// Apply every registered seeder so a fresh DB lands in a usable demo
	// state — matches the Rails `db:setup` / Phoenix `ecto.setup` convention
	// ("create + migrate + seed"). Seeders are idempotent, so this is also
	// safe when run on an already-seeded DB (e.g. via db_reset).
	if err := runSeeders(context.Background(), cfg, seeds.All()); err != nil {
		return err
	}
	log.Info("db_setup: database ready", "db", cfg.DBName)
	return nil
}

func runDBDrop(cfg *config.Config) error {
	log.Warn("db_reset: dropping database", "db", cfg.DBName)
	return postgres.DropDatabase(cfg)
}
