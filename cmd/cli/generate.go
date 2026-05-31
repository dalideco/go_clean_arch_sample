package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
)

// migrationsDir is the path (relative to the repo root) where new migration
// files are written. The generate command assumes invocation from the repo
// root (mise run cli -- ... satisfies that).
const migrationsDir = "internal/adapter/repository/postgres/migrations"

var migrationNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// newGenerateMigrationCmd scaffolds a new migration file with the proper
// <YYYYMMDDHHMMSS>_<name>.go filename + matching ID + Migrate/Rollback
// stubs that fail loudly until the dev fills them in. Dev-only — writing
// source files only makes sense at dev time.
func newGenerateMigrationCmd(_ *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "generate_migration <name>",
		Short: "Scaffold a new migration file with a timestamp prefix (dev only)",
		Long: `Create internal/adapter/repository/postgres/migrations/<YYYYMMDDHHMMSS>_<name>.go
with an init() that registers a *gormigrate.Migration whose Migrate and
Rollback stubs return errors.New("not implemented: ...") so an accidental
` + "`cli migrate`" + ` before you fill them in fails loudly.

Name must match ` + migrationNameRE.String() + ` (lowercase snake_case).`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runGenerateMigration(args[0])
		},
	}
}

func runGenerateMigration(name string) error {
	if !migrationNameRE.MatchString(name) || len(name) > 60 {
		return fmt.Errorf("invalid migration name %q (lowercase snake_case, ≤60 chars)", name)
	}
	if _, err := os.Stat(migrationsDir); err != nil {
		return fmt.Errorf("migrations dir not found at %q — run from the repo root: %w", migrationsDir, err)
	}

	id := time.Now().Format("20060102150405") + "_" + name
	path := filepath.Join(migrationsDir, id+".go")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s", path)
	}

	if err := os.WriteFile(path, []byte(scaffoldMigration(id)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("generated migration", "path", path, "id", id)
	return nil
}

func scaffoldMigration(id string) string {
	const tmpl = `package migrations

import (
	"errors"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// TODO: implement. If this migration touches an entity, declare a local
// frozen-struct snapshot in here — do NOT import the live persistence
// model (see AGENTS.md on the frozen-struct rule).

func init() {
	register(&gormigrate.Migration{
		ID: %q,
		Migrate: func(tx *gorm.DB) error {
			return errors.New("not implemented: " + %q)
		},
		Rollback: func(tx *gorm.DB) error {
			return errors.New("not implemented: " + %q)
		},
	})
}
`
	return fmt.Sprintf(tmpl, id, id, id)
}
