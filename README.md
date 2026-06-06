# go_clean_arch_sample

A small, deliberately-scoped Go service that shows how I structure a real
backend: clean architecture with dependencies pointing inward, a Gin HTTP API,
an embedded [asynq](https://github.com/hibiken/asynq) (Redis) worker, Postgres
via GORM, and a two-tier test strategy (fakes for units, real containers for
integration).

It implements one feature — user create / get / list, with a welcome-email
task enqueued on creation — and stops there on purpose. The point is the
**shape and the judgment calls**, not the feature count.

## What it's meant to demonstrate

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

## Key decisions (the interview conversation)

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
mise run cli -- migrate     # apply migrations
mise run server             # API + embedded worker on :8081
```

`mise run infra:dev` also brings up the asynqmon queue UI on
<http://localhost:8082>.

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

## Stack

Go 1.25 · Gin · GORM + pgx (Postgres) · asynq (Redis) · gormigrate · cobra ·
slog · testify · dockertest · golangci-lint v2 · mise.
