// Package queue is the single asynq + Redis adapter. It owns both the
// producer side (Client + Enqueue methods, used by the API binary) and the
// consumer side (handlers + asynq.Logger bridge, used by the worker
// binary). Tasks are grouped per-file: each task type's payload + name
// const + Client.EnqueueXxx + XxxHandler live together so a feature is one
// file, not three packages.
//
// The package replaces the earlier three-way split (internal/tasks +
// internal/adapter/queue + internal/adapter/worker). Producer and consumer
// were the same monolith on either side of one Redis — they never had to
// be independently swappable, so the three packages bought us nothing the
// linker doesn't already give us (dead-code elimination).
package queue

import (
	"github.com/hibiken/asynq"

	"github.com/dali/go_project_sample/internal/config"
	"github.com/dali/go_project_sample/internal/usecase"
)

// Client wraps *asynq.Client. The only place in the codebase that opens an
// asynq producer connection.
type Client struct {
	c *asynq.Client
}

// New opens a connection to Redis using cfg's RedisAddr / RedisPassword.
func New(cfg *config.Config) *Client {
	return &Client{c: asynq.NewClient(RedisOpt(cfg))}
}

// Close releases the underlying Redis connection. Idempotent.
func (c *Client) Close() error { return c.c.Close() }

// NewProducers exposes this client as the infrastructure-free
// usecase.Producers bundle. This is the single seam where "we use asynq"
// meets the rest of the app; swapping the queue means rewriting this
// package, not the wiring layer.
func NewProducers(c *Client) usecase.Producers {
	return usecase.Producers{
		WelcomeEmail: c,
	}
}

// RedisOpt builds the asynq client option from config. Lives here so
// internal/config stays asynq-free.
func RedisOpt(cfg *config.Config) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	}
}
