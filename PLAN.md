# Go Project Scaffold — Step-by-Step Build Plan

## Context
Greenfield project at `/Users/dali/Documents/GitHub/projects/go_project_sample`. Goal: build up a Gin API + Postgres + Asynq worker stack incrementally — each step adds one capability, installs only the libraries it needs, and ends in a runnable, verifiable state. By the time you finish, you'll have a `users` resource where `POST /users` writes to Postgres and enqueues an async "welcome email" job that the worker consumes from Redis.

Module path: `github.com/dali/go_project_sample`.

Stack picks: **Gin** for HTTP, **GORM** for the ORM, **gormigrate** for versioned migrations (GORM-native), **Asynq** for the worker.

---

## Layout (clean-arch / hexagonal style)

```
cmd/
  api/main.go              # HTTP server entrypoint (Step 1)
  cli/main.go              # Operator CLI for ad-hoc / admin tasks (Step 2.5)
  worker/main.go           # Asynq worker entrypoint (Step 6)
  seed/main.go             # Seed runner (Step 5)
internal/
  domain/                  # entities (innermost layer; no project-internal deps)
  usecase/                 # use cases / application business rules
  tasks/                   # shared task payload types + name constants
                           # imported by both adapter/queue/ and adapter/worker/
  config/                  # env loading
  log/                     # slog facade (already implemented)
  adapter/
    http/
      api/                 # gin engine assembly
      router/              # path → handler binding
      middleware/          # cross-cutting HTTP concerns
      handler/             # HTTP controllers
    repository/            # persistence adapters
      postgres/            # GORM impl
    queue/                 # asynq client (enqueuer)
    worker/                # asynq server + task handlers
```

**Dependency rule:** `cmd → adapter → usecase → domain`. Domain imports nothing project-internal. `adapter/queue/` and `adapter/worker/` both depend on `internal/tasks/` for shared payload types — `tasks/` is a contract, not an adapter. `log/` and `config/` are cross-cutting and used by everyone.

## Conventions in force
- **Error responses** use snake_case codes: `{"error": "<code>"}` (e.g. `not_found`, `internal_error`). Set in `internal/adapter/http/middleware/{not_found,recovery}.go`; applies to all handlers.
- **Request log path** records `c.Request.URL.Path` only — query strings are excluded so secrets accidentally passed via `?token=…` don't end up in logs.
- **Logging** goes through `internal/log` only; nothing else imports `log/slog`. Source-line attribution preserved via a `runtime.Callers`-based wrapper. Tint provides colored TTY output (env-driven JSON handler is a Step 2 follow-up).
- **Graceful shutdown** is implemented in `cmd/api/main.go` using `http.Server` + `signal.Notify` on SIGINT/SIGTERM with a 10s drain.

---

## Step 0 — Project Init
**Goal:** Empty Go module with a `.gitignore` and `README`.

**Commands**
```bash
cd /Users/dali/Documents/GitHub/projects/go_project_sample
git init
go mod init github.com/dali/go_project_sample
```

**Files**
- `.gitignore` — `bin/`, `.env`, `*.log`, `tmp/`, `.DS_Store`
- `README.md` — one-line description; you'll fill in the quick-start later

**Verify:** `go mod tidy` runs without error; `go.mod` exists with the right module path.

---

## Step 1 — Gin API + Logging + Middleware Scaffold ✅ DONE
**Goal:** A Gin server you can `curl` returns 200 on `/health`, with structured logging, request logging middleware, panic recovery, custom 404, and graceful shutdown.

**Implemented:**
- `cmd/api/main.go` — graceful shutdown via `http.Server` + `signal.Notify` (10s drain). `startServer` and `notifyShutdown` helpers return channels; `main` selects on them. Gin global writers redirected to `log.Writer(...)`.
- `internal/log/log.go` — slog wrapper with tint colored output. `Debug/Info/Warn/Error/Fatal/Writer`. Source-line attribution via `runtime.Callers`-based `logAt` helper. `AddSource: true`. Re-exports `Level`/`LevelDebug` etc. so callers don't import `log/slog`.
- `internal/adapter/http/api/api.go` — `New() *gin.Engine` wires middleware + routes.
- `internal/adapter/http/router/router.go` — `Register(engine)` — route binding only.
- `internal/adapter/http/handler/health.go` — `Health` handler.
- `internal/adapter/http/middleware/{request_logger,recovery,not_found}.go` — status-aware request logger, JSON-returning panic recovery, JSON 404.

