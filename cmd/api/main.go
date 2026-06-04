package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/api"
	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/log"
	"github.com/dali/go_clean_arch_sample/internal/queue"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

func main() {
	cfg := config.Load()
	log.Setup(cfg.LogFormat, cfg.LogLevel)

	db := startDbConnection(cfg)
	defer closeDB(db)
	repos := postgres.NewRepositories(db)

	queueClient := queue.New(cfg)
	defer func() { _ = queueClient.Close() }()
	log.Info("queue client opened", "redis_addr", cfg.RedisAddr)

	// The worker runs embedded in this process — this app never consumes
	// tasks without also producing them, so there's no standalone worker
	// binary. Started in the background and drained in the shutdown sequence
	// below, after the HTTP server stops accepting new work.
	worker := queue.NewServer(cfg, repos)
	if err := worker.Start(); err != nil {
		// Fast-fail before the HTTP listener starts; deferred Close/closeDB
		// won't run but the process is exiting anyway.
		log.Fatal("worker start failed", "err", err) //nolint:gocritic // exitAfterDefer: intentional fast-fail path
	}
	log.Info("embedded worker started")

	deps := usecase.Deps{
		Repos:     repos,
		Producers: queue.NewProducers(queueClient),
	}

	gin.DefaultWriter = log.Writer(log.LevelInfo)
	gin.DefaultErrorWriter = log.Writer(log.LevelError)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           api.New(deps),
		ReadHeaderTimeout: 5 * time.Second, // Slowloris mitigation; full request timeout lives in Step 12 middleware.
	}

	serverErr := startServer(srv)
	shutdown, stopNotify := notifyShutdown()
	defer stopNotify()

	select {
	case err := <-serverErr:
		// Defers above (stopNotify, closeDB) won't run on log.Fatal, but
		// the process is exiting anyway and the OS reclaims their resources.
		log.Fatal("server failed", "err", err) //nolint:gocritic // exitAfterDefer: intentional fast-fail path
	case sig := <-shutdown:
		log.Info("shutdown signal received, draining", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer cancel()

	// Stop accepting HTTP first (no new tasks get enqueued), then drain the
	// worker so in-flight tasks finish.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	worker.Shutdown()
	log.Info("server and worker stopped")
}

func startDbConnection(cfg *config.Config) *gorm.DB {
	db, err := postgres.New(cfg)
	if err != nil {
		log.Fatal("db connect failed", "err", err)
	}
	log.Info("db connected", "host", cfg.DBHost, "name", cfg.DBName)
	return db
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func startServer(srv *http.Server) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		log.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	return errCh
}

func notifyShutdown() (<-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	return sigCh, func() { signal.Stop(sigCh) }
}
