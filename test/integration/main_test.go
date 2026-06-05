// Package integration holds full-stack tests that wire the real HTTP API,
// Postgres, and Redis together via dockertest — the slice that unit tests
// (which fake the repository/queue boundaries) deliberately don't cover.
package integration

import (
	"context"
	"flag"
	"log"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/testsupport"
)

// testCfg is the shared harness: a config pointed at throwaway Postgres +
// Redis containers. nil means skip (no Docker daemon, or running under
// -short).
var testCfg *config.Config

func TestMain(m *testing.M) {
	// testing.Short() panics if flags haven't been parsed yet.
	testing.Init()
	flag.Parse()

	// Skips under -short; fatal if Docker is required but unreachable.
	pool := testsupport.RequirePool()
	if pool == nil {
		os.Exit(m.Run()) // -short: integration tests self-skip via reset(t)
	}

	cfg := testsupport.BaseConfig()
	pg, err := testsupport.StartPostgres(pool, cfg)
	if err != nil {
		log.Fatalf("could not start postgres container: %v", err)
	}
	rd, err := testsupport.StartRedis(pool, cfg)
	if err != nil {
		testsupport.Purge(pool, pg)
		log.Fatalf("could not start redis container: %v", err)
	}

	if err := postgres.Migrate(cfg); err != nil {
		testsupport.Purge(pool, pg, rd)
		log.Fatalf("apply migrations: %v", err)
	}
	testCfg = cfg

	code := m.Run()
	testsupport.Purge(pool, pg, rd)
	os.Exit(code)
}

// requireIntegration skips a test when no live infra is available.
func requireIntegration(t *testing.T) {
	t.Helper()
	if testCfg == nil {
		t.Skip("skipping integration (no Docker / short mode)")
	}
}

// reset wipes shared-container state so each test starts on a clean slate:
// TRUNCATEs the Postgres tables and FLUSHes the Redis DB (asynq queues
// included). The integration analog of the postgres package's fresh(t), but
// covering both stores. Call it first in every integration test.
func reset(t *testing.T) {
	t.Helper()
	requireIntegration(t)

	db, err := postgres.New(testCfg)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	require.NoError(t, db.Exec(`TRUNCATE TABLE "users" RESTART IDENTITY CASCADE`).Error)

	rdb := redis.NewClient(&redis.Options{Addr: testCfg.RedisAddr, Password: testCfg.RedisPassword})
	defer func() { _ = rdb.Close() }()
	require.NoError(t, rdb.FlushDB(context.Background()).Err())
}
