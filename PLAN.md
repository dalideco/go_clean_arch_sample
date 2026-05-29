# Go Project Scaffold — Step-by-Step Build Plan

## Context
Greenfield project at `/Users/dali/Documents/GitHub/projects/go_project_sample`. Goal: build up a Gin API + Postgres + Asynq worker stack incrementally — each step adds one capability, installs only the libraries it needs, and ends in a runnable, verifiable state. By the time you finish, you'll have a `users` resource where `POST /users` writes to Postgres and enqueues an async "welcome email" job that the worker consumes from Redis.

Module path: `github.com/dali/go_project_sample`.

Stack picks (most common in the Go ecosystem): **Gin** for HTTP, **GORM** for the ORM, **golang-migrate** for migrations, **Asynq** for the worker.

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
- **Dev-only CLI lifecycle:** `cmd/cli` `db_setup` / `db_reset` (gated to `EnvDev`) → `postgres.CreateDatabase` / `DropDatabase`, plus a **temporary** `postgres.AutoMigrate` for schema bring-up (removed when Step 4 lands). Conventions live in `AGENTS.md` (renamed from CLAUDE.md for cross-vendor agents).

**Note:** `db.AutoMigrate` is used **only** as a dev-only bring-up via `cli db_setup`, never at API boot — schema authority still belongs to migrations (Step 4).

**Verify** (DB on `localhost:5432`, `.env` sets `DB_NAME=go_db`):
```bash
mise run cli -- db_reset      # drop + recreate go_db, AutoMigrate users table
mise run server               # boots; "db connected" then "starting server"

curl -s localhost:8081/users                                   # → []
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"a@b.com","name":"Test"}'                    # → 201
curl -X POST localhost:8081/users -H 'Content-Type: application/json' \
     -d '{"email":"a@b.com","name":"Dup"}'                     # → 409 {"error":"email_taken"}
curl -s localhost:8081/users/<id>                              # → {...}; random uuid → 404; bad uuid → 400
```

---

## Step 4 — Migrations with golang-migrate
**Goal:** Replace the manual `psql` table creation with a versioned migration file.

**Install (CLI only, not a Go dep)**
```bash
brew install golang-migrate
# or use the migrate/migrate Docker image — done in Step 7
```

**Files**
- `migrations/000001_create_users_table.up.sql`
  ```sql
  CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
  CREATE TABLE users (
      id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
      email       TEXT NOT NULL UNIQUE,
      name        TEXT NOT NULL,
      created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_users_email ON users(email);
  ```
- `migrations/000001_create_users_table.down.sql` — `DROP TABLE IF EXISTS users;`

**Verify**
```bash
docker exec -i pg psql -U app -d app -c 'DROP TABLE users;'   # reset
migrate -path ./migrations \
  -database "postgres://app:app@localhost:5432/app?sslmode=disable" up
# → 1/u create_users_table (xx ms)
curl -s localhost:8080/users   # → [] (table is back)
```

---

## Step 5 — Seed Data
**Goal:** A `cmd/seed` binary that idempotently inserts demo users.

**No new deps** — uses GORM from Step 3.

**Files**
- `cmd/seed/main.go`
  - Loads config + db, then:
    ```go
    users := []domain.User{
        {Email: "alice@example.com", Name: "Alice"},
        {Email: "bob@example.com",   Name: "Bob"},
        {Email: "carol@example.com", Name: "Carol"},
    }
    db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}}, DoNothing: true}).
       Create(&users)
    ```
  - Logs how many rows were inserted, exits 0.

**Verify**
```bash
go run ./cmd/seed
curl -s localhost:8080/users | jq 'length'   # → 3
go run ./cmd/seed                            # idempotent — still 3
```

---

## Step 6 — Asynq Worker + Redis
**Goal:** `POST /users` enqueues a "send welcome email" task; a separate worker process consumes it and logs the email.

**Install**
```bash
go get github.com/hibiken/asynq
```

**Run Redis locally** (temporary — Step 7 moves it to compose):
```bash
docker run -d --name redis -p 6379:6379 redis:7-alpine
```

**Files**
- Update `internal/config/config.go` — add `RedisOpt() asynq.RedisClientOpt` returning `asynq.RedisClientOpt{Addr: c.RedisAddr, Password: c.RedisPassword}`.
- `internal/tasks/tasks.go` — task name constants. `const TypeWelcomeEmail = "email:welcome"`.
- `internal/tasks/email.go` — payload types only (no asynq logic).
  - `type WelcomeEmailPayload struct { UserID uuid.UUID }`
  - `func NewWelcomeEmailTask(userID uuid.UUID) (*asynq.Task, error)` — JSON-marshals payload, returns `asynq.NewTask(TypeWelcomeEmail, data)`. (This *does* import asynq, but the type itself is the contract.)
