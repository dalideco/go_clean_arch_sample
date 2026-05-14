package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/adapter/http/handler"
)

// Register mounts all HTTP routes on engine. Add one line per feature —
// each feature's wiring lives in its own router/<feature>.go file.
func Register(engine *gin.Engine, db *gorm.DB) {
	engine.GET("/health", handler.Health)

	RegisterUsers(engine, db)
}
