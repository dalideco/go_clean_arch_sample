# Go Project Scaffold — Step-by-Step Build Plan

## Context
Greenfield project at `/Users/dali/Documents/GitHub/projects/go_project_sample`. Goal: build up a Gin API + Postgres + Asynq worker stack incrementally — each step adds one capability, installs only the libraries it needs, and ends in a runnable, verifiable state. By the time you finish, you'll have a `users` resource where `POST /users` writes to Postgres and enqueues an async "welcome email" job that the worker consumes from Redis.

Module path: `github.com/dali/go_project_sample`.

Stack picks (most common in the Go ecosystem): **Gin** for HTTP, **GORM** for the ORM, **golang-migrate** for migrations, **Asynq** for the worker.

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

## Step 1 — Gin API with a Health Endpoint
**Goal:** A Gin server you can `curl` returns 200 on `/health`. No DB, no worker — just prove the HTTP layer works.

**Install**
```bash
go get github.com/gin-gonic/gin
```

**Files**
- `cmd/api/main.go`
  - `func main()`: builds a `gin.Default()` engine, registers `GET /health` → `c.JSON(200, gin.H{"status":"ok"})`, calls `r.Run(":8080")`.
- `internal/router/router.go`
  - `func New() *gin.Engine`: returns the configured engine. Keeps `main.go` thin.
- `internal/handlers/health.go`
  - `func Health(c *gin.Context)`: the handler.

**Verify**
```bash
go run ./cmd/api
curl -s localhost:8080/health   # → {"status":"ok"}
```

---

## Step 2 — Config Loading from `.env`
**Goal:** Centralized config so later steps can pull DB/Redis settings from one place.

**Install**
```bash
go get github.com/joho/godotenv
```

**Files**
- `.env.example` — placeholders for `HTTP_PORT`, `DB_*`, `REDIS_ADDR` (see Step 3 / Step 5 for values).
- `.env` — copy of `.env.example`, gitignored.
- `internal/config/config.go`
  - `type Config struct { HTTPPort, DBHost, DBPort, DBUser, DBPassword, DBName, DBSSLMode, RedisAddr, RedisPassword string }`
  - `func Load() *Config`: calls `godotenv.Load()` (ignore `os.IsNotExist` error so prod is fine without a file), reads each var via `os.Getenv` with defaults.
  - `func (c *Config) DatabaseDSN() string` — builds the postgres DSN string.
  - `func (c *Config) RedisOpt() asynq.RedisClientOpt` — *add this method in Step 5* once asynq is imported.

**Wire it in:** `cmd/api/main.go` calls `config.Load()` at the top and uses `cfg.HTTPPort`.

**Verify:** Set `HTTP_PORT=9090` in `.env`, restart, server now binds 9090.

---

## Step 3 — Postgres + GORM
**Goal:** API can talk to Postgres. Add a `User` model and a `GET /users` endpoint that returns rows from the database (initially empty).

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
- `internal/db/db.go`
  - `func New(cfg *config.Config) (*gorm.DB, error)`: `gorm.Open(postgres.Open(cfg.DatabaseDSN()), &gorm.Config{})`, ping the underlying `*sql.DB`, set pool limits (`SetMaxOpenConns(25)`, `SetMaxIdleConns(5)`, `SetConnMaxLifetime(5*time.Minute)`).
- `internal/models/user.go`
  - `type User struct { ID uuid.UUID; Email, Name string; CreatedAt, UpdatedAt time.Time }` with GORM tags.
- `internal/handlers/users.go`
  - `type UsersHandler struct { db *gorm.DB }`
  - `func (h *UsersHandler) List(c *gin.Context)` — `db.Limit(100).Find(&users)`.
  - `func (h *UsersHandler) Get(c *gin.Context)` — fetch by `:id`, 404 if not found.
  - `func (h *UsersHandler) Create(c *gin.Context)` — bind `{email, name}`, insert, return 201. (No enqueue yet — added in Step 6.)
- `internal/router/router.go` — accept `*gorm.DB`, register `/users` routes.
- `cmd/api/main.go` — call `db.New(cfg)`, pass to `router.New(db)`.

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
    users := []models.User{
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
- `internal/tasks/tasks.go`
  - `const TypeWelcomeEmail = "email:welcome"`
- `internal/tasks/email.go`
  - `type WelcomeEmailPayload struct { UserID uuid.UUID }`
  - `func NewWelcomeEmailTask(userID uuid.UUID) (*asynq.Task, error)` — JSON-marshals payload, returns `asynq.NewTask(TypeWelcomeEmail, data)`.
  - `type EmailHandler struct { db *gorm.DB }`
  - `func (h *EmailHandler) HandleWelcomeEmail(ctx context.Context, t *asynq.Task) error` — unmarshal payload, look up user, log `"sending welcome email to <email>"`. Return non-nil error to trigger asynq's retry.
- `internal/queue/client.go`
  - `type Client struct { c *asynq.Client }`
  - `func NewClient(opt asynq.RedisClientOpt) *Client`
  - `func (c *Client) EnqueueWelcomeEmail(ctx context.Context, userID uuid.UUID) error`
  - `func (c *Client) Close() error`
- `cmd/worker/main.go`
  - Loads config + db, builds `asynq.NewServer(cfg.RedisOpt(), asynq.Config{Concurrency: 10, Queues: map[string]int{"default": 1}})`.
  - `mux := asynq.NewServeMux(); mux.HandleFunc(tasks.TypeWelcomeEmail, (&tasks.EmailHandler{DB: db}).HandleWelcomeEmail)`
  - `srv.Run(mux)` — asynq handles SIGINT/SIGTERM itself.
- Update `internal/handlers/users.go` — `Create` calls `h.queue.EnqueueWelcomeEmail(c, user.ID)` after the DB insert. **Log and continue on enqueue failure** — don't fail the request, the user is already saved (comment why).
- Update `internal/router/router.go` and `cmd/api/main.go` — pass `*queue.Client` through.

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
  FROM golang:1.23-alpine AS build
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
- Structured request-logging middleware beyond Gin's default
- Graceful shutdown beyond Gin's defaults / asynq's built-in signal handling
