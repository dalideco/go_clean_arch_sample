package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dali/go_project_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
)

// newMigrateCmd exposes the migration runner as a CLI command. Unlike
// db_setup / db_reset, this is registered in *all* environments — applying
// pending migrations is the prod-safe deploy step (idempotent, additive).
func newMigrateCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations (safe in all envs)",
		RunE: func(*cobra.Command, []string) error {
			if err := postgres.Migrate(cfg); err != nil {
				return err
			}
			log.Info("migrate: done", "db", cfg.DBName)
			return nil
		},
	}
}

// newMigrateStatusCmd prints each registered migration with its applied /
// pending / orphan state. Read-only — registered in all envs (useful for
// staging/prod inspection).
func newMigrateStatusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate_status",
		Short: "Show registered migrations and which have been applied",
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses, err := postgres.Statuses(cfg)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, s := range statuses {
				state := "PENDING"
				switch {
				case s.Orphan:
					state = "ORPHAN"
				case s.Applied:
					state = "APPLIED"
				}
				fmt.Fprintf(out, "%-7s  %s\n", state, s.ID)
			}
			return nil
		},
	}
}