**Verify**
```bash
go run ./cmd/api
curl -s localhost:8080/health    # → {"status":"ok"}
curl -s localhost:8080/missing   # → {"error":"not_found"}
# Ctrl-C — should see "shutdown signal received, draining" then "server stopped"
```

---

## Step 2 — Config Loading from `.env` ✅ DONE
**Goal:** Centralized, env-aware config. Three profiles (dev / prod / test) with prod-safe defaults in `baseConfig()`; each profile overrides only what differs. Moves `port` and `shutdownTimeout` out of `cmd/api/main.go` constants. Switches gin into release mode for prod (silences `[GIN-debug]` chatter and the debug-mode warning). Wires log format/level through env vars.

**Implemented:**
- `internal/config/config.go` — `Env` int-iota enum (`EnvDev`/`EnvProd`/`EnvTest`) with `String()` and `parseEnv` (invalid `APP_ENV` fails loudly via `log.Fatal`). `Config` struct, `Load()` dispatcher, `baseConfig()` with prod-safe defaults, `IsProd()`. Single helper: `getenv`. *(`mustGetenv` will be added with Step 3 when `DB_HOST` requires it.)*
- `internal/config/dev.go` — hardcoded for dev: tint format, debug level, 10s drain.
- `internal/config/prod.go` — calls `gin.SetMode(gin.ReleaseMode)`, sets `Env=EnvProd`. Base (json/info/30s) is already prod-safe.
- `internal/config/test.go` — 5s drain, warn level. Random port via `HTTP_PORT=0`. Stub for when tests land.
- **Env-var policy:** only things that vary at deploy time (or are secrets) come from env. `HTTP_PORT` is env-driven; `HTTP_SHUTDOWN_TIMEOUT`, `LOG_FORMAT`, `LOG_LEVEL` are hardcoded in the per-profile files because they're decided per environment, not per deployment.
- `.env.example` — committed; just `APP_ENV` and `HTTP_PORT`. (DB/Redis vars get appended in Steps 3 / 6.)
- `internal/log/log.go` — `init()` calls `Setup("tint", "debug")` for pre-Setup defaults; `Setup(format, level)` switches handler (`json` → `slog.NewJSONHandler`, anything else → `tint`). `parseLevel` maps strings to slog levels.
- `cmd/api/main.go` — `cfg := config.Load()` then `log.Setup(cfg.LogFormat, cfg.LogLevel)` first. Uses `cfg.HTTPPort` and `cfg.HTTPShutdownTimeout`.

**Verify** — confirmed working:
- `go run ./cmd/api` (dev default): tint colored output, port 8080, `[GIN-debug]` chatter present, `/health` → 200.
- `APP_ENV=prod go run ./cmd/api`: JSON log output with source attribution; no `[GIN-debug]` chatter; no debug-mode warning.
- `APP_ENV=prdo go run ./cmd/api`: exits with `invalid APP_ENV: "prdo" (want dev|prod|test)`.

---

## Step 2.5 — mise Toolchain + `cmd/cli` Scaffold ✅ DONE
**Goal:** Get the project to "clone → `mise install` → ready" and put the operator CLI binary in place so future ops actions slot into an existing scaffold instead of building one each time.

**Implemented:**
- `mise.toml` (repo root) — pins `go = "1.25.0"`; installs `dlv` via mise's `go:` backend (`"go:github.com/go-delve/delve/cmd/dlv" = "latest"`) so `mise install` is the one-and-only setup step; auto-loads `.env` into the shell via `[env] _.file = ".env"`; tasks: `server` (`go run ./cmd/api`), `console` (`dlv debug ./cmd/api`), `cli` (`go run ./cmd/cli`).
- `cmd/cli/main.go` — Cobra root command, no subcommands yet. Heavy package doc comment explaining the rationale (Go has no `iex --remsh`; ops actions are CLI subcommands that wrap `internal/usecase/...` functions), what NOT to use this binary for, and how to add a subcommand. Future command files (`cmd/cli/requests.go`, etc.) will be lean — rationale lives once in `main.go`.
- `go.mod` — added `github.com/spf13/cobra` (de-facto Go CLI library; stdlib `flag` is fine for one command, painful at five).
- **godotenv stays.** mise auto-loads `.env` into interactive shells; `godotenv.Load()` in `config.go` handles compiled-binary, CI, and prod cases where mise isn't managing the shell.

