# go_clean_arch_sample

A small, deliberately-scoped Go service demonstrating a production-shaped
backend: clean architecture with dependencies pointing inward, a Gin HTTP API,
an embedded [asynq](https://github.com/hibiken/asynq) (Redis) worker, Postgres
via GORM, and a two-tier test strategy (fakes for units, real containers for
integration).

It implements a single feature — user create / get / list, with a welcome-email
task enqueued on creation — and stops there on purpose, so the focus stays on
the **structure and the design decisions** rather than feature count.

## What it demonstrates

- **Dependencies point inward.** `domain` → `usecase` → `adapter` → `cmd`. The
  inner layers never import the outer ones.
- **Interfaces are consumer-defined and minimal.** There are exactly two
  (`UserRepository`, `WelcomeEmailEnqueuer`), both declared in `usecase` where
  they're *used*, implemented by the adapters. No interface exists purely to
  invert a dependency that didn't need inverting — concrete types (the queue
  `Server`, `DrainQueue`) stay concrete.
- **A real test strategy, not just "has tests."** Unit tests fake the two
  boundaries and run with no infrastructure; integration tests spin up real
  throwaway Postgres + Redis containers via dockertest and exercise the actual
  adapters end-to-end.
- **Composition roots that don't rot.** `main.go` builds the dependency bundle
  once; each feature wires itself in its own `router/<feature>.go`, so `main`
  doesn't grow a limb per feature.
- **Production-shaped lifecycle.** Graceful shutdown drains HTTP first, then the
  worker; fast-fail on startup errors; structured logging; env-specific config.

## Layout

```
cmd/
  api/                 HTTP server + embedded worker (the deployable)
  cli/                 operator CLI (migrate, seed, db setup/reset) — cobra
internal/
  domain/              entities + invariants (User, validation). No deps.
  usecase/             application logic; defines the interfaces it needs
    testfakes/         shared fakes for the two boundaries
  adapter/
    http/              gin: api, router (per-feature), handler, middleware, response
    repository/postgres/  GORM impl of UserRepository, migrations
  queue/               asynq: client (producer), server (consumer), DrainQueue
  config/              env-specific config (dev / prod / test)
  log/                 slog wrapper
  testsupport/         dockertest harness (RequirePool, StartPostgres/Redis…)
test/
  integration/         full-stack tests (real HTTP → Postgres → Redis)
```

## Design decisions

- **The worker is embedded in the API process — there is no standalone worker
  binary.** This service never consumes tasks without also producing them, so a
  separate deployable would be ceremony. `cmd/api` starts the asynq server in
  the background and drains it during shutdown.
- **Two consumer-side interfaces, and that's it.** "Clean architecture" samples
  tend to drown in one-implementation interfaces. Here a boundary becomes an
  interface only when something on the *use case* side genuinely needs to swap
  the implementation (a fake in tests, a different adapter later).
- **The welcome-email handler is an intentional stub.** It logs what it *would*
  send rather than wiring a real SMTP/provider. That keeps the queue seam fully
  exercised (enqueue → drain → handler runs) without a credential dependency the
  sample would only have to mock anyway.
- **Synchronous queue draining in tests, à la `Oban.drain_queue`.** Integration
  tests call `queue.DrainQueue(...)`, which runs the handler inline and returns
  a processed count — no background server, no `Eventually`-polling flake.
- **`mise run test` fails loudly without Docker.** Container tests are not
  silently skipped in the full run; only `mise run test:short` skips them. A
  green run means the real adapters actually ran.

