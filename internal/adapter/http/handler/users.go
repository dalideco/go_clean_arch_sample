package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dali/go_project_sample/internal/adapter/http/httperr"
	"github.com/dali/go_project_sample/internal/domain"
	"github.com/dali/go_project_sample/internal/log"
	"github.com/dali/go_project_sample/internal/usecase"
)

// userUseCase is the handler-side view of UserUseCase. Declaring the
// interface here (consumer side) keeps the handler unbound from the
// concrete use-case struct so tests can inject a fake without touching DB.
// *usecase.UserUseCase satisfies this via structural typing.
type userUseCase interface {
	List(ctx context.Context) ([]domain.User, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.User, error)
	Create(ctx context.Context, email, name string) (*domain.User, error)
}

type UsersHandler struct {
	uc userUseCase
}

func NewUsersHandler(uc userUseCase) *UsersHandler {
	return &UsersHandler{uc: uc}
}

type createUserRequest struct {
	Email string `json:"email" binding:"required,email"`
	Name  string `json:"name"  binding:"required"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(u domain.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func (h *UsersHandler) List(c *gin.Context) {
	users, err := h.uc.List(c.Request.Context())
	if err != nil {
		log.Error("users: list failed", "err", err)
		c.JSON(http.StatusInternalServerError, httperr.Response{Error: "internal_error"})
		return
	}
	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = toResponse(u)
	}
	c.JSON(http.StatusOK, out)
}

func (h *UsersHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, httperr.Response{Error: "invalid_id"})
		return
	}
	u, err := h.uc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, usecase.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, httperr.Response{Error: "not_found"})
			return
		}
		log.Error("users: get failed", "err", err, "id", id)
		c.JSON(http.StatusInternalServerError, httperr.Response{Error: "internal_error"})
		return
	}
	c.JSON(http.StatusOK, toResponse(*u))
}

func (h *UsersHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, httperr.Response{Error: "invalid_body"})
		return
	}
	u, err := h.uc.Create(c.Request.Context(), req.Email, req.Name)
	if err != nil {
		if errors.Is(err, usecase.ErrUserEmailTaken) {
			c.JSON(http.StatusConflict, httperr.Response{Error: "email_taken"})
			return
		}
		log.Error("users: create failed", "err", err, "email", req.Email)
		c.JSON(http.StatusInternalServerError, httperr.Response{Error: "internal_error"})
		return
	}
	c.JSON(http.StatusCreated, toResponse(*u))
}
