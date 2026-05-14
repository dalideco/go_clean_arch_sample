package service

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

type UserService struct {
	repo UserRepository
}

func NewUserService(repo UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) List(ctx context.Context) ([]domain.User, error) {
	return s.repo.List(ctx)
}

func (s *UserService) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.repo.Get(ctx, id)
}

func (s *UserService) Create(ctx context.Context, email, name string) (*domain.User, error) {
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
