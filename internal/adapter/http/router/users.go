package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/adapter/http/handler"
	"github.com/dali/go_project_sample/internal/adapter/repository/postgres"
	"github.com/dali/go_project_sample/internal/service"
)

// RegisterUsers wires the users feature end-to-end and mounts its routes
// on r. This is the per-feature composition root: it owns the construction
// of repo → service → handler so that main.go does not grow per feature.
func RegisterUsers(r gin.IRouter, db *gorm.DB) {
	repo := postgres.NewUserRepository(db)
	svc := service.NewUserService(repo)
	h := handler.NewUsersHandler(svc)

	r.GET("/users", h.List)
	r.GET("/users/:id", h.Get)
	r.POST("/users", h.Create)
}
