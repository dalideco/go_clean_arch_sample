package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dali/go_project_sample/internal/adapter/http/response"
	"github.com/dali/go_project_sample/internal/domain"
	"github.com/dali/go_project_sample/internal/usecase"
)

// Fakes are duplicated from internal/usecase/user_test.go on purpose:
// shared testfakes would create a circular import (testfakes → usecase →
// testfakes when usecase's own tests use them). When/if a third consumer
// appears, we'll promote to internal/usecase/testfakes and move the
// usecase tests to an external `usecase_test` package.

type fakeUserRepository struct {
	users     map[uuid.UUID]domain.User
	listErr   error
	getErr    error
	createErr error
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: map[uuid.UUID]domain.User{}}
}

func (r *fakeUserRepository) List(_ context.Context) ([]domain.User, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]domain.User, 0, len(r.users))
	for _, u := range r.users {
		out = append(out, u)
	}
	return out, nil
}

func (r *fakeUserRepository) Get(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	u, ok := r.users[id]
	if !ok {
		return nil, usecase.ErrUserNotFound
	}
	return &u, nil
}

func (r *fakeUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range r.users {
		if u.Email == email {
			return &u, nil
		}
	}
	return nil, usecase.ErrUserNotFound
}

func (r *fakeUserRepository) Create(_ context.Context, u *domain.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	for _, existing := range r.users {
		if existing.Email == u.Email {
			return usecase.ErrUserEmailTaken
		}
	}
	r.users[u.ID] = *u
	return nil
}

type fakeWelcomeEmailEnqueuer struct{}

func (fakeWelcomeEmailEnqueuer) EnqueueWelcomeEmail(_ context.Context, _ uuid.UUID) error {
	return nil
}

// newTestEngine wires a real *UserUseCase (with fakes) into a gin engine
// with the same routes the production router registers. This is the seam
// we rely on instead of the now-removed handler-side userUseCase interface.
func newTestEngine(t *testing.T) (*gin.Engine, *fakeUserRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	response.RegisterFieldNames() // idempotent

	repo := newFakeUserRepository()
	uc := usecase.NewUserUseCase(repo, fakeWelcomeEmailEnqueuer{})
	h := NewUsersHandler(uc)

	engine := gin.New()
	engine.GET("/users", h.List)
	engine.GET("/users/:id", h.Get)
	engine.POST("/users", h.Create)
	return engine, repo
}

func decodeBody(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(body.Bytes(), &out))
	return out
}

func post(t *testing.T, engine *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestCreate_HappyPath(t *testing.T) {
	engine, repo := newTestEngine(t)

	rec := post(t, engine, `{"email":"alice@example.com","name":"Alice"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, true, body["success"])
	user, ok := body["user"].(map[string]any)
	require.True(t, ok, "envelope has `user` object")
	assert.Equal(t, "alice@example.com", user["email"])
	assert.Equal(t, "Alice", user["name"])
	assert.NotEmpty(t, user["id"])
	assert.Len(t, repo.users, 1)
}

func TestCreate_DuplicateEmail(t *testing.T) {
	engine, _ := newTestEngine(t)
	post(t, engine, `{"email":"alice@example.com","name":"Alice"}`)

	rec := post(t, engine, `{"email":"alice@example.com","name":"Alice2"}`)

	assert.Equal(t, http.StatusConflict, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, false, body["success"])
	assert.Equal(t, "email_taken", body["error"])
}

func TestCreate_MalformedJSON(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := post(t, engine, `{bad json`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, false, body["success"])
	assert.Equal(t, "invalid_body", body["error"])
	// Details may be nil for non-validator errors (raw JSON parse fail).
	if d, ok := body["details"]; ok {
		assert.Nil(t, d)
	}
}

func TestCreate_EmptyBody_BindingDetails(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := post(t, engine, `{}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, false, body["success"])
	assert.Equal(t, "invalid_body", body["error"])

	details, ok := body["details"].([]any)
	require.True(t, ok, "details present")
	require.Len(t, details, 2, "both email and name reported")

	fields := map[string]string{}
	for _, d := range details {
		m := d.(map[string]any)
		fields[m["field"].(string)] = m["message"].(string)
	}
	assert.Equal(t, "is required", fields["email"])
	assert.Equal(t, "is required", fields["name"])
}

func TestCreate_BindingEmailMessage(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := post(t, engine, `{"email":"not-an-email","name":"X"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec.Body)
	details := body["details"].([]any)
	require.Len(t, details, 1)
	d := details[0].(map[string]any)
	assert.Equal(t, "email", d["field"])
	assert.Equal(t, "must be a valid email address", d["message"])
}

func TestCreate_RepoFailure_500(t *testing.T) {
	engine, repo := newTestEngine(t)
	repo.createErr = errors.New("db connection lost")

	rec := post(t, engine, `{"email":"alice@example.com","name":"Alice"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, false, body["success"])
	assert.Equal(t, "internal_error", body["error"])
}

func TestList_Happy(t *testing.T) {
	engine, repo := newTestEngine(t)
	repo.users[uuid.New()] = domain.User{ID: uuid.New(), Email: "a@example.com", Name: "A"}
	repo.users[uuid.New()] = domain.User{ID: uuid.New(), Email: "b@example.com", Name: "B"}

	rec := get(t, engine, "/users")

	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, true, body["success"])
	users, ok := body["users"].([]any)
	require.True(t, ok, "envelope has `users` array")
	assert.Len(t, users, 2)
}

func TestList_Empty(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := get(t, engine, "/users")

	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec.Body)
	users, ok := body["users"].([]any)
	require.True(t, ok)
	assert.Len(t, users, 0, "empty list, not null")
}

func TestList_RepoFailure_500(t *testing.T) {
	engine, repo := newTestEngine(t)
	repo.listErr = errors.New("db down")

	rec := get(t, engine, "/users")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGet_Happy(t *testing.T) {
	engine, repo := newTestEngine(t)
	id := uuid.New()
	repo.users[id] = domain.User{ID: id, Email: "alice@example.com", Name: "Alice"}

	rec := get(t, engine, "/users/"+id.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec.Body)
	user := body["user"].(map[string]any)
	assert.Equal(t, "alice@example.com", user["email"])
}

func TestGet_NotFound(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := get(t, engine, "/users/"+uuid.New().String())

	assert.Equal(t, http.StatusNotFound, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, "not_found", body["error"])
}

func TestGet_BadUUID(t *testing.T) {
	engine, _ := newTestEngine(t)
	rec := get(t, engine, "/users/not-a-uuid")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec.Body)
	assert.Equal(t, "invalid_id", body["error"])
}