- `internal/adapter/queue/client.go`
  - `type Client struct { c *asynq.Client }`
  - `func NewClient(opt asynq.RedisClientOpt) *Client`
  - `func (c *Client) EnqueueWelcomeEmail(ctx context.Context, userID uuid.UUID) error` — uses `tasks.NewWelcomeEmailTask`.
  - `func (c *Client) Close() error`
- `internal/adapter/worker/email.go`
  - `type EmailHandler struct { db *gorm.DB }`
  - `func (h *EmailHandler) HandleWelcomeEmail(ctx context.Context, t *asynq.Task) error` — unmarshal `tasks.WelcomeEmailPayload`, look up user, log `"sending welcome email to <email>"`. Return non-nil error to trigger asynq's retry.
- `cmd/worker/main.go`
  - `config.Load()` → `log.Setup(cfg)` → `db.New(cfg)` → build `asynq.NewServer(cfg.RedisOpt(), asynq.Config{Concurrency: 10, Queues: map[string]int{"default": 1}})`.
  - `mux := asynq.NewServeMux(); mux.HandleFunc(tasks.TypeWelcomeEmail, (&worker.EmailHandler{DB: db}).HandleWelcomeEmail)`
  - `srv.Run(mux)` — asynq handles SIGINT/SIGTERM itself.
- Update `internal/usecase/user.go` — `Create` calls `queue.EnqueueWelcomeEmail(ctx, user.ID)` after the DB insert. **Log and continue on enqueue failure** — don't fail the request, the user is already saved (comment why).
- Update `internal/adapter/http/api/api.go`, `internal/adapter/http/router/router.go`, `cmd/api/main.go` — pass `*queue.Client` through the dependency chain.

**Verify**
```bash
# Terminal 1
go run ./cmd/worker
# Terminal 2
go run ./cmd/api
# Terminal 3
curl -X POST localhost:8080/users -H 'Content-Type: application/json' \
     -d '{"email":"new@example.com","name":"New"}'
# Terminal 1 logs: "sending welcome email to new@example.com"
```

---

## Step 7 — Docker Compose
**Goal:** Replace the manual `docker run pg` / `docker run redis` from earlier steps with one declarative file. Add a one-shot `migrate` service that applies migrations on startup.

**No Go deps.**

**Files**
- `docker-compose.yml`
  - **postgres** — `postgres:16-alpine`, env `POSTGRES_USER=app`, `POSTGRES_PASSWORD=app`, `POSTGRES_DB=app`, named volume `pgdata:/var/lib/postgresql/data`, healthcheck `pg_isready -U app`, `5432:5432`.
  - **redis** — `redis:7-alpine`, named volume `redisdata:/data`, healthcheck `redis-cli ping`, `6379:6379`.
  - **migrate** — `migrate/migrate:latest`, mounts `./migrations:/migrations`, command `["-path=/migrations","-database=postgres://app:app@postgres:5432/app?sslmode=disable","up"]`, `depends_on: postgres (service_healthy)`, `restart: "no"` (one-shot).
  - **asynqmon** *(optional, dev profile)* — `hibiken/asynqmon:latest`, `8081:8080`, `--redis-addr=redis:6379`. Web UI for poking at the queue.
  - Volumes: `pgdata`, `redisdata`.
- Update `.env` — change `DB_HOST=postgres` and `REDIS_ADDR=redis:6379` only when running inside compose; for local `go run` keep `localhost`. Easiest: keep `.env` for local dev, pass overrides via compose `environment:` block in Step 8.

**Verify**
```bash
docker compose down -v   # clean slate
docker compose up -d postgres redis
docker compose run --rm migrate   # applies 000001
go run ./cmd/api                  # still works against compose-managed pg
```

---

## Step 8 — Dockerfile + API/Worker as Services
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

## Step 9 — Makefile
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

## Step 10 — End-to-End Smoke Test
Run after Step 9 to confirm everything is wired together.

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

## Out of Scope (add later when needed)
- Auth (JWT/session middleware)
- Real SMTP for the welcome email — handler currently just logs
- Tests (`_test.go`) — testify + dockertest is the usual combo
- CI config
- Linting (`.golangci.yml`)