**Convention:** every ad-hoc operation an operator might run (list/retry/cancel a job, update a row in a controlled way, etc.) lands as a subcommand under `cmd/cli/`, each wrapping an `internal/usecase/...` function so the same domain logic backs both HTTP handlers and CLI invocations.

**Verify** — confirmed working:
- `mise install` — installs Go 1.25.0 if missing.
- `mise tasks` — lists `cli`, `console`, `server`.
- `mise env` — shows `APP_ENV=dev`, `HTTP_PORT=8080` loaded from `.env`.
- `mise run cli -- --help` — prints Cobra's root help.
- `go build ./...` — both `cmd/api` and `cmd/cli` compile cleanly.

---

## Step 3 — Postgres + GORM ✅ DONE
**Goal:** API can talk to Postgres. Add a `User` entity and `GET/POST /users` endpoints exercising the full layer stack: handler → service → repository → db.

**Layering decision (revised — the original pragmatic call was reversed):** the first draft above proposed *one* `User` struct serving both domain entity and persistence model, with `gorm:` + `json:` tags on it. We rejected that: it leaks ORM/transport concerns into the innermost layer. Final approach is strict separation —
- `internal/domain/user.go` — plain struct, **zero** `gorm:`/`json:`/`binding:` tags.
- `internal/adapter/repository/postgres/user_model.go` — separate `userModel` (GORM tags live here) + `toDomain`/`fromDomain`.
- `internal/adapter/http/handler/users.go` — wire DTOs (`createUserRequest`, `userResponse`) with `json:`/`binding:` tags live here.
- `internal/usecase/user.go` — owns the `UserRepository` interface (dependency inversion) and error sentinels `ErrUserNotFound`, `ErrUserEmailTaken`; business logic in `UserUseCase`.

**Implemented:**
- Config (`internal/config/`): DB env vars (`DB_HOST/PORT/USER/PASSWORD/NAME/SSL_MODE`) + `mustGetenv` helper + `DatabaseDSN()`; pool knobs hardcoded per profile (prod 25/5/5m, dev 10/2/5m, test 5/1/1m).
- `postgres/db.go` — `New(cfg) (*gorm.DB, error)`: open, ping, pool limits. `postgres/gorm_logger.go` — bridges GORM's logger to `internal/log` (the only sanctioned `fmt.Sprintf`-into-logger bridge).
- `postgres/user_repository.go` — implements `usecase.UserRepository`; converts `gorm.ErrRecordNotFound` → `usecase.ErrUserNotFound` and `pgconn.PgError` `23505` → `usecase.ErrUserEmailTaken` at the boundary. `*gorm.DB` never leaves the package via exported signatures.
- `handler/users.go` — depends on an unexported `userServicer` interface (consumer-side inversion, fake-able in tests); maps sentinels to 404 / 409, logs unexpected errors before 500.
- **Wiring is swap-resistant:** `usecase.Repositories` bundle + `postgres.NewRepositories(db)`; per-feature `router.RegisterUsers(r, repos)`; `api.New(repos)`. `gorm.io/...` is confined to `cmd/api/main.go` + `internal/adapter/repository/postgres/` — the entire HTTP layer is ORM-agnostic. Adding an entity never touches `main.go`.
- **Dev-only CLI lifecycle:** `cmd/cli` `db_setup` / `db_reset` (gated to `EnvDev`) → `postgres.CreateDatabase` / `DropDatabase`, then `postgres.Migrate` (Step 4). Conventions live in `AGENTS.md` (renamed from CLAUDE.md for cross-vendor agents).

**Note:** Schema authority belongs to the migration sequence in `internal/adapter/repository/postgres/migrations/` (Step 4). The API server never mutates schema at boot.

**Verify** (DB on `localhost:5432`, `.env` sets `DB_NAME=go_db`):
```bash
mise run cli -- db_reset      # drop + recreate go_db, apply migrations
mise run server               # boots; "db connected" then "starting server"

curl -s localhost:8081/users                                   # → []
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"a@b.com","name":"Test"}'                    # → 201
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"a@b.com","name":"Dup"}'                     # → 409 {"error":"email_taken"}
curl -s localhost:8081/users/<id>                              # → {...}; random uuid → 404; bad uuid → 400
```

---

## Step 4 — Migrations with gormigrate ✅ DONE
**Goal:** Replace the temporary `postgres.AutoMigrate` with a versioned migration sequence so schema changes are ordered, applied-state-tracked, and reproducible across environments.

