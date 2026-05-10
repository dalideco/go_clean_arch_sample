package router

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/adapter/http/handler"
)

func Register(engine *gin.Engine) {
	engine.GET("/health", handler.Health)
}
