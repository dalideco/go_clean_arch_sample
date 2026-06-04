// Package seeds holds dev/test data population steps. Each entity gets one
// file whose init() registers a named *Seeder*; there is no central manifest
// to maintain. The CLI command (cmd/cli/seed.go) calls All()/Find() to drive
// either a full run or a targeted one.
//
// Seeders write **through the use-case layer**, never bypassing into raw
// GORM — they exercise the same domain factory + repository chain as
// production traffic. Idempotency comes from each entity's natural-key
// "taken" sentinel (usecase.ErrUserEmailTaken etc.), which the seeder
// catches and skips. No tracking table.
package seeds

import (
	"context"
	"fmt"

	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// Seeder is a named, idempotent population step. Run reports how many
// records it actually inserted (skipped duplicates don't count); errors
// abort the run. Tables names the tables the seeder owns — used by
// `cli seed --reset` to truncate before re-running.
type Seeder struct {
	Name   string
	Tables []string
	Run    func(ctx context.Context, repos usecase.Repositories) (inserted int, err error)
}

var registered []Seeder

// register is called from each seeder file's init(). Panics on duplicate
// name so collisions surface at program startup, not at first use.
func register(s Seeder) {
	for _, existing := range registered {
		if existing.Name == s.Name {
			panic(fmt.Sprintf("seeds: duplicate registration %q", s.Name))
		}
	}
	registered = append(registered, s)
}

// All returns the registered seeders in registration order (a copy).
func All() []Seeder {
	out := make([]Seeder, len(registered))
	copy(out, registered)
	return out
}

// Find returns the seeder with this exact name, or nil if not registered.
func Find(name string) *Seeder {
	for i := range registered {
		if registered[i].Name == name {
			return &registered[i]
		}
	}
	return nil
}
