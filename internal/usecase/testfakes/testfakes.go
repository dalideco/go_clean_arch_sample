// Package testfakes holds hand-written, stateful fakes for the use-case
// boundary interfaces (UserRepository, WelcomeEmailEnqueuer). They live in a
// regular (non-_test) package so multiple test packages can share them
// without copy-paste.
//
// Because the fakes reference usecase sentinel errors, this package imports
// usecase. Consequently the usecase package's own tests must use the external
// `usecase_test` package to avoid an import cycle.
package testfakes

import (
	"context"

	"github.com/google/uuid"

	"github.com/dali/go_clean_arch_sample/internal/domain"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// FakeUserRepository is a minimal in-memory usecase.UserRepository. Per-method
// error hooks let tests force boundary failures (e.g. repo returning an error
// to exercise the 500 path).
type FakeUserRepository struct {
	Users map[uuid.UUID]domain.User

	ListErr       error
	GetErr        error
	GetByEmailErr error
	CreateErr     error
}

func NewFakeUserRepository() *FakeUserRepository {
	return &FakeUserRepository{Users: map[uuid.UUID]domain.User{}}
}

func (r *FakeUserRepository) List(_ context.Context) ([]domain.User, error) {
	if r.ListErr != nil {
		return nil, r.ListErr
	}
	out := make([]domain.User, 0, len(r.Users))
	for _, u := range r.Users {
		out = append(out, u)
	}
	return out, nil
}

func (r *FakeUserRepository) Get(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if r.GetErr != nil {
		return nil, r.GetErr
	}
	u, ok := r.Users[id]
	if !ok {
		return nil, usecase.ErrUserNotFound
	}
	return &u, nil
}

func (r *FakeUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if r.GetByEmailErr != nil {
		return nil, r.GetByEmailErr
	}
	for _, u := range r.Users {
		if u.Email == email {
			return &u, nil
		}
	}
	return nil, usecase.ErrUserNotFound
}

func (r *FakeUserRepository) Create(_ context.Context, u *domain.User) error {
	if r.CreateErr != nil {
		return r.CreateErr
	}
	for _, existing := range r.Users {
		if existing.Email == u.Email {
			return usecase.ErrUserEmailTaken
		}
	}
	r.Users[u.ID] = *u
	return nil
}

// FakeWelcomeEmailEnqueuer records every call's user ID; the settable Err lets
// tests force enqueue failures to prove "log + continue".
type FakeWelcomeEmailEnqueuer struct {
	Calls []uuid.UUID
	Err   error
}

func (f *FakeWelcomeEmailEnqueuer) EnqueueWelcomeEmail(_ context.Context, userID uuid.UUID) error {
	f.Calls = append(f.Calls, userID)
	return f.Err
}
