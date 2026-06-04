package queue

import (
	"context"
	"errors"
	"sort"

	"github.com/hibiken/asynq"

	"github.com/dali/go_clean_arch_sample/internal/config"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// drainPageSize bounds how many pending tasks DrainQueue pulls per round. The
// loop always re-reads page 1 (it deletes as it goes), so this is a batch
// size, not pagination.
const drainPageSize = 100

// Queue names. Each task type enqueues into exactly one of these (via
// asynq.Queue in its EnqueueXxx method); naming queues per concern lets us
// tune throughput/priority independently (e.g. emails shouldn't starve
// latency-sensitive work).
const (
	QueueEmails = "emails"
)

// queues is the single source of truth for which asynq queues this app
// processes and their relative weights (asynq allocates workers across queues
// by weight). NewServer feeds it to the server and DrainQueue iterates it, so
// the consumer and the test drainer can never drift from the producers.
// Adding a queue = one entry here + asynq.Queue(<name>) at the enqueue site.
var queues = map[string]int{
	QueueEmails: 1,
}

// Server is the consumer side of the queue: an asynq task server with every
// handler registered on a ServeMux. It runs embedded in the API process
// (cmd/api) rather than as a standalone binary — this app never runs the
// consumer without the producer, so a separate worker command would be
// indirection without a use case.
type Server struct {
	inner *asynq.Server
	mux   *asynq.ServeMux
}

// NewServer builds the task server and registers handlers. It takes the
// repositories bundle because handlers do read-only domain lookups (the
// welcome-email handler re-reads the user); the bundle names no
// infrastructure, so this stays the only asynq-aware wiring seam.
func NewServer(cfg *config.Config, repos usecase.Repositories) *Server {
	inner := asynq.NewServer(RedisOpt(cfg), asynq.Config{
		Concurrency: 10,
		Queues:      queues,
		Logger:      NewAsynqLogger(),
	})
	return &Server{inner: inner, mux: newMux(repos)}
}

// newMux is the single place task types are wired to their handlers — shared
// by the live Server and by DrainQueue so both run identical routing.
func newMux(repos usecase.Repositories) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeWelcomeEmail, NewWelcomeEmailHandler(repos.Users).Handle)
	return mux
}

// Start begins processing tasks in background goroutines and returns
// immediately. Pair with Shutdown for graceful drain. (asynq's own Run is
// just Start + wait-for-signal + Shutdown; we drive the lifecycle from
// cmd/api instead so the worker shares the API's shutdown sequence.)
func (s *Server) Start() error { return s.inner.Start(s.mux) }

// Shutdown stops fetching new tasks and blocks until in-flight tasks finish.
func (s *Server) Shutdown() { s.inner.Shutdown() }

// DrainQueue synchronously processes every currently-pending task across all
// configured queues in the calling goroutine, using the same handler routing
// the live Server uses, and removes each from its queue. It returns the count
// processed and stops at the first handler error (leaving that task pending).
//
// Fidelity is routing-only: unlike the live server, DrainQueue does NOT honor
// retries, asynq.SkipRetry, or backoff — a handler error just aborts the
// drain. That's the right contract for a test that wants tasks run exactly
// once, deterministically.
//
// This is the test seam — the asynq analog of Oban.drain_queue. In tests the
// Server is never Started (the consumer is "halted"), so enqueued tasks sit
// pending until drained here: inline, with no background goroutine and no
// time-based polling. Production uses Start/Shutdown.
func DrainQueue(ctx context.Context, cfg *config.Config, repos usecase.Repositories) (int, error) {
	mux := newMux(repos)
	insp := asynq.NewInspector(RedisOpt(cfg))
	defer func() { _ = insp.Close() }()

	// Drain queues in a stable order so a multi-queue drain is deterministic
	// run-to-run (map iteration order is randomized).
	names := make([]string, 0, len(queues))
	for qname := range queues {
		names = append(names, qname)
	}
	sort.Strings(names)

	processed := 0
	for _, qname := range names {
		for {
			pending, err := insp.ListPendingTasks(qname, asynq.PageSize(drainPageSize))
			if errors.Is(err, asynq.ErrQueueNotFound) {
				// Queue never had a task enqueued, so it doesn't exist in Redis
				// yet — nothing to drain. (asynq materializes a queue lazily on
				// first enqueue.)
				break
			}
			if err != nil {
				return processed, err
			}
			if len(pending) == 0 {
				break
			}
			for _, info := range pending {
				if err := mux.ProcessTask(ctx, asynq.NewTask(info.Type, info.Payload)); err != nil {
					return processed, err
				}
				if err := insp.DeleteTask(qname, info.ID); err != nil {
					return processed, err
				}
				processed++
			}
		}
	}
	return processed, nil
}