For the deeper rationale (wiring aggregates, why use cases never receive
`Deps`, the CLI's reason to exist) see `ARCHITECTURE.md` and `AGENTS.md`.

## Running it

Prereqs: [mise](https://mise.jdx.dev) (pins Go 1.25 + tooling) and Docker.

```bash
cp .env.example .env        # or use the committed .env (APP_ENV=dev, HTTP_PORT=8081)
mise install                # Go toolchain + golangci-lint + delve
mise run infra              # Postgres + Redis via docker compose
mise run cli -- db_setup    # create DB + migrate + seed demo data
mise run server             # API + embedded worker on :8081
```

`db_setup` is the first-run convenience (create DB → migrate → seed), the
`rails db:setup` analog. On later runs you usually just need `mise run cli --
migrate` to apply new migrations. `mise run infra:dev` also brings up the
asynqmon queue UI on <http://localhost:8082>.

### Try the API

The port is `8081` (from `.env`). Every response is a uniform envelope:
`{"success": true, ...}` or `{"success": false, "error": "<code>", "details": [...]}`.

```bash
# health
curl -s localhost:8081/health
# {"status":"ok","success":true}

# create a user -> 201
curl -s -X POST localhost:8081/users \
  -H 'content-type: application/json' \
  -d '{"email":"alice@example.com","name":"Alice"}'
# {"success":true,"user":{"id":"…","email":"alice@example.com","name":"Alice",…}}
# (also enqueues a welcome-email task on the "emails" queue)

# list users -> 200
curl -s localhost:8081/users
# {"success":true,"users":[{…}]}

# get one -> 200 / 404
curl -s localhost:8081/users/<id>

# duplicate email -> 409 {"success":false,"error":"email_taken"}
# invalid body   -> 400 {"success":false,"error":"invalid_body","details":[…]}
```

## Operator CLI (`cmd/cli`)

A cobra CLI that wraps the same use-case functions the HTTP layer calls. It's
the Go stand-in for `rails console` / `iex --remsh`: Go can't attach a REPL to
a running process, so operational actions are anticipated and shipped as
subcommands. Run it via `mise run cli -- <command>` (dev), or `./bin/cli
<command>` from a built binary on a host.

**Always available** (safe in every environment):

| Command | Description |
|---|---|
| `migrate` | Apply pending migrations. Idempotent and additive — this is the prod deploy step. |
| `migrate_status` | Print each migration as `APPLIED` / `PENDING` / `ORPHAN`. Read-only. |

**Dev-only** (gated on `APP_ENV=dev`; they don't even appear in `--help`
elsewhere, because destructive ops, local bring-up, and source scaffolding must
never be reachable in prod/test):

| Command | Description |
|---|---|
| `db_setup` | Create database → migrate → seed. The `rails db:setup` analog; idempotent. |
| `db_reset` | Drop database → `db_setup`. Destructive. |
| `seed [name]` | Run all seeders, or one by name. `--list` to list them, `--reset` to truncate first. Idempotent (each seeder skips its own duplicates). |
| `generate_migration <name>` | Scaffold `migrations/<timestamp>_<name>.go` with `Migrate`/`Rollback` stubs that fail loudly until you implement them. Name must be lowercase snake_case. |

Migrations live one-per-file under
`internal/adapter/repository/postgres/migrations`; each registers itself in an
`init()`, so there's no central manifest — the timestamp-prefixed filename is
the ordering. Seeders follow the same self-registering pattern under
`internal/seeds` and write *through* the use-case layer, never raw SQL.

## Testing

```bash
mise run test         # everything, -race, incl. dockertest integration (needs Docker)
mise run test:short   # unit tests only (-short skips containers, no Docker needed)
mise run check        # pre-push gate: lint + short tests
```

Bypass Go's test cache with `-count=1`, e.g.
`mise exec -- go test ./... -race -count=1`, or `go clean -testcache` first.

- **Unit** (`*_test.go` beside the code): fake `UserRepository` /
  `WelcomeEmailEnqueuer` from `internal/usecase/testfakes`. No infra.
- **Integration** (`test/integration`, plus the postgres repo test): real
  ephemeral Postgres + Redis containers (random ports, auto-removed) — fully
  separate from the dev compose instance.

## Starting from this as a template

The `user` feature is the worked example of the architecture — once you've read
how it's wired, you can delete it and build your own features in the same shape.

To stamp out a brand-new module with its own import path (the Go-native
`create-react-app`), use [`gonew`](https://pkg.go.dev/golang.org/x/tools/cmd/gonew):

```bash
go install golang.org/x/tools/cmd/gonew@latest
gonew github.com/dali/go_clean_arch_sample@latest github.com/you/myapp ./myapp
```

That copies the tree and rewrites the module path in `go.mod` and every import.
A few non-import references (`.env` `DB_NAME`, the CLI `Short:` string, this
README's title) still mention the old name — find-and-replace those by hand.

### Removing the example `user` feature

Delete the user-specific files:

```bash
rm internal/domain/user.go internal/domain/user_test.go
rm internal/usecase/user.go internal/usecase/user_test.go
rm internal/usecase/testfakes/testfakes.go
rm internal/adapter/http/handler/users.go internal/adapter/http/handler/users_test.go
rm internal/adapter/http/router/users.go
rm internal/adapter/repository/postgres/user_repository.go
rm internal/adapter/repository/postgres/user_model.go
rm internal/adapter/repository/postgres/user_repository_test.go
rm internal/queue/welcome_email.go
rm internal/seeds/users.go
rm internal/adapter/repository/postgres/migrations/*_create_users.go
rm test/integration/create_user_test.go
```

Then drop the references to them — each is a one-line edit:

| File | Edit |
|---|---|
| `internal/adapter/http/router/router.go` | remove the `RegisterUsers(engine, deps)` line |
| `internal/usecase/repositories.go` | empty the `Repositories` struct (drop `Users`) |
| `internal/usecase/producers.go` | empty the `Producers` struct (drop `WelcomeEmail`) |
| `internal/adapter/repository/postgres/repositories.go` | drop `Users: NewUserRepository(db)` |
| `internal/queue/client.go` | drop `WelcomeEmail: c` in `NewProducers` |
| `internal/queue/server.go` | remove the `mux.HandleFunc(TypeWelcomeEmail, …)` line |

`Repositories` and `Producers` are deliberately just aggregates, so leaving them
empty is fine — they grow one field per entity/producer as you add features.

Verify you're back to a clean slate:

```bash
go build ./...
mise run check          # lint + short tests
```

Now scaffold your first feature. Generate a migration, then mirror how `user`
was wired — a domain entity, its use case + the interface(s) it needs, a
postgres repository, an HTTP handler, and a `router/<feature>.go` that mounts
it:

```bash
mise run cli -- generate_migration create_widgets
```

(gormigrate needs at least one migration, so add yours before running
`db_setup` / `migrate`.)

## Stack

Go 1.25 · Gin · GORM + pgx (Postgres) · asynq (Redis) · gormigrate · cobra ·
slog · testify · dockertest · golangci-lint v2 · mise.
