package postgres

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/domain"
	"github.com/dali/go_clean_arch_sample/internal/testsupport"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// testCfg is set up by TestMain. nil means skip (no Docker daemon, or
// running under -short).
var testCfg *config.Config

func TestMain(m *testing.M) {
	// testing.Short() panics if flags haven't been parsed yet — TestMain
	// runs before m.Run() parses, so we have to do it ourselves first.
	testing.Init()
	flag.Parse()

	// Skips under -short; fatal if Docker is required but unreachable.
	pool := testsupport.RequirePool()
	if pool == nil {
		os.Exit(m.Run()) // -short: run the unit slices, skip integration
	}

	cfg := testsupport.BaseConfig()
	pg, err := testsupport.StartPostgres(pool, cfg)
	if err != nil {
		log.Fatalf("could not start postgres container: %v", err)
	}

	if err := Migrate(cfg); err != nil {
		testsupport.Purge(pool, pg)
		log.Fatalf("apply migrations: %v", err)
	}
	testCfg = cfg

	code := m.Run()
	testsupport.Purge(pool, pg)
	os.Exit(code)
}

// requireIntegration skips a test when no live DB is available (no Docker
// daemon, or -short). Lets local devs still run the unit slices clean.
func requireIntegration(t *testing.T) {
	t.Helper()
	if testCfg == nil {
		t.Skip("skipping postgres integration (no Docker / short mode)")
	}
}

// fresh opens a connection and TRUNCATEs `users` so each test starts on a
// clean slate without re-running migrations.
func fresh(t *testing.T) *UserRepository {
	t.Helper()
	requireIntegration(t)
	db, err := New(testCfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// CASCADE keeps future FK additions safe.
	require.NoError(t, db.Exec(`TRUNCATE TABLE "users" RESTART IDENTITY CASCADE`).Error)
	return NewUserRepository(db)
}

func TestUserRepository_CreateGet_RoundTrip(t *testing.T) {
	repo := fresh(t)
	ctx := context.Background()

	u, err := domain.NewUser("alice@example.com", "Alice")
	require.NoError(t, err)

	require.NoError(t, repo.Create(ctx, u))

	got, err := repo.Get(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, "Alice", got.Name)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestUserRepository_Get_NotFound(t *testing.T) {
	repo := fresh(t)
	_, err := repo.Get(context.Background(), uuid.New())
	assert.ErrorIs(t, err, usecase.ErrUserNotFound)
}

func TestUserRepository_GetByEmail(t *testing.T) {
	repo := fresh(t)
	ctx := context.Background()
	u, _ := domain.NewUser("alice@example.com", "Alice")
	require.NoError(t, repo.Create(ctx, u))

	got, err := repo.GetByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)

	_, err = repo.GetByEmail(ctx, "ghost@example.com")
	assert.ErrorIs(t, err, usecase.ErrUserNotFound)
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	// The most important seam: pgconn 23505 → usecase.ErrUserEmailTaken.
	// If this silently breaks, the HTTP layer goes from 409 to 500.
	repo := fresh(t)
	ctx := context.Background()

	first, _ := domain.NewUser("dup@example.com", "First")
	require.NoError(t, repo.Create(ctx, first))

	second, _ := domain.NewUser("dup@example.com", "Second")
	err := repo.Create(ctx, second)
	assert.ErrorIs(t, err, usecase.ErrUserEmailTaken)
}

func TestUserRepository_List(t *testing.T) {
	repo := fresh(t)
	ctx := context.Background()
	for _, e := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		u, _ := domain.NewUser(e, "X")
		require.NoError(t, repo.Create(ctx, u))
	}

	got, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 3)
}

func TestUserRepository_Get_ReconstituteFailure(t *testing.T) {
	// Drift case: simulate an out-of-band insert with an invariant-violating
	// row. Get must surface a *domain.ValidationError, not silently hand
	// back a User that lies about its own validity.
	repo := fresh(t)
	ctx := context.Background()

	db, err := New(testCfg)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	id := uuid.New()
	require.NoError(t, db.Exec(
		`INSERT INTO "users" ("id","email","name","created_at","updated_at") VALUES (?, ?, ?, ?, ?)`,
		id, "garbage", "X", time.Now(), time.Now(),
	).Error)

	_, err = repo.Get(ctx, id)
	require.Error(t, err)
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve, fmt.Sprintf("expected *domain.ValidationError, got %T", err))
	require.Len(t, ve.Violations, 1)
	assert.Equal(t, "email", ve.Violations[0].Field)
}
