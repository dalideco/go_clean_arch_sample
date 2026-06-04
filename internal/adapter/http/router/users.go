package router

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/handler"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// RegisterUsers wires the users feature end-to-end and mounts its routes
// on r. This is the per-feature composition root: it owns the construction
// of use case → handler so that main.go does not grow per feature. Deps
// carries pre-built repository + producer bundles, so this file is
// ORM-agnostic and queue-tech-agnostic.
func RegisterUsers(r gin.IRouter, deps usecase.Deps) {
	uc := usecase.NewUserUseCase(deps.Repos.Users, deps.Producers.WelcomeEmail)
	h := handler.NewUsersHandler(uc)

	r.GET("/users", h.List)
	r.GET("/users/:id", h.Get)
	r.POST("/users", h.Create)
}
