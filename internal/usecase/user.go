package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dali/go_project_sample/internal/domain"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrUserEmailTaken = errors.New("user email taken")
)

type UserRepository interface {
	List(ctx context.Context) ([]domain.User, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.User, error)
	Create(ctx context.Context, u *domain.User) error
}

type UserUseCase struct {
	repo UserRepository
}

func NewUserUseCase(repo UserRepository) *UserUseCase {
	return &UserUseCase{repo: repo}
}

func (s *UserUseCase) List(ctx context.Context) ([]domain.User, error) {
	return s.repo.List(ctx)
}

func (s *UserUseCase) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.repo.Get(ctx, id)
}

func (s *UserUseCase) Create(ctx context.Context, email, name string) (*domain.User, error) {
	now := time.Now()
	u := &domain.User{
		ID:        uuid.New(),
		Email:     email,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}
