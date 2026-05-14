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

Strict dependency direction: `domain` ← `service` ← `adapter` ← `cmd`. Inner layers know nothing about outer layers.

### `internal/domain/`
- Plain Go structs only. **No** `gorm:"..."` tags. **No** `json:"..."` tags. **No** `binding:"..."` tags.
- Imports nothing from `internal/`. External imports limited to primitive helpers (e.g. `github.com/google/uuid`, stdlib `time`).
- If you printed a domain file with no context, you should not be able to tell what ORM, transport, or framework the project uses.

### `internal/service/`
- Imports `internal/domain` only. Never `internal/adapter/...`. Never `gorm.io/...`.
- **Declares repository interfaces** that the service depends on (consumer-side dependency inversion). Example: `service.UserRepository` is declared in `internal/service/user.go`; the postgres package implements it.
- **Owns domain error sentinels** (e.g. `service.ErrUserNotFound`). Adapters convert infrastructure errors to these at the boundary.
- Business logic lives here. App-generated UUIDs (`uuid.New()`) happen here, not in the DB.

### `internal/adapter/repository/postgres/`
- The **only** package allowed to import `gorm.io/...`. `*gorm.DB` must not leak out of this package — not in function signatures, not in return types, not in struct fields visible outside.
- GORM model structs (e.g. `userModel` with `gorm:"..."` tags) live here, separate from `domain.User`. Convert via `toDomain()` / `fromDomain()` helpers.
- The repository implements the interface declared in `internal/service/`. Each method must:
  - Use `r.db.WithContext(ctx)` so request cancellation reaches the driver.
  - Convert `gorm.ErrRecordNotFound` → the corresponding domain sentinel (`service.ErrXxxNotFound`).
  - Return `domain.X`, never `userModel` or anything GORM-typed.
- Schema authority lives in versioned SQL migrations (Step 4: `golang-migrate`). **Temporary exception**: `postgres.AutoMigrate(cfg)` exists as a dev-only schema bring-up, called exclusively from `cmd/cli` via `db_setup` / `db_reset`. It must **not** be called from `cmd/api` or any boot path. When Step 4 lands, `postgres.AutoMigrate` is deleted along with its caller.

### `internal/adapter/http/handler/`
- Imports `internal/service` and `internal/domain`. Never `internal/adapter/repository/...`, never `gorm.io/...`.
- **Owns wire DTOs** with `json:"..."` and `binding:"..."` tags (e.g. `createUserRequest`, `userResponse`). The domain struct stays infrastructure-free.
- Convert `domain.X` → response DTO via `toResponse()` before writing the response.
- Map service error sentinels to HTTP codes here: `errors.Is(err, service.ErrUserNotFound)` → 404. Unknown errors → 500 with `httperr.Response{Error: "internal_error"}`.

### `internal/adapter/http/router/` (HTTP composition root)
- This is where features get wired up; the package is allowed to import `gorm.io/gorm`, `internal/adapter/repository/postgres`, `internal/service`, and `internal/adapter/http/handler`.
- Each feature ships a `Register<Feature>(r gin.IRouter, db *gorm.DB)` function in its own file (`router/<feature>.go`). That function constructs repo → service → handler and mounts the feature's routes.
- `router.Register(engine, db)` is the dispatcher — one line per feature. Adding a feature touches one new file plus one new line here. `main.go` never changes.
- The `router` package only *constructs* things from `gorm.io/...`; it does not call GORM methods. Active GORM use is still confined to `internal/adapter/repository/postgres/`.

### `cmd/api/main.go`
- The process entrypoint, **not** the per-feature wiring root. Loads config, opens the DB, hands `*gorm.DB` to `api.New(db)`, runs the HTTP server with graceful shutdown.
- Must **not** import `internal/service` or `internal/adapter/http/handler`. Its only adapter knowledge is the postgres connection helper (`postgres.New` via `startDbConnection`).
- When a new adapter (Redis, Asynq, ...) lands, `main.go` opens it here and threads it through `api.New(...)`. Per-feature wiring still lives in the router package.
- DB connect is **fail-fast** (before HTTP listener starts), via `startDbConnection(cfg)`.

## Quick "is this allowed?" reference

| File location | May import `gorm.io/...`? | May have `gorm:` tags? | May have `json:` tags? |
|---|---|---|---|
| `internal/domain/*.go` | ❌ | ❌ | ❌ |
| `internal/service/*.go` | ❌ | ❌ | ❌ |
| `internal/adapter/repository/postgres/*.go` | ✅ | ✅ | ❌ |
| `internal/adapter/http/handler/*.go` | ❌ | ❌ | ✅ |
| `internal/adapter/http/router/*.go` | ✅ (construct only) | ❌ | ❌ |
| `cmd/api/main.go` | only the `postgres.*` constructors + threading `*gorm.DB` to `api.New` | ❌ | ❌ |

If a change would flip any of these, push back — that's a layering violation.

## Adding a new entity (recipe)

1. `internal/domain/foo.go` — plain struct.
2. `internal/service/foo.go` — `FooRepository` interface, `ErrFooNotFound` sentinel, `FooService` with methods.
3. `internal/adapter/repository/postgres/foo_model.go` — `fooModel` with GORM tags + `toDomain`/`fromDomain`.
4. `internal/adapter/repository/postgres/foo_repository.go` — implements `service.FooRepository`, uses `WithContext(ctx)`, converts `gorm.ErrRecordNotFound` → `service.ErrFooNotFound`.
5. `internal/adapter/http/handler/foos.go` — `FoosHandler` + `createFooRequest` / `fooResponse` DTOs, JSON tags here.
6. `internal/adapter/http/router/foos.go` — `RegisterFoos(r gin.IRouter, db *gorm.DB)` constructs `postgres.NewFooRepository(db)` → `service.NewFooService(repo)` → `handler.NewFoosHandler(svc)` and mounts the routes. Add one line `RegisterFoos(engine, db)` to `router.Register` in `router.go`. **`main.go` does not change.**
7. Add a migration in the migrations dir (once Step 4 lands).

## Build / verify

- `go build ./... && go vet ./...` should be clean before declaring work done.
- `mise run server` starts the API. `mise run console` runs it under Delve. `mise run cli` invokes the operator CLI.
