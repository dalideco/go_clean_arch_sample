package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dali/go_project_sample/internal/adapter/http/response"
	"github.com/dali/go_project_sample/internal/domain"
	"github.com/dali/go_project_sample/internal/log"
	"github.com/dali/go_project_sample/internal/usecase"
)

type UsersHandler struct {
	uc *usecase.UserUseCase
}

func NewUsersHandler(uc *usecase.UserUseCase) *UsersHandler {
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
		response.Error(c, http.StatusInternalServerError, "internal_error")
		return
	}
	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = toResponse(u)
	}
	response.OK(c, "users", out)
}

func (h *UsersHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid_id")
		return
	}
	u, err := h.uc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, usecase.ErrUserNotFound) {
			response.Error(c, http.StatusNotFound, "not_found")
			return
		}
		log.Error("users: get failed", "err", err, "id", id)
		response.Error(c, http.StatusInternalServerError, "internal_error")
		return
	}
	response.OK(c, "user", toResponse(*u))
}

func (h *UsersHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid_body", response.ValidationDetails(err)...)
		return
	}
	u, err := h.uc.Create(c.Request.Context(), req.Email, req.Name)
	if err != nil {
		if errors.Is(err, usecase.ErrUserEmailTaken) {
			response.Error(c, http.StatusConflict, "email_taken")
			return
		}
		// Any validation error (currently from the domain) carries its own
		// field details — render them generically, no per-rule branches.
		if details := response.ValidationDetails(err); details != nil {
			response.Error(c, http.StatusBadRequest, "invalid_body", details...)
			return
		}
		log.Error("users: create failed", "err", err, "email", req.Email)
		response.Error(c, http.StatusInternalServerError, "internal_error")
		return
	}
	response.Created(c, "user", toResponse(*u))
}
