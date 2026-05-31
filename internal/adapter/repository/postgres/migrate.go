package postgres

import (
	"fmt"
	"sort"

	"github.com/go-gormigrate/gormigrate/v2"

	"github.com/dali/go_project_sample/internal/adapter/repository/postgres/migrations"
	"github.com/dali/go_project_sample/internal/config"
)

// Migrate applies any pending migrations to cfg.DBName. Idempotent — already-
// applied migrations are tracked in the "migrations" table and skipped on
// re-run, so this is safe to call on every deploy or local boot.
func Migrate(cfg *config.Config) error {
	db, err := New(cfg)
	if err != nil {
		return err
	}
	defer closeAdmin(db)

	m := gormigrate.New(db, gormigrate.DefaultOptions, migrations.All())
	if err := m.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// MigrationStatus is one row of the status report: a migration ID with
// whether it's been applied. Orphan=true means the row exists in the
// tracking table but no migration with that ID is registered in the code
// anymore (someone deleted the file).
type MigrationStatus struct {
	ID      string
	Applied bool
	Orphan  bool
}

// Statuses cross-references the migrations registered in code with the
// tracking table to report applied / pending / orphaned state. Registered
// migrations come first in chronological order; orphans are appended last,
// sorted by ID.
func Statuses(cfg *config.Config) ([]MigrationStatus, error) {
	db, err := New(cfg)
	if err != nil {
		return nil, err
	}
	defer closeAdmin(db)

	table := gormigrate.DefaultOptions.TableName
	applied := map[string]bool{}
	if db.Migrator().HasTable(table) {
		var ids []string
		if err := db.Table(table).Select("id").Scan(&ids).Error; err != nil {
			return nil, fmt.Errorf("read migration tracking table: %w", err)
		}
		for _, id := range ids {
			applied[id] = true
		}
	}

	registered := map[string]bool{}
	out := []MigrationStatus{}
	for _, m := range migrations.All() {
		registered[m.ID] = true
		out = append(out, MigrationStatus{ID: m.ID, Applied: applied[m.ID]})
	}
	var orphans []string
	for id := range applied {
		if !registered[id] {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	for _, id := range orphans {
		out = append(out, MigrationStatus{ID: id, Applied: true, Orphan: true})
	}
	return out, nil
}
