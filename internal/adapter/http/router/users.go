package router

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/adapter/http/handler"
	"github.com/dali/go_project_sample/internal/usecase"
)

// RegisterUsers wires the users feature end-to-end and mounts its routes
// on r. This is the per-feature composition root: it owns the construction
// of use case → handler so that main.go does not grow per feature. The
// repository comes pre-built in the bundle, so this file is ORM-agnostic.
func RegisterUsers(r gin.IRouter, repos usecase.Repositories) {
	svc := usecase.NewUserUseCase(repos.Users)
	h := handler.NewUsersHandler(svc)

	r.GET("/users", h.List)
	r.GET("/users/:id", h.Get)
	r.POST("/users", h.Create)
}
