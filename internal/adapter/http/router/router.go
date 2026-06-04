package router

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/handler"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// Register mounts all HTTP routes on engine. Add one line per feature —
// each feature's wiring lives in its own router/<feature>.go file.
func Register(engine *gin.Engine, deps usecase.Deps) {
	engine.GET("/health", handler.Health)

	RegisterUsers(engine, deps)
}