**Tool choice:** `github.com/go-gormigrate/gormigrate/v2` — community library that wraps GORM's Migrator with version tracking. Picked over golang-migrate (SQL files, most common) because it stays inside the GORM ecosystem we already use and keeps schema work in Go. Tradeoff: less battle-tested at scale (~3.5k vs ~14k stars).

**Schema authority moved** from "live `userModel` tags" (AutoMigrate inspects them) to "the ordered sequence of migration functions in `internal/adapter/repository/postgres/migrations/`". The live `userModel` tags now only drive runtime row mapping.

**Implemented:**
- `internal/adapter/repository/postgres/migrations/` — sub-package: `migrations.go` aggregates `All []*gormigrate.Migration`; each migration in its own `<YYYYMMDDHHMMSS>_<desc>.go` file (timestamp prefix → no upper bound, conflict-free across parallel branches). First migration `20260531104432_create_users` uses a frozen `usersV1` struct (snapshot — never the live model) and creates `users` via `tx.Table("users").Migrator().CreateTable(&usersV1{})`. Same schema today's AutoMigrate produced.
- `internal/adapter/repository/postgres/migrate.go` — `postgres.Migrate(cfg) error`: opens the DB, runs `gormigrate.New(db, DefaultOptions, migrations.All).Migrate()`. Idempotent; tracked in the `migrations` table.
- `postgres.AutoMigrate` **deleted** from `admin.go`.
- `cmd/cli/migrate.go` — new `cli migrate` command. **Registered in all environments** (the prod-safe deploy step). `cli db_setup` (still dev-only) now calls `postgres.Migrate` after `CreateDatabase`.

**Convention:** Each migration must be self-contained — declare any structs locally (frozen snapshots) or use raw `tx.Exec`. Referencing the live `userModel` makes historical migrations change behavior as the model evolves; that's the classic gormigrate pitfall.

**Verify**
```bash
mise run cli -- db_reset           # drop go_db, create it, apply 0001
mise run cli -- migrate            # idempotent: "no migrations to run"

# Confirm the tracking table
docker exec -i pg psql -U postgres -d go_db -c "SELECT * FROM migrations;"
# → id="0001_create_users"

# Drop just the users table and re-migrate — re-creates the table but does
# NOT re-record the migration (entry is still in `migrations`).
docker exec -i pg psql -U postgres -d go_db -c "DROP TABLE users;"
mise run cli -- migrate            # no-op (entry already present!) — this is a known gormigrate gotcha
# For partial recovery, db_reset is the dev tool of choice.

curl -s localhost:8081/users   # → {"success":true,"users":[]} (table is back via db_reset)
```

---

## Step 5 — Seeds ✅ DONE
**Goal:** A `cli seed` command that idempotently inserts demo data, going through the use-case layer so seeds exercise the same domain factory + repository chain as production traffic. The original spec (`cmd/seed/main.go` with `domain.User{}` struct literals via raw GORM) predated the architecture work and was incompatible with three current rules — factories only, through-use-case writes, operator actions under `cmd/cli`.

**Tool choice:** in-process sub-package `internal/seeds/` with self-registration via `init()`, mirroring the migrations pattern. No tracking table — idempotency comes from each entity's natural-key "taken" sentinel (`usecase.ErrUserEmailTaken` for users), which the seeder catches and skips.

**Implemented:**
- `internal/seeds/seeds.go` — `Seeder{Name, Run}` type, unexported `register(name, fn)` (panics on duplicate name), `All()`, `Find(name)`.
- `internal/seeds/users.go` — `init()` registers `"users"` seeder with `Tables: []string{"users"}`. Constructs `usecase.NewUserUseCase(repos.Users)` and **pre-checks via `GetByEmail`** before calling `Create` so re-seeds don't spam the gormLogger with 23505 errors. The `ErrUserEmailTaken` catch on Create stays as belt-and-braces.
- `cmd/cli/seed.go` — `cli seed` command. No args ⇒ run all in order; `cli seed <name>` ⇒ targeted; `cli seed --list` ⇒ print registered names; `cli seed --reset [name]` ⇒ truncate the targeted seeders' `Tables` (RESTART IDENTITY CASCADE) then run. Resolves target *before* opening the DB so typos don't burn a connection.
- `internal/usecase/user.go` + `internal/adapter/repository/postgres/user_repository.go` — `GetByEmail(ctx, email) (*User, error)` added to support the seeder pre-check (and any future auth/lookup flow). `gorm.ErrRecordNotFound` → `usecase.ErrUserNotFound` at the boundary, same pattern as `Get(id)`.
- `internal/adapter/repository/postgres/admin.go` — `TruncateTables(cfg, []string) error` for the `--reset` path.
- `cmd/cli/main.go` — registers `seed` in the existing `EnvDev`-gated block (next to `db_setup`, `db_reset`, `generate_migration`).

