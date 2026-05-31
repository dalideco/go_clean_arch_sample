package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/adapter/http/api"
	"github.com/dali/go_project_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
	"github.com/dali/go_project_sample/internal/queue"
	"github.com/dali/go_project_sample/internal/usecase"
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

	deps := usecase.Deps{
		Repos:     repos,
		Producers: queue.NewProducers(queueClient),
	}

	gin.DefaultWriter = log.Writer(log.LevelInfo)
	gin.DefaultErrorWriter = log.Writer(log.LevelError)

	srv := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: api.New(deps),
	}

	serverErr := startServer(srv)
	shutdown, stopNotify := notifyShutdown()
	defer stopNotify()

	select {
	case err := <-serverErr:
		log.Fatal("server failed", "err", err)
	case sig := <-shutdown:
		log.Info("shutdown signal received, draining", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		return
	}
	log.Info("server stopped")
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
