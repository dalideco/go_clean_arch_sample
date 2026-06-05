// Package testsupport bootstraps throwaway Postgres/Redis containers for
// integration tests via dockertest. It lives in a regular (non-_test) package
// so multiple test packages share one harness instead of copy-pasting
// TestMain.
//
// It deliberately does NOT import the postgres adapter or the queue package —
// readiness is checked with pgx / go-redis directly — so the postgres
// package's own internal test can import this without an import cycle.
// Migrations stay with the caller (they're adapter-specific, not infra
// bootstrap).
package testsupport

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"

	"github.com/dali/go_clean_arch_sample/internal/config"
)

const readyTimeout = 60 * time.Second

// RequirePool returns a dockertest pool for an integration TestMain, applying
// the suite policy and returning nil only when the suite should be skipped:
//
//   - under -short (`mise run test:short`): skip — returns nil, no Docker needed.
//   - otherwise (`mise run test`): Docker is REQUIRED. An unreachable daemon is
//     fatal, so the run fails loudly instead of passing green without ever
//     exercising the containers.
//
// Call after testing.Init()/flag.Parse() so -short is observable.
func RequirePool() *dockertest.Pool {
	if testing.Short() {
		return nil
	}
	pool := newPool()
	if pool == nil {
		log.Fatal("docker daemon unreachable — integration tests require Docker " +
			"(run `mise run test:short` to skip them)")
	}
	return pool
}

// newPool connects to the local Docker daemon, trying DOCKER_HOST, the macOS
// Docker Desktop default, then dockertest auto-discovery (which finds
// /var/run/docker.sock on Linux / CI). Returns nil when Docker is unreachable.
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

// BaseConfig returns a *config.Config with test-env defaults (Env, connection
// pool sizes, log level) and empty container coordinates — fill those by
// passing it to StartPostgres / StartRedis.
func BaseConfig() *config.Config {
	return &config.Config{
		Env:               config.EnvTest,
		DBMaxOpenConns:    5,
		DBMaxIdleConns:    1,
		DBConnMaxLifetime: time.Minute,
		LogFormat:         "json",
		LogLevel:          "warn",
	}
}

// StartPostgres runs a throwaway postgres:16-alpine, fills cfg's DB* fields,
// and blocks until it accepts SQL connections. The caller runs migrations.
func StartPostgres(pool *dockertest.Pool, cfg *config.Config) (*dockertest.Resource, error) {
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
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
		return nil, err
	}

	cfg.DBHost = "localhost"
	cfg.DBPort = res.GetPort("5432/tcp")
	cfg.DBUser = "test"
	cfg.DBPassword = "test"
	cfg.DBName = "test"
	cfg.DBSSLMode = "disable"

	pool.MaxWait = readyTimeout
	if err := pool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, cfg.DatabaseDSN())
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()
		return conn.Ping(ctx)
	}); err != nil {
		_ = pool.Purge(res)
		return nil, err
	}
	return res, nil
}

// StartRedis runs a throwaway redis:7-alpine, sets cfg.RedisAddr, and blocks
// until it responds to PING.
func StartRedis(pool *dockertest.Pool, cfg *config.Config) (*dockertest.Resource, error) {
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redis",
		Tag:        "7-alpine",
	}, autoRemove)
	if err != nil {
		return nil, err
	}

	cfg.RedisAddr = "localhost:" + res.GetPort("6379/tcp")

	pool.MaxWait = readyTimeout
	if err := pool.Retry(func() error {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
		defer func() { _ = rdb.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return rdb.Ping(ctx).Err()
	}); err != nil {
		_ = pool.Purge(res)
		return nil, err
	}
	return res, nil
}

// Purge removes containers; errors are ignored since they're throwaway and
// AutoRemove is set anyway.
func Purge(pool *dockertest.Pool, resources ...*dockertest.Resource) {
	for _, r := range resources {
		_ = pool.Purge(r)
	}
}

// autoRemove tells Docker to reap the container when it stops.
func autoRemove(hc *docker.HostConfig) {
	hc.AutoRemove = true
	hc.RestartPolicy = docker.RestartPolicy{Name: "no"}
}
