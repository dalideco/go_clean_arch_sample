package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/dali/go_clean_arch_sample/internal/domain"
	"github.com/dali/go_clean_arch_sample/internal/log"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrUserEmailTaken = errors.New("user email taken")
)

type UserRepository interface {
	List(ctx context.Context) ([]domain.User, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Create(ctx context.Context, u *domain.User) error
}

// WelcomeEmailEnqueuer is the consumer-defined interface for queuing a
// welcome-email task after user creation. The queue adapter
// (internal/queue) implements it; tests may pass a fake. The use case knows
// nothing about asynq, Redis, or the task payload format.
type WelcomeEmailEnqueuer interface {
	EnqueueWelcomeEmail(ctx context.Context, userID uuid.UUID) error
}

type UserUseCase struct {
	repo    UserRepository
	welcome WelcomeEmailEnqueuer
}

func NewUserUseCase(repo UserRepository, welcome WelcomeEmailEnqueuer) *UserUseCase {
	return &UserUseCase{repo: repo, welcome: welcome}
}

func (s *UserUseCase) List(ctx context.Context) ([]domain.User, error) {
	return s.repo.List(ctx)
}

func (s *UserUseCase) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.repo.Get(ctx, id)
}

func (s *UserUseCase) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.repo.GetByEmail(ctx, email)
}

func (s *UserUseCase) Create(ctx context.Context, email, name string) (*domain.User, error) {
	u, err := domain.NewUser(email, name)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	// Best-effort enqueue: the user is committed; a failed welcome-email
	// task is a notification miss, not a request failure. Log + continue.
	// If welcome email delivery becomes business-critical, this is the
	// seam to swap for an outbox pattern (write to DB tx, drain to queue).
	if err := s.welcome.EnqueueWelcomeEmail(ctx, u.ID); err != nil {
		log.Warn("welcome email enqueue failed", "err", err, "user_id", u.ID)
	}
	return u, nil
}
