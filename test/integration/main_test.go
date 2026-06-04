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
	"time"

	"github.com/hibiken/asynq"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/queue"
)

// testCfg is the shared harness: a config pointed at throwaway Postgres +
// Redis containers. nil means skip (no Docker daemon, or running under
// -short).
var testCfg *config.Config

func TestMain(m *testing.M) {
	// testing.Short() panics if flags haven't been parsed yet.
	testing.Init()
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	pool := newPool()
	if pool == nil {
		log.Printf("docker daemon unreachable — skipping integration tests")
		os.Exit(m.Run())
	}

	pg, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
			"listen_addresses='*'",
		},
	}, autoRemove)
	if err != nil {
		log.Fatalf("could not start postgres container: %v", err)
	}

	rd, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redis",
		Tag:        "7-alpine",
	}, autoRemove)
	if err != nil {
		purge(pool, pg)
		log.Fatalf("could not start redis container: %v", err)
	}

	cfg := &config.Config{
		Env:               config.EnvTest,
		DBHost:            "localhost",
		DBPort:            pg.GetPort("5432/tcp"),
		DBUser:            "test",
		DBPassword:        "test",
		DBName:            "test",
		DBSSLMode:         "disable",
		DBMaxOpenConns:    5,
		DBMaxIdleConns:    1,
		DBConnMaxLifetime: time.Minute,
		RedisAddr:         "localhost:" + rd.GetPort("6379/tcp"),
		LogFormat:         "json",
		LogLevel:          "warn",
	}

	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error {
		db, err := postgres.New(cfg)
		if err != nil {
			return err
		}
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Ping()
	}); err != nil {
		purge(pool, pg, rd)
		log.Fatalf("postgres never became ready: %v", err)
	}
	if err := pool.Retry(func() error {
		c := asynq.NewClient(queue.RedisOpt(cfg))
		defer func() { _ = c.Close() }()
		return c.Ping()
	}); err != nil {
		purge(pool, pg, rd)
		log.Fatalf("redis never became ready: %v", err)
	}

	if err := postgres.Migrate(cfg); err != nil {
		purge(pool, pg, rd)
		log.Fatalf("apply migrations: %v", err)
	}
	testCfg = cfg

	code := m.Run()
	purge(pool, pg, rd)
	os.Exit(code)
}

// newPool connects to the local Docker daemon, trying DOCKER_HOST, the macOS
// Docker Desktop default, then dockertest auto-discovery (which finds
// /var/run/docker.sock on Linux / CI). Returns nil when unreachable.
func newPool() *dockertest.Pool {
	endpoints := []string{os.Getenv("DOCKER_HOST"), "unix://" + os.Getenv("HOME") + "/.docker/run/docker.sock", ""}
	for _, ep := range endpoints {
		p, err := dockertest.NewPool(ep)
		if err == nil && p.Client.Ping() == nil {
			return p
		}
	}
	return nil
}

// autoRemove tells Docker to reap the container when it stops.
func autoRemove(hc *docker.HostConfig) {
	hc.AutoRemove = true
	hc.RestartPolicy = docker.RestartPolicy{Name: "no"}
}

func purge(pool *dockertest.Pool, resources ...*dockertest.Resource) {
	for _, r := range resources {
		_ = pool.Purge(r)
	}
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
