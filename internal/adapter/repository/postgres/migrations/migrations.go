// Package migrations is the chronologically-ordered list of schema changes
// applied by gormigrate. Each change lives in its own file as a registration
// call in an init() — there is no central manifest to maintain; the set of
// migrations is exactly the files compiled into this package.
//
// Adding a migration:
//  1. Create <YYYYMMDDHHMMSS>_<desc>.go (timestamp via `date +%Y%m%d%H%M%S`).
//     The timestamp prefix avoids the 9999-cap of sequential numbering and
//     keeps parallel branches conflict-free.
//  2. Inside, an init() that calls register(&gormigrate.Migration{...})
//     with the matching "<YYYYMMDDHHMMSS>_<desc>" ID and Migrate/Rollback
//     funcs. No need to touch this file.
//
// Each migration must be self-contained: declare any structs it needs
// locally ("frozen" snapshots) rather than referencing live persistence
// models. Live models drift; a migration's behaviour on a fresh DB must not.
package migrations

import (
	"sort"

	"github.com/go-gormigrate/gormigrate/v2"
)

var registered []*gormigrate.Migration

// register adds a migration to the package-level set. Called from each
// migration file's init(). Not safe to call concurrently (init is sequential).
func register(m *gormigrate.Migration) {
	registered = append(registered, m)
}

// All returns the registered migrations sorted by ID. Sorting at access
// time means the chronological order is guaranteed by the timestamp-prefixed
// IDs, not by Go's filename-lexical init-order convention (which is reliable
// in gc but not strictly mandated by the spec).
func All() []*gormigrate.Migration {
	out := make([]*gormigrate.Migration, len(registered))
	copy(out, registered)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
