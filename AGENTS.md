# Project conventions for AI agents

These are non-negotiable rules for any agent (Claude, Codex, etc.) editing this repo. They encode architectural decisions we've already made; deviating from them silently is a regression.

## Logging

- Use `internal/log` (slog-backed) for **all** application logging. Import it as `"github.com/dali/go_project_sample/internal/log"`.
- Available functions: `log.Debug`, `log.Info`, `log.Warn`, `log.Error`, `log.Fatal`. All take a `msg string` followed by structured key/value args: `log.Info("user created", "id", u.ID, "email", u.Email)`.
- **Do not** use `fmt.Println` / `fmt.Printf` / `fmt.Fprintln(os.Stderr, ...)` for logging. Do not import the stdlib `"log"` package.
- `fmt.Errorf("...: %w", err)` is fine — that builds an error value, it does not log.
- `fmt.Sprintf` is fine for building strings (e.g. DSNs, Stringer impls). The one place it bridges into the logger is the gorm logger in `internal/adapter/repository/postgres/gorm_logger.go`, which translates gorm's printf-style API into our structured logger. Don't introduce more bridges like that — write to `internal/log` directly.
- For third-party libraries that need an `io.Writer`, use `log.Writer(log.LevelInfo)` (this is how gin's `DefaultWriter` is wired in `cmd/api/main.go`).

## Clean architecture / layering

Strict dependency direction: `domain` ← `usecase` ← `adapter` ← `cmd`. Inner layers know nothing about outer layers.

### `internal/domain/`
- Plain Go structs only. **No** `gorm:"..."` tags. **No** `json:"..."` tags. **No** `binding:"..."` tags.
- Imports nothing from `internal/`. External imports limited to primitive helpers (e.g. `github.com/google/uuid`, stdlib `time`/`net/mail`).
- If you printed a domain file with no context, you should not be able to tell what ORM, transport, or framework the project uses.
- **Each entity has two factories, and is built only through them — never a struct literal outside `domain`:**
  - `NewXxx(...) (*Xxx, error)` — *creation*: validate inputs, mint identity (`uuid.New()`) and timestamps.
  - `ReconstituteXxx(id, ..., createdAt, updatedAt) (*Xxx, error)` — *rebuild from persistence*: run the **same** validation, restore the stored identity/timestamps. The repository's `toDomain` calls this and is therefore fallible.
  - Both share a private `validateXxx` helper that **accumulates all violations** into a `*domain.ValidationError` (one error listing every bad field), rather than returning on the first.
- **Factories enforce intrinsic, stable invariants only** (email *format*, non-empty name) — because they also gate reconstitution, a failure breaks reads. **Mutable policy** (allowlists, tier limits, anything that changes over time) belongs in the **use case**, never a factory; otherwise a policy change retroactively invalidates legitimately-stored rows.
- **Validation failures use `domain.ValidationError`** (`internal/domain/validation.go`) carrying `[]FieldViolation{Field, Message}` — the transport renders the details generically without knowing any specific field/rule. This is distinct from use-case policy sentinels (`usecase.ErrUserEmailTaken`) and converted-infrastructure sentinels (`usecase.ErrUserNotFound`). A `ReconstituteXxx` failure wraps a `ValidationError` but is treated as a data-integrity error (logged, 500-class, **not** a client 400) — **migrate data before deploying a stricter invariant.**

### `internal/usecase/` (application business rules — Clean Architecture "use cases")
- Imports `internal/domain` only. Never `internal/adapter/...`. Never `gorm.io/...`.
- **Declares repository interfaces** that the use case depends on (consumer-side dependency inversion). Example: `usecase.UserRepository` is declared in `internal/usecase/user.go`; the postgres package implements it.
- **Owns domain error sentinels** (e.g. `usecase.ErrUserNotFound`). Adapters convert infrastructure errors to these at the boundary.
- Business logic lives here, in `XxxUseCase` types. App-generated UUIDs (`uuid.New()`) happen here, not in the DB.
- **Repositories and producers are injected explicitly, never as a bundle.** A use case takes the *specific* repository / producer interfaces it needs as named constructor params (`NewUserUseCase(users UserRepository, welcome WelcomeEmailEnqueuer)`). **Never inject `usecase.Repositories` or `usecase.Producers` into a use case** — those bundles are wiring-time aggregates (composition-root convenience) only; injecting them is a service locator that hides real dependencies and breaks testability. Multi-entity / multi-event use cases just take more params; the per-feature `RegisterX` wires them, so `main.go` never changes.
- **Producer interfaces** for asynchronous side effects (enqueue a task, publish an event) live in `internal/usecase/` next to the use case that triggers them (e.g. `WelcomeEmailEnqueuer` in `user.go`). The queue adapter implements them; the use case knows nothing about asynq or Redis. Enqueue failures are **logged and tolerated** by default (the primary write is committed — a missed notification is not a request failure); swap for an outbox pattern if delivery becomes business-critical.
- A use case is an *operation*, not an *actor*: "the user does it" doesn't force one `UserUseCase` to own everything user-related — split into another `XxxUseCase` when a type gets unwieldy. Whether a sub-entity needs its own repository is a modeling call (independent lifecycle / separate table / security boundary → own repo; otherwise persist it as part of the owning aggregate's repo).
- Atomic writes spanning repositories are a transaction (Unit of Work) concern — deferred; the pattern is a `WithTx(ctx, func(...) error)` callback at the adapter boundary, **not** a shared mutable bundle.

