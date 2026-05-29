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
- **Repositories are injected explicitly, never as a bundle.** A use case takes the *specific* repository interfaces it needs as named constructor params (`NewUserUseCase(users UserRepository, bankCreds BankCredentialRepository)`). **Never inject `usecase.Repositories` into a use case** — that bundle is a wiring-time aggregate (composition-root convenience) only; injecting it is a service locator that hides real dependencies and breaks testability. Multi-entity use cases just take more params; the per-feature `RegisterX` wires them, so `main.go` never changes.
- A use case is an *operation*, not an *actor*: "the user does it" doesn't force one `UserUseCase` to own everything user-related — split into another `XxxUseCase` when a type gets unwieldy. Whether a sub-entity needs its own repository is a modeling call (independent lifecycle / separate table / security boundary → own repo; otherwise persist it as part of the owning aggregate's repo).
- Atomic writes spanning repositories are a transaction (Unit of Work) concern — deferred; the pattern is a `WithTx(ctx, func(...) error)` callback at the adapter boundary, **not** a shared mutable bundle.

### `internal/adapter/repository/postgres/`
- The **only** package allowed to import `gorm.io/...`. `*gorm.DB` must not leak out of this package — not in function signatures, not in return types, not in struct fields visible outside.
- GORM model structs (e.g. `userModel` with `gorm:"..."` tags) live here, separate from `domain.User`. Convert via `toDomain()` / `fromDomain()` helpers.
- The repository implements the interface declared in `internal/usecase/`. Each method must:
  - Use `r.db.WithContext(ctx)` so request cancellation reaches the driver.
  - Convert `gorm.ErrRecordNotFound` → the corresponding domain sentinel (`usecase.ErrXxxNotFound`).
  - Return `domain.X`, never `userModel` or anything GORM-typed.
- Schema authority lives in versioned SQL migrations (Step 4: `golang-migrate`). **Temporary exception**: `postgres.AutoMigrate(cfg)` exists as a dev-only schema bring-up, called exclusively from `cmd/cli` via `db_setup` / `db_reset`. It must **not** be called from `cmd/api` or any boot path. When Step 4 lands, `postgres.AutoMigrate` is deleted along with its caller.

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

### `cmd/api/main.go`
- The process entrypoint, **not** the per-feature wiring root. Loads config, opens the DB, builds the repository bundle via `postgres.NewRepositories(db)`, hands the bundle to `api.New(repos)`, runs the HTTP server with graceful shutdown.
- Must **not** import `internal/usecase` or `internal/adapter/http/handler`. Its only adapter knowledge is the postgres package (`startDbConnection` → `*gorm.DB`, then `postgres.NewRepositories`). `*gorm.DB` is named here and nowhere else outside `internal/adapter/repository/postgres/` — this is the documented composition-root boundary.
- When a new adapter (Redis, Asynq, ...) lands, `main.go` opens it here and threads it through `api.New(...)`. Per-feature wiring still lives in the router package.
- DB connect is **fail-fast** (before HTTP listener starts), via `startDbConnection(cfg)`.

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
7. `internal/adapter/http/router/foos.go` — `RegisterFoos(r gin.IRouter, repos usecase.Repositories)` constructs `usecase.NewFooUseCase(repos.Foos)` → `handler.NewFoosHandler(svc)` and mounts the routes. Add one line `RegisterFoos(engine, repos)` to `router.Register` in `router.go`. **`main.go` does not change.**
8. Add a migration in the migrations dir (once Step 4 lands).

## Build / verify

- `go build ./... && go vet ./...` should be clean before declaring work done.
- `mise run server` starts the API. `mise run console` runs it under Delve. `mise run cli` invokes the operator CLI.