**Why not under `internal/adapter/repository/postgres/`?** Seeds speak `usecase.Repositories` (an interface), not `*gorm.DB`. Putting them under postgres would falsely couple them to the current adapter — `internal/seeds/` is the application-data home.

**Verify**
```bash
mise run cli -- db_reset
mise run cli -- seed --list                                  # → users
mise run cli -- seed users                                   # → inserted=3
mise run cli -- seed users                                   # → inserted=0 (additive default, no SQL errors)
mise run cli -- seed users --reset                           # WRN truncating + inserted=3
mise run cli -- seed nope                                    # → Error: unknown seeder "nope"

mise run server &
curl -s localhost:8081/users | jq '.users | length'          # → 3
pkill -f "go run ./cmd/api"

APP_ENV=prod go run ./cmd/cli seed                           # → unknown command "seed"
```

---

## Step 6 — Asynq Worker + Redis ✅ DONE
**Goal:** `POST /users` enqueues a welcome-email task; a separate worker process consumes it and (today) logs the send.

**Failure semantics chosen:** log + continue on enqueue failure — the user is already committed, a missed welcome email is not a request failure. Documented in `AGENTS.md`; the comment in `UserUseCase.Create` flags the outbox-pattern seam if delivery ever becomes business-critical.

**Implemented:**
- **Single `internal/queue/` package** owns both sides of the asynq + Redis boundary (producer + consumer) **and** the payload contract. The original three-way split (`tasks/` + `adapter/queue/` + `adapter/worker/`) was collapsed — producer and consumer were the same monolith on either side of one Redis, so the separation bought nothing the Go linker doesn't already give us via dead-code elimination.
  - **One file per task type:** `internal/queue/welcome_email.go` holds `TypeWelcomeEmail` + `WelcomeEmailPayload` + `(c *Client) EnqueueWelcomeEmail(...)` producer method + `WelcomeEmailHandler.Handle(...)` consumer. Adding a task = one new file with all three sides.
  - **Shared infra:** `client.go` (`Client` wrapping `*asynq.Client`, `New(cfg)`, `Close()`, `NewProducers(c) usecase.Producers`, `RedisOpt(cfg)`) and `logger.go` (`NewAsynqLogger()` printf→`internal/log` bridge — same shape as gormLogger).
  - Consumer-side handlers depend on the *narrowest* use-case-layer interface they actually need (`usecase.UserRepository` for read-only lookups — don't drag in the full `UserUseCase` or its write-side producers).
- **Consumer-defined interface** `WelcomeEmailEnqueuer` in `internal/usecase/user.go`; `UserUseCase` gets a `welcome` field, `Create` enqueues after the repo create with log + continue.
- **`usecase.Producers` bundle** in `internal/usecase/producers.go` — mirror of `Repositories`. Folded into `usecase.Deps` (along with `Repos`) so wiring signatures don't grow: `api.New(deps)` → `router.Register(engine, deps)` → `RegisterUsers(r, deps)`.
- **`cmd/worker/main.go`** composition root: config → log → DB → repos → `asynq.NewServer(queue.RedisOpt(cfg), asynq.Config{Concurrency: 10, Logger: queue.NewAsynqLogger()})` → `asynq.ServeMux` with `queue.TypeWelcomeEmail` handler → `srv.Run(mux)` (asynq owns SIGINT/SIGTERM).
- **Seeds use a no-op enqueuer** — demo emails are fake, no point spamming the queue with junk tasks for `alice@example.com` etc.
- Config gains `RedisAddr`/`RedisPassword`; `.env` + `.env.example` get `REDIS_ADDR=localhost:6379`; `mise.toml` gets `[tasks.worker]`.

**Verify**
```bash
docker run -d --name redis -p 6379:6379 redis:7-alpine   # Step 8 moves to compose
mise run cli -- db_reset
mise run worker &                                          # waits on tasks
mise run server &

# Create a user → 201 + worker logs the send within ~ms
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"new@example.com","name":"New"}'
# worker stdout: INF welcome email sent to=new@example.com user_id=<uuid>

# Log-and-continue on Redis outage: user still saved, no welcome task
docker stop redis
curl -X POST localhost:8081/users -d '{"email":"loss@example.com","name":"L"}'   # → 201
# API logs: WRN welcome email enqueue failed err=...
docker start redis

pkill -f "go run ./cmd/(api|worker)"
```

---

## Step 7 — Tests + Linting + CI
**Goal:** Quality gates so future changes don't silently regress. Tests at every layer (domain → use case → handler → integration), a lint stack as the Go equivalent of Elixir's `credo`, and a CI workflow that runs both on every push. Currently the codebase has zero automated tests — this is the highest-leverage step left before shipping anything.

**Deps**
```bash
go get -t github.com/stretchr/testify
go get -t github.com/ory/dockertest/v3
```
mise tools: add `golangci-lint` via the `go:` backend (the closest Go analog to credo — wraps `govet`, `staticcheck`, `errcheck`, `revive`, `unused`, `gocritic`, `gosec`, etc.).

**Files**
- `internal/domain/user_test.go` — table tests for `NewUser` (every invariant violated + accepted cases) and `ReconstituteUser` (drift detection on bad stored data).
- `internal/usecase/user_test.go` — `UserUseCase` against a fake `UserRepository`; asserts policy errors (`ErrUserEmailTaken`) propagate from the adapter.
- `internal/adapter/http/handler/users_test.go` — handler tests via `httptest` + a fake `userUseCase` (the consumer-defined interface we built precisely for this). Asserts envelope shape (`success`, `details`) and status codes for every branch.
- `internal/adapter/repository/postgres/user_repository_test.go` — integration: `dockertest` spins up Postgres, `postgres.Migrate(cfg)` applies migrations, exercises CRUD + the `pgconn` 23505 → `ErrUserEmailTaken` translation. Skipped (with a clear message) when Docker is unreachable.
- `.golangci.yml` — opinionated config: enable `govet`, `staticcheck`, `errcheck`, `revive`, `unused`, `gocritic`, `gosec`; exclude generated/migration files where appropriate; strict rules on errors (`errcheck`) and unused returns.
- `.github/workflows/ci.yml` — checkout, install Go + golangci-lint via mise, run `go vet`, `golangci-lint run`, `go test ./... -race -coverprofile=cover.out`, upload coverage.
- `mise.toml` — `[tasks.test]` (`go test ./... -race`), `[tasks.lint]` (`golangci-lint run`).

**Convention** — every new entity/feature ships with at least domain + use-case tests in the same PR; handler tests when the response shape changes; integration tests for new repository methods. Add this to `AGENTS.md` under "Build / verify."

**Verify**
```bash
mise run test           # green
mise run lint           # clean
# CI: open a no-op PR; the workflow runs and reports.
```

---

## Step 8 — Docker Compose ✅ DONE
**Goal:** Replace the manual `docker run pg` / `docker run redis` from earlier steps with one declarative file so a developer (or CI) brings up local infra with a single command.

**Scope-adjustment from the original spec:**
- Dropped the `migrate/migrate:latest` one-shot service. Our migrations are gormigrate (Go functions), not standalone SQL files, so the migrate runner *is* our binary. The "migrate as a compose service" naturally belongs in Step 9 where the app image lands — until then `mise run cli -- migrate` from the host is fine and matches the existing operator workflow.
- Postgres credentials match `.env` (`postgres / postgres / go_db`) so host-side `mise run server` / `mise run worker` / `mise run cli` keep working against the compose-managed services through localhost — no env switch needed.

**Implemented:**
- `docker-compose.yml` with:
  - **postgres** — `postgres:16-alpine`, env from compose, `5432:5432`, `pgdata` volume, `pg_isready` healthcheck.
  - **redis** — `redis:7-alpine`, `6379:6379`, `redisdata` volume, `redis-cli ping` healthcheck.
  - **asynqmon** — `hibiken/asynqmon:latest` under `profiles: ["dev"]` so default `up` doesn't start it; binds `:8082` (not `:8081`, which collides with the API). Lets you poke pending / archived / retried tasks.
- mise tasks: `infra` (`docker compose up -d`), `infra:dev` (with asynqmon), `infra:down` (stop, keep data).

**Convention:** local dev = compose for infra + `mise run server/worker` for the Go binaries on the host. Containerizing the binaries themselves lands in Step 9 (then `DB_HOST=postgres`, `REDIS_ADDR=redis:6379` get set in each app service's `environment:` block, leaving `.env` for host-side runs).

**Verify**
```bash
# Clean slate (wipes pgdata + redisdata)
docker compose down -v

# Bring up infra
mise run infra                      # postgres + redis, both healthy
# or: mise run infra:dev            # + asynqmon at http://localhost:8082

mise run cli -- db_reset            # creates go_db, runs gormigrate
mise run worker &
mise run server &

curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"compose@test.com","name":"C"}'
# → 201 + worker logs "welcome email sent"

mise run infra:down                 # stop, keep volumes for next time
```

---

## Step 9 — Dockerfile + API/Worker as Services
**Goal:** Build the Go binaries into container images and add `api` + `worker` services to compose.

**Files**
- `Dockerfile` (multi-stage, multi-target)
  ```dockerfile
  FROM golang:1.25-alpine AS build
  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -o /out/api    ./cmd/api
  RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker
  RUN CGO_ENABLED=0 go build -o /out/seed   ./cmd/seed

  FROM alpine:3.20 AS api
  RUN apk add --no-cache ca-certificates
  COPY --from=build /out/api /usr/local/bin/api
  ENTRYPOINT ["/usr/local/bin/api"]

  FROM alpine:3.20 AS worker
  RUN apk add --no-cache ca-certificates
  COPY --from=build /out/worker /usr/local/bin/worker
  ENTRYPOINT ["/usr/local/bin/worker"]
  ```
- Update `docker-compose.yml` — add:
  - **api** — `build: { context: ., target: api }`, `8080:8080`, `env_file: .env`, `depends_on: { postgres: {condition: service_healthy}, redis: {condition: service_healthy}, migrate: {condition: service_completed_successfully} }`. Override `DB_HOST=postgres`, `REDIS_ADDR=redis:6379` via `environment:`.
  - **worker** — same image with `target: worker`, same `depends_on`.

**Verify**
```bash
docker compose up -d --build
docker compose ps                            # api, worker, postgres, redis all up
curl localhost:8080/health                   # 200
curl -X POST localhost:8080/users -H 'Content-Type: application/json' \
     -d '{"email":"docker@example.com","name":"Docker"}'
docker compose logs worker                   # shows the welcome-email log line
```

---

## Step 10 — Makefile
**Goal:** One-line commands for the most common workflows.

**Files**
- `Makefile`
  ```makefile
  .PHONY: up down logs migrate-up migrate-down migrate-create seed run-api run-worker tidy

  up:        ; docker compose up -d --build
  down:      ; docker compose down
  logs:      ; docker compose logs -f api worker
  seed:      ; docker compose run --rm api /usr/local/bin/seed   # add a seed target to Dockerfile if needed
  run-api:   ; go run ./cmd/api
  run-worker:; go run ./cmd/worker
  tidy:      ; go mod tidy

  migrate-up:
  	docker run --rm -v $(PWD)/migrations:/migrations --network host \
  	  migrate/migrate -path=/migrations \
  	  -database "postgres://app:app@localhost:5432/app?sslmode=disable" up

  migrate-down:
  	docker run --rm -v $(PWD)/migrations:/migrations --network host \
  	  migrate/migrate -path=/migrations \
  	  -database "postgres://app:app@localhost:5432/app?sslmode=disable" down 1

  migrate-create:
  	docker run --rm -v $(PWD)/migrations:/migrations \
  	  migrate/migrate create -ext sql -dir /migrations -seq $(name)
  ```

**Verify:** `make up && make logs` works end-to-end. `make migrate-create name=add_posts_table` writes a new pair of empty migration files.

---

## Step 11 — End-to-End Smoke Test
Run after Step 10 to confirm everything is wired together.

```bash
docker compose down -v        # clean slate
make up                       # builds + starts postgres, redis, migrate, api, worker
docker compose logs migrate   # → 1/u create_users_table applied
make seed                     # 3 demo users (idempotent)

curl -s localhost:8080/users | jq 'length'      # → 3
curl -X POST localhost:8080/users -H 'Content-Type: application/json' \
     -d '{"email":"e2e@example.com","name":"E2E"}'   # → 201
docker compose logs worker | grep e2e@example.com   # task fired

open http://localhost:8081    # asynqmon — queue stats, retries, history

make down                     # tear down (keep volumes); add `-v` to nuke data
```

---

## Step 12 — Observability
**Goal:** When something is slow or failing in prod, you can attribute it. Request correlation IDs threaded through every log line, Prometheus metrics for HTTP traffic, OpenTelemetry traces, and `pprof` for live diagnostics.

**Deps**
```bash
go get github.com/prometheus/client_golang
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
```

**Files**
- `internal/adapter/http/middleware/request_id.go` — accept `X-Request-ID` from inbound or generate a fresh one; attach to the request context; echo as response header.
- `internal/log/log.go` — auto-include `request_id` (and `trace_id`/`span_id` once tracing is up) from context attributes.
- `internal/adapter/http/middleware/metrics.go` — HTTP request counter + latency histogram, labeled by `route` / `method` / `status`.
- `internal/adapter/http/handler/metrics.go` — `GET /metrics` returning Prometheus exposition format.
- `internal/adapter/http/handler/pprof.go` — wire `/debug/pprof/*` (dev/staging by default; behind auth in prod).
- `internal/observability/tracing.go` — OTel SDK setup (TracerProvider + OTLP exporter); sampler config from env.
- `cmd/api/main.go` — start the TracerProvider with a shutdown hook in graceful-shutdown.

**Verify**
- `curl /metrics` returns Prometheus format with non-zero counters after exercising endpoints.
- A request's logs all share a `request_id`; the same value comes back in the `X-Request-ID` response header.
- `/debug/pprof/goroutine?debug=2` works in dev.
- With an OTLP collector configured (Jaeger / Tempo / Honeycomb), request traces appear with handler + db spans.

---

## Step 13 — Operational Hardening
**Goal:** Survive real-world conditions — slow downstreams, transient failures, concurrent deploys, abusive clients. Mostly small middlewares + boot-time changes.

**Deps**
```bash
go get golang.org/x/time/rate
```

**Files / changes**
- `internal/adapter/http/handler/readyz.go` — `GET /readyz` probes DB (and later Redis / queue) connectivity. Distinct from `/health` (liveness): readiness gates traffic, liveness gates restarts.
- `cmd/api/main.go::startDbConnection` — exponential backoff with cap (~30s) instead of immediate `log.Fatal`. Container orchestrators often start the app before the DB is ready.
- `postgres.Migrate` — take a Postgres advisory lock (`pg_advisory_lock(<magic>)`) around the gormigrate run so two replicas can't race during a rolling deploy.
- `internal/adapter/http/middleware/timeout.go` — per-request `context.WithTimeout` (default 30s, env-tunable). Handler abort → 504.
- `internal/adapter/http/middleware/body_limit.go` — `http.MaxBytesReader` on `c.Request.Body`; default 1MB, env-tunable; 413 on overflow.
- `internal/adapter/http/middleware/security_headers.go` — CSP, HSTS, X-Content-Type-Options, Referrer-Policy, X-Frame-Options.
- `internal/adapter/http/middleware/rate_limit.go` — per-IP token bucket via `golang.org/x/time/rate`; cheap, in-process; 429 on overflow. For multi-replica, push this to the ingress layer.
- `internal/config/` — env vars for the new knobs (`HTTP_REQUEST_TIMEOUT`, `HTTP_MAX_BODY_BYTES`, `HTTP_RATE_LIMIT_RPS`, ...) with prod-safe defaults.

**Out of scope here:** authn/authz — depends entirely on the deployment shape (internal-only? OIDC? JWT? mTLS?) and worth designing once before implementing. Flag as the missing piece for any externally-reachable deploy.

**Verify**
```bash
# /readyz reflects DB state
docker stop pg; curl -s -o /dev/null -w "%{http_code}" localhost:8081/readyz   # → 503
docker start pg; curl -s -o /dev/null -w "%{http_code}" localhost:8081/readyz  # → 200

# Startup retry
docker stop pg && mise run server &     # API logs reconnect attempts, doesn't crash
docker start pg                          # API converges to "starting server"

# Body limit
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
  --data-binary @100MB.json              # → 413

# Security headers
curl -I localhost:8081/health            # CSP / HSTS / X-Content-Type-Options present

# Migration lock (two replicas, same DB)
mise run cli -- migrate & mise run cli -- migrate &   # one waits on the advisory lock
```

---

## Out of Scope (add later when needed)
- Auth (JWT / session / OIDC / mTLS) — Step 13 flags it; design is deployment-specific.
- Real SMTP for the welcome email — handler currently just logs.