### `internal/adapter/repository/postgres/`
- The **only** package allowed to import `gorm.io/...`. `*gorm.DB` must not leak out of this package — not in function signatures, not in return types, not in struct fields visible outside.
- GORM model structs (e.g. `userModel` with `gorm:"..."` tags) live here, separate from `domain.User`. Convert via `toDomain()` / `fromDomain()` helpers.
- The repository implements the interface declared in `internal/usecase/`. Each method must:
  - Use `r.db.WithContext(ctx)` so request cancellation reaches the driver.
  - Convert `gorm.ErrRecordNotFound` → the corresponding domain sentinel (`usecase.ErrXxxNotFound`).
  - Return `domain.X`, never `userModel` or anything GORM-typed.
- Schema authority lives in versioned migrations under `internal/adapter/repository/postgres/migrations/` (one file per migration, aggregated in `migrations.All`), applied via `postgres.Migrate(cfg)` from `cmd/cli` (`cli migrate`, idempotent, all envs; or as part of dev's `cli db_setup`). Each migration is a **self-contained** Go function using a *frozen* struct snapshot (never the live `userModel`) or raw `tx.Exec` — referencing the live model would silently change historical migrations as it evolves. **Do not call `db.AutoMigrate`** anywhere; the schema must come from the migration sequence.

### `internal/adapter/http/handler/`
- Imports `internal/usecase`, `internal/domain`, and `internal/adapter/http/response`. Never `internal/adapter/repository/...`, never `gorm.io/...`.
- **Owns wire DTOs** with `json:"..."` and `binding:"..."` tags (e.g. `createUserRequest`, `userResponse`). The domain struct stays infrastructure-free.
- Convert `domain.X` → response DTO via `toResponse()` before writing the response.
- **Never call `c.JSON` directly — always go through `internal/adapter/http/response`** so every body carries the uniform envelope (see below).
- Map errors to HTTP codes here, by *category* not by individual rule: `errors.Is(err, usecase.ErrUserNotFound)` → 404; policy conflicts (`usecase.ErrUserEmailTaken`) → 409; **any validation error** → 400 via `response.ValidationDetails(err)` (returns non-nil for both gin binding and `*domain.ValidationError`); unknown → 500 `internal_error`. Adding a new field-validation rule must **not** add a handler branch — the details ride inside the error.

### `internal/adapter/http/response/` (response envelope)
- The single place that writes JSON. Every response carries a top-level `success` bool.
  - Success (flat, payload under a named key): `response.OK(c, "users", out)` → `{"success":true,"users":[...]}`; `response.OK(c, "user", dto)` / `response.Created(c, "user", dto)` → `{"success":true,"user":{...}}`.
  - Error: `response.Error(c, status, code, details...)` → `{"success":false,"error":"<code>","details":[...]}` (`details` omitted when empty). `response.AbortError` is the same for middleware.
- `response.ValidationDetails(err)` is the **only** validation→details translator: it understands gin `validator.ValidationErrors` *and* `*domain.ValidationError`, returning `[]FieldError` (or nil if `err` isn't a validation error). `response.RegisterFieldNames()` (called once in `api.New`) makes binding errors report JSON field names.

### `internal/adapter/http/router/` (HTTP composition root)
- This is where features get wired up; it imports `internal/usecase`, `internal/adapter/http/handler`, and `gin`. It does **not** import `gorm.io/...` or the postgres adapter — repositories arrive pre-built in a `usecase.Repositories` bundle, so the wiring layer is ORM-agnostic.
- Each feature ships a `Register<Feature>(r gin.IRouter, repos usecase.Repositories)` function in its own file (`router/<feature>.go`). That function constructs use case → handler (the repo comes from the bundle) and mounts the feature's routes.
- `router.Register(engine, repos)` is the dispatcher — one line per feature. Adding a feature touches one new file plus one new line here. `main.go` never changes.

### `internal/queue/` (asynq adapter — producer + consumer)
- Single package that owns both sides of the asynq + Redis boundary: the **producer** (used by `cmd/api`) and the **consumer** handlers (used by `cmd/worker`). Producer↔consumer payload contracts also live here — there is no separate `tasks/` package; co-locating with the producer + handler keeps a feature in one file.
- The **only** package that imports `asynq`.
- **One file per task type**, holding all three sides of that task: payload struct + name constant + `(c *Client) EnqueueXxx(...)` producer method + `XxxHandler.Handle(...)` consumer. Adding a task = one new file, not three.
- Shared infra in their own files: `client.go` (`Client` wrapping `*asynq.Client`, `New(cfg)`, `Close()`, `NewProducers(c) usecase.Producers`, `RedisOpt(cfg)`) and `logger.go` (`NewAsynqLogger() asynq.Logger` — printf→`internal/log` bridge, same role gormLogger plays for GORM).
- Handlers depend on the *narrowest* use-case-layer interface they actually need — usually `usecase.UserRepository` directly for read-only lookups, not the full `UserUseCase` (the worker has no business carrying write-side producers it never calls).
- Handler error returns: wrap with `asynq.SkipRetry` (`fmt.Errorf("...: %w", asynq.SkipRetry)`) when retrying can't help (malformed payload, deleted user, etc.); return any other error to let asynq retry with backoff per its config.
- `RedisOpt(cfg)` lives here so `internal/config` stays asynq-free.

### `cmd/api/main.go`
- The process entrypoint, **not** the per-feature wiring root. Loads config, opens the DB + queue client, assembles the `usecase.Deps` bundle (with `Repos: postgres.NewRepositories(db)` and `Producers: queue.NewProducers(client)`), hands it to `api.New(deps)`, runs the HTTP server with graceful shutdown.
- May import `internal/usecase` **only to name `usecase.Deps`** when building the wiring bundle. Must **not** import `internal/adapter/http/handler` or construct any `XxxUseCase` / `XxxHandler` — those live in the per-feature `router/<feature>.go` files. Adapter knowledge stays minimal: `postgres` (`startDbConnection` → `*gorm.DB`, then `postgres.NewRepositories`) and `queue` (`queue.New`, `queue.NewProducers`). `*gorm.DB` is named here and nowhere else outside `internal/adapter/repository/postgres/`; the same boundary holds for asynq inside `internal/queue/`.
- When a new adapter type (cache, S3, ...) lands, add a field to `usecase.Deps`, open it here, set the field. Per-feature wiring still lives in the router package; `api.New` / `router.Register` / `RegisterXxx` signatures **do not grow** — that's the whole point of the bundle.
- DB connect is **fail-fast** (before HTTP listener starts), via `startDbConnection(cfg)`.

### `cmd/worker/main.go`
- Composition root for the asynq server. Loads config, opens the DB (workers read entities back from the DB), builds repositories, constructs `asynq.NewServer(queue.RedisOpt(cfg), asynq.Config{...})` with `worker.NewAsynqLogger()`, registers task handlers on an `asynq.ServeMux`, and calls `srv.Run(mux)`. asynq owns SIGINT/SIGTERM and graceful drain — no manual signal plumbing.
- Imports `internal/queue` (handlers + `RedisOpt` + `NewAsynqLogger`) and `internal/adapter/repository/postgres` (for repositories). The producer-side `Client` / `EnqueueXxx` methods inside `internal/queue` are linked in but dead-stripped — the worker only uses the consumer surface.

## Quick "is this allowed?" reference

| File location | May import `gorm.io/...`? | May have `gorm:` tags? | May have `json:` tags? |
|---|---|---|---|
| `internal/domain/*.go` | ❌ | ❌ | ❌ |
| `internal/usecase/*.go` | ❌ | ❌ | ❌ |
| `internal/adapter/repository/postgres/*.go` | ✅ | ✅ | ❌ |
| `internal/adapter/http/handler/*.go` | ❌ | ❌ | ✅ |
| `internal/adapter/http/router/*.go` | ❌ | ❌ | ❌ |
| `cmd/api/main.go` | only `startDbConnection` + `postgres.NewRepositories` | ❌ | ❌ |

If a change would flip any of these, push back — that's a layering violation.

## Adding a new entity (recipe)

1. `internal/domain/foo.go` — plain struct **plus `NewFoo` and `ReconstituteFoo` factories** sharing a private `validateFoo` helper for the entity's intrinsic invariants.
2. `internal/usecase/foo.go` — `FooRepository` interface, `ErrFooNotFound` sentinel, `FooUseCase` with methods.
3. `internal/adapter/repository/postgres/foo_model.go` — `fooModel` with GORM tags + `toDomain`/`fromDomain`.
4. `internal/adapter/repository/postgres/foo_repository.go` — implements `usecase.FooRepository`, uses `WithContext(ctx)`, converts `gorm.ErrRecordNotFound` → `usecase.ErrFooNotFound`.
5. `internal/adapter/http/handler/foos.go` — `FoosHandler` + `createFooRequest` / `fooResponse` DTOs, JSON tags here.
6. Add `Foos FooRepository` to `usecase.Repositories` (`internal/usecase/repositories.go`) and `Foos: NewFooRepository(db)` to `postgres.NewRepositories` (`internal/adapter/repository/postgres/repositories.go`).
7. `internal/adapter/http/router/foos.go` — `RegisterFoos(r gin.IRouter, deps usecase.Deps)` constructs `usecase.NewFooUseCase(deps.Repos.Foos, deps.Producers.WhateverEnqueuer)` → `handler.NewFoosHandler(uc)` and mounts the routes. Add one line `RegisterFoos(engine, deps)` to `router.Register` in `router.go`. **`main.go` does not change** — the `Deps` bundle absorbs new adapter types without growing signatures.
8. `internal/adapter/repository/postgres/migrations/<YYYYMMDDHHMMSS>_<desc>.go` (timestamp via `date +%Y%m%d%H%M%S`) — an `init()` that calls `register(&gormigrate.Migration{...})` with the matching `"<YYYYMMDDHHMMSS>_<desc>"` ID and `Migrate`/`Rollback` funcs (use a *frozen* local struct, not the live model). The set of migrations is whatever files are in the directory — no central manifest to touch.
9. **If the entity emits async events** (e.g. enqueues a task on create): declare the producer interface in `internal/usecase/foo.go` (e.g. `FooCreatedEnqueuer`), add the field to `usecase.Producers`, then create **one file** `internal/queue/foo_created.go` containing the payload struct + name constant + `(c *Client) EnqueueFooCreated(...)` method (satisfies the interface) + `FooCreatedHandler.Handle(...)` consumer. Register the consumer with `mux.HandleFunc(queue.TypeFooCreated, queue.NewFooCreatedHandler(repos.Foos).Handle)` in `cmd/worker/main.go`.
10. `internal/seeds/foos.go` (optional, dev only) — an `init()` that calls `register(seeds.Seeder{Name: "foos", Tables: []string{"foos"}, Run: ...})`. Write through the use case (`usecase.NewFooUseCase(repos.Foos, nopFooEnqueuer{}).Create(...)`), catch the entity's "already taken" sentinel for idempotency, never bypass into raw GORM.

## Seeds convention

- `internal/seeds/` holds dev/test seeders. Each entity gets one file that `init()`-registers a named `Seeder` (with the entity's `Tables`); no central manifest. Names must be unique within the package (collision panics at startup).
- Seeders write **through the use case** — same domain factory + adapter chain as production traffic.
- **Idempotency-by-pre-check:** each seeder *looks up* via the use case (e.g. `GetByEmail`) and only `Create`s on `ErrXxxNotFound`. Avoids a noisy failed INSERT on every re-seed; catches the unique-violation sentinel as belt-and-braces. No tracking table.
- `cli seed` is the entrypoint, dev-only. No args runs every seeder; `cli seed <name>` targets one; `cli seed --list` enumerates them; `cli seed --reset` truncates the targeted seeders' `Tables` (RESTART IDENTITY CASCADE) before running — destructive: wipes API-created data in those tables too.

## Build / verify

- `go build ./... && go vet ./...` should be clean before declaring work done.
- `mise run server` starts the API. `mise run console` runs it under Delve. `mise run cli` invokes the operator CLI.
