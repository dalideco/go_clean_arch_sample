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
  worker/main.go           # Asynq worker entrypoint (Step 6)
  seed/main.go             # Seed runner (Step 5)
internal/
  domain/                  # entities (innermost layer; no project-internal deps)
  service/                 # use cases / application services
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

**Dependency rule:** `cmd → adapter → service → domain`. Domain imports nothing project-internal. `adapter/queue/` and `adapter/worker/` both depend on `internal/tasks/` for shared payload types — `tasks/` is a contract, not an adapter. `log/` and `config/` are cross-cutting and used by everyone.

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

## Step 2 — Config Loading from `.env`
**Goal:** Centralized config so later steps can pull DB/Redis settings from one place. Also moves `port` and `shutdownTimeout` out of `cmd/api/main.go` constants and into env-driven config.

**Install**
```bash
go get github.com/joho/godotenv
```

**Files**
- `.env.example` — placeholders for `APP_ENV`, `HTTP_PORT`, `HTTP_SHUTDOWN_TIMEOUT`, `LOG_FORMAT`, `DB_*`, `REDIS_ADDR`.
- `.env` — copy of `.env.example`, gitignored.
- `internal/config/config.go`
  - `type Config struct { AppEnv, HTTPPort, LogFormat, DBHost, DBPort, DBUser, DBPassword, DBName, DBSSLMode, RedisAddr, RedisPassword string; HTTPShutdownTimeout time.Duration }`
  - `func Load() *Config`: calls `godotenv.Load()` (ignore not-found so prod is fine without a file), reads each var via `os.Getenv` with sensible defaults.
  - `func (c *Config) DatabaseDSN() string` — postgres DSN.
  - `func (c *Config) RedisOpt() asynq.RedisClientOpt` — *add this method in Step 6* once asynq is imported.

**Wire it in**
- `cmd/api/main.go`: call `config.Load()` first, use `cfg.HTTPPort` and `cfg.HTTPShutdownTimeout`, drop the local `port` / `shutdownTimeout` consts.
- `internal/log/log.go`: add a `Setup(cfg *config.Config)` (or `Setup(format string)`) that switches between `tint.NewHandler` (dev) and `slog.NewJSONHandler` (prod) based on `cfg.LogFormat` / `cfg.AppEnv`. `main` calls `log.Setup(cfg)` before any other code logs. Move the `init()`-time tint setup into `Setup`.

**Verify:** Set `HTTP_PORT=9090` in `.env`, restart, server now binds 9090. Set `LOG_FORMAT=json` and confirm JSON log output.

---

## Step 3 — Postgres + GORM
**Goal:** API can talk to Postgres. Add a `User` entity and a `GET /users` endpoint that returns rows from the database (initially empty). This is the first step that exercises the full layer stack: handler → service → repository → db.

**Install**
```bash
go get gorm.io/gorm
go get gorm.io/driver/postgres
go get github.com/google/uuid
```

**Run Postgres locally** (temporary — Step 7 replaces this with compose):
```bash
docker run -d --name pg -p 5432:5432 \
  -e POSTGRES_USER=app -e POSTGRES_PASSWORD=app -e POSTGRES_DB=app \
  postgres:16-alpine
```

**Files**
- `internal/domain/user.go`
  - `type User struct { ID uuid.UUID; Email, Name string; CreatedAt, UpdatedAt time.Time }` with GORM tags + JSON tags. (Pragmatic: one struct serves domain + persistence model. Re-evaluate if domain grows complex.)
- `internal/service/user.go`
  - `type UserRepository interface { List(ctx) ([]domain.User, error); Get(ctx, id) (*domain.User, error); Create(ctx, *domain.User) error }` — interface owned by the service layer (dependency inversion).
  - `type UserService struct { repo UserRepository }`
  - `func NewUserService(repo UserRepository) *UserService`
  - Methods: `List`, `Get`, `Create`.
- `internal/adapter/repository/postgres/db.go`
  - `func New(cfg *config.Config) (*gorm.DB, error)`: GORM open, ping `*sql.DB`, set pool limits (`SetMaxOpenConns(25)`, `SetMaxIdleConns(5)`, `SetConnMaxLifetime(5*time.Minute)`).
- `internal/adapter/repository/postgres/user_repository.go`
  - `type UserRepository struct { db *gorm.DB }` — implements `service.UserRepository`.
- `internal/adapter/http/handler/users.go`
  - `type UsersHandler struct { svc *service.UserService }`
  - `List`, `Get`, `Create` methods (Create is bind+validate JSON, no enqueue yet — added in Step 6).
- Update `internal/adapter/http/router/router.go` — accept a `Handlers` struct (or similar dependency container), register `/users` routes.
- Update `internal/adapter/http/api/api.go` — accept dependencies, pass to `router.Register`.
- `cmd/api/main.go` — wire: `cfg → db → repo → svc → handlers → api.New(handlers)`.

**Note:** Don't use `db.AutoMigrate` — schema authority belongs to migrations (Step 4).

**Verify**
```bash
# Manually create the table for now (Step 4 automates this):
docker exec -i pg psql -U app -d app <<'SQL'
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE TABLE users (id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  email TEXT UNIQUE NOT NULL, name TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
SQL

curl -s localhost:8080/users                  # → []
curl -X POST localhost:8080/users -H 'Content-Type: application/json' \
     -d '{"email":"a@b.com","name":"Test"}'   # → 201
curl -s localhost:8080/users                  # → [{...}]
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
- Update `internal/service/user.go` — `Create` calls `queue.EnqueueWelcomeEmail(ctx, user.ID)` after the DB insert. **Log and continue on enqueue failure** — don't fail the request, the user is already saved (comment why).
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
