package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/api"
	"github.com/dali/go_clean_arch_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_clean_arch_sample/internal/queue"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// TestCreateUser_PersistsAndQueuesWelcomeEmail exercises the whole stack the
// way production wires it: the real HTTP API → use case → Postgres (persist)
// → Redis (enqueue), then the embedded worker draining the welcome-email
// task. Unit tests fake the repo/queue boundaries; this is where the real
// adapters get exercised together.
func TestCreateUser_PersistsAndQueuesWelcomeEmail(t *testing.T) {
	reset(t) // clean DB + Redis; also skips when infra is unavailable

	db, err := postgres.New(testCfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repos := postgres.NewRepositories(db)

	client := queue.New(testCfg)
	t.Cleanup(func() { _ = client.Close() })

	// Build the real engine exactly as cmd/api does.
	engine := api.New(usecase.Deps{
		Repos:     repos,
		Producers: queue.NewProducers(client),
	})

	insp := asynq.NewInspector(queue.RedisOpt(testCfg))
	t.Cleanup(func() { _ = insp.Close() })

	// 1. Create a user through the real API.
	req := httptest.NewRequest(http.MethodPost, "/users",
		strings.NewReader(`{"email":"e2e@example.com","name":"E2E"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create succeeds: %s", rec.Body)

	// 2. It was persisted to Postgres (read back through the real repo).
	got, err := repos.Users.GetByEmail(context.Background(), "e2e@example.com")
	require.NoError(t, err)
	assert.Equal(t, "E2E", got.Name)

	// 3. A welcome-email task is scheduled — on the dedicated emails queue,
	//    not the default one.
	pending, err := insp.ListPendingTasks(queue.QueueEmails)
	require.NoError(t, err)
	require.Len(t, pending, 1, "exactly one task scheduled by the create")
	assert.Equal(t, queue.TypeWelcomeEmail, pending[0].Type)
	assert.Equal(t, queue.QueueEmails, pending[0].Queue, "lands on the emails queue")

	// 4. Drain the queue synchronously in this goroutine (no background
	//    server, no polling) — runs the welcome-email handler inline, the
	//    asynq analog of Oban.drain_queue.
	processed, err := queue.DrainQueue(context.Background(), testCfg, repos)
	require.NoError(t, err, "handler processes the task without error")
	assert.Equal(t, 1, processed, "exactly one task drained")

	// The emails queue is now empty.
	info, err := insp.GetQueueInfo(queue.QueueEmails)
	require.NoError(t, err)
	assert.Zero(t, info.Pending, "no pending tasks left")
	assert.Zero(t, info.Active, "no active tasks left")
}
