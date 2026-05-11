package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/adapter/http/api"
	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
)

func main() {
	cfg := config.Load()
	log.Setup(cfg.LogFormat, cfg.LogLevel)

	gin.DefaultWriter = log.Writer(log.LevelInfo)
	gin.DefaultErrorWriter = log.Writer(log.LevelError)

	srv := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: api.New(),
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
