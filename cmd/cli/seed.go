package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/log"
	"github.com/dali/go_clean_arch_sample/internal/seeds"
)

// newSeedCmd applies dev/test seed data. Dev-only because demo records must
// never accidentally land in prod. Idempotent: each seeder catches its
// "already exists" sentinel internally, so reruns are safe.
//
// Args:
//
//	cli seed              # run every registered seeder, in order
//	cli seed <name>       # run just one
//	cli seed --list       # print the registered names
func newSeedCmd(cfg *config.Config) *cobra.Command {
	var (
		list  bool
		reset bool
	)
	cmd := &cobra.Command{
		Use:   "seed [name]",
		Short: "Apply registered seed data (dev only, idempotent)",
		Long: `Without arguments, runs every registered seeder in order.
With a name, runs only that seeder.
Use --list to print the registered names.
Use --reset to truncate the targeted seeders' tables before running (destructive — wipes API-created data too).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				for _, s := range seeds.All() {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), s.Name); err != nil {
						return err
					}
				}
				return nil
			}

			// Resolve target before opening the DB so a typo doesn't burn a connection.
			var targets []seeds.Seeder
			if len(args) == 1 {
				s := seeds.Find(args[0])
				if s == nil {
					return fmt.Errorf("unknown seeder %q (try `cli seed --list`)", args[0])
				}
				targets = []seeds.Seeder{*s}
			} else {
				targets = seeds.All()
			}

			if reset {
				tables := targetTables(targets)
				log.Warn("seed: truncating tables", "tables", tables)
				if err := postgres.TruncateTables(cfg, tables); err != nil {
					return err
				}
			}

			return runSeeders(cmd.Context(), cfg, targets)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "Print registered seeder names and exit")
	cmd.Flags().BoolVar(&reset, "reset", false, "Truncate the targeted seeders' tables before running (destructive)")
	return cmd
}

// runSeeders opens the DB once, builds repositories, and runs the given
// seeders in order. Shared by the `seed` command and `db_setup` (which
// runs all seeders after migrations as part of bringing the DB to a
// usable state). No-op on an empty target list.
func runSeeders(ctx context.Context, cfg *config.Config, targets []seeds.Seeder) error {
	if len(targets) == 0 {
		return nil
	}
	db, err := postgres.New(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	repos := postgres.NewRepositories(db)

	for _, s := range targets {
		n, err := s.Run(ctx, repos)
		if err != nil {
			return fmt.Errorf("seed %s: %w", s.Name, err)
		}
		log.Info("seed: applied", "name", s.Name, "inserted", n)
	}
	return nil
}

// targetTables returns the deduplicated, sorted union of Tables across the
// given seeders. Sorting gives stable log output and a deterministic
// TRUNCATE order.
func targetTables(targets []seeds.Seeder) []string {
	set := map[string]struct{}{}
	for _, s := range targets {
		for _, t := range s.Tables {
			set[t] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
