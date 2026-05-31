// Command worker runs the asynq task server: it consumes tasks enqueued by
// the API (and any future producers) and routes them to handlers in
// internal/queue. asynq owns the SIGINT/SIGTERM handling and graceful
// drain — srv.Run blocks until the process is asked to stop.
package main

import (
	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/log"
	"github.com/dali/go_project_sample/internal/queue"
)

func main() {
	cfg := config.Load()
	log.Setup(cfg.LogFormat, cfg.LogLevel)

	db := startDbConnection(cfg)
	defer closeDB(db)
	repos := postgres.NewRepositories(db)

	srv := asynq.NewServer(queue.RedisOpt(cfg), asynq.Config{
		Concurrency: 10,
		Queues:      map[string]int{"default": 1},
		Logger:      queue.NewAsynqLogger(),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeWelcomeEmail, queue.NewWelcomeEmailHandler(repos.Users).Handle)

	log.Info("worker starting", "redis_addr", cfg.RedisAddr)
	if err := srv.Run(mux); err != nil {
		log.Fatal("worker failed", "err", err)
	}
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
