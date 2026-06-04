package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_clean_arch_sample/internal/domain"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
	"github.com/dali/go_clean_arch_sample/internal/usecase/testfakes"
)

func newTestUseCase() (*usecase.UserUseCase, *testfakes.FakeUserRepository, *testfakes.FakeWelcomeEmailEnqueuer) {
	repo := testfakes.NewFakeUserRepository()
	enq := &testfakes.FakeWelcomeEmailEnqueuer{}
	return usecase.NewUserUseCase(repo, enq), repo, enq
}

func TestUserUseCase_Create_HappyPath(t *testing.T) {
	uc, repo, enq := newTestUseCase()

	u, err := uc.Create(context.Background(), "alice@example.com", "Alice")
	require.NoError(t, err)
	require.NotNil(t, u)

	assert.Equal(t, "alice@example.com", u.Email)
	assert.Equal(t, "Alice", u.Name)
	require.Len(t, repo.Users, 1, "user persisted")
	require.Len(t, enq.Calls, 1, "enqueue called exactly once")
	assert.Equal(t, u.ID, enq.Calls[0], "enqueued the right user ID")
}

func TestUserUseCase_Create_InvalidEmail(t *testing.T) {
	uc, repo, enq := newTestUseCase()

	u, err := uc.Create(context.Background(), "garbage", "Alice")
	assert.Nil(t, u)
	require.Error(t, err)

	var ve *domain.ValidationError
	require.True(t, errors.As(err, &ve), "domain validation error")
	assert.Len(t, repo.Users, 0, "no repo write on validation failure")
	assert.Len(t, enq.Calls, 0, "no enqueue on validation failure")
}

func TestUserUseCase_Create_RepoErrUserEmailTaken(t *testing.T) {
	uc, repo, enq := newTestUseCase()
	// First create succeeds.
	_, err := uc.Create(context.Background(), "alice@example.com", "Alice")
	require.NoError(t, err)
	require.Len(t, enq.Calls, 1)

	// Second create with same email → ErrUserEmailTaken from the fake repo.
	u, err := uc.Create(context.Background(), "alice@example.com", "Alice2")
	assert.Nil(t, u)
	assert.ErrorIs(t, err, usecase.ErrUserEmailTaken)
	assert.Len(t, repo.Users, 1, "no second row")
	assert.Len(t, enq.Calls, 1, "no enqueue on policy failure")
}

func TestUserUseCase_Create_EnqueueFailure_LogAndContinue(t *testing.T) {
	// This is the documented contract: a failed enqueue does NOT fail the
	// create. The user is already committed; the welcome email is a
	// best-effort notification.
	uc, repo, enq := newTestUseCase()
	enq.Err = errors.New("redis down")

	u, err := uc.Create(context.Background(), "alice@example.com", "Alice")
	require.NoError(t, err, "enqueue failure must not fail Create")
	require.NotNil(t, u)
	assert.Len(t, repo.Users, 1, "user is committed")
	assert.Len(t, enq.Calls, 1, "enqueue was attempted")
}

func TestUserUseCase_Get_DelegatesToRepo(t *testing.T) {
	uc, repo, _ := newTestUseCase()
	id := uuid.New()
	repo.Users[id] = domain.User{ID: id, Email: "alice@example.com", Name: "Alice"}

	u, err := uc.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, id, u.ID)

	_, err = uc.Get(context.Background(), uuid.New())
	assert.ErrorIs(t, err, usecase.ErrUserNotFound)
}

func TestUserUseCase_GetByEmail_DelegatesToRepo(t *testing.T) {
	uc, repo, _ := newTestUseCase()
	id := uuid.New()
	repo.Users[id] = domain.User{ID: id, Email: "alice@example.com", Name: "Alice"}

	u, err := uc.GetByEmail(context.Background(), "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", u.Email)

	_, err = uc.GetByEmail(context.Background(), "ghost@example.com")
	assert.ErrorIs(t, err, usecase.ErrUserNotFound)
}

func TestUserUseCase_List_DelegatesToRepo(t *testing.T) {
	uc, repo, _ := newTestUseCase()
	repo.Users[uuid.New()] = domain.User{Email: "a@example.com", Name: "A"}
	repo.Users[uuid.New()] = domain.User{Email: "b@example.com", Name: "B"}

	users, err := uc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, users, 2)
}
