// Command cli is the operator CLI for ad-hoc and administrative tasks.
//
// Unlike cmd/api (which serves HTTP traffic), this binary is meant to be
// run by an operator from the shell — locally during development, or via
// SSH on a deployed environment. Each subcommand wraps a function in
// internal/usecase/..., so the same domain logic that powers HTTP handlers
// is also reachable from the command line.
//
// Why this exists
// ---------------
// Go has no equivalent of Elixir's `iex --remsh` or Rails' `rails console`.
// You cannot attach a REPL to a running production process and improvise.
// Instead, the Go convention is: anticipate the ops actions you'll need,
// implement them as subcommands here, and ship them with each deploy.
// Over time this binary accumulates a library of operations (list, retry,
// cancel, update, ...) that operators can run safely and auditably.
//
// What this is NOT for
// --------------------
//   - Running raw SQL — use `psql` directly for one-off data fixes.
//   - Inspecting live process state — use `/debug/pprof/goroutine?debug=2`
//     against the running server (added in a later step).
//   - One-off scripts you'll only ever run once — keep those as a personal
//     `go run` invocation or under /scripts; don't pollute this binary.
//
// Adding a new subcommand
// -----------------------
//  1. Implement the operation in internal/usecase/<area>/ as a function
//     taking ctx + typed args, returning typed results + error.
//  2. Add a file here (e.g. cmd/cli/requests.go) that parses flags, wires
//     up dependencies (DB handle, etc.), and calls the use-case function.
//  3. Register it: rootCmd.AddCommand(newRequestsCmd()).
//
// Invocation
// ----------
//
//	go run ./cmd/cli <command> [flags]     # during local development
//	mise run cli -- <command> [flags]      # via mise task
//	./bin/cli <command> [flags]            # from a built binary on a host
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
)

var rootCmd = &cobra.Command{
	Use:   "cli",
	Short: "Operator CLI for go_project_sample",
	Long: `Operator CLI for go_project_sample.

Each subcommand wraps an internal/usecase function so domain logic is
reachable both from HTTP handlers and from this command line. See the
package doc comment in cmd/cli/main.go for the full rationale.`,
}

func main() {
	cfg := config.Load()
	log.Setup(cfg.LogFormat, cfg.LogLevel)

	// migrate / migrate_status are idempotent / read-only — register in all envs.
	rootCmd.AddCommand(newMigrateCmd(cfg))
	rootCmd.AddCommand(newMigrateStatusCmd(cfg))

	// Dev-only commands. Destructive operations, local-bring-up, and source
	// scaffolding must not be reachable in prod/test envs — gate them here
	// so they don't even appear in `cli --help` outside dev.
	if cfg.Env == config.EnvDev {
		rootCmd.AddCommand(newDBSetupCmd(cfg))
		rootCmd.AddCommand(newDBResetCmd(cfg))
		rootCmd.AddCommand(newGenerateMigrationCmd(cfg))
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
