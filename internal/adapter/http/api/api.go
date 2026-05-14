package api

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/dali/go_project_sample/internal/adapter/http/middleware"
	"github.com/dali/go_project_sample/internal/adapter/http/router"
)

func New(db *gorm.DB) *gin.Engine {
	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)

	engine.Use(middleware.RequestLogger(), middleware.Recovery())
	engine.NoRoute(middleware.NotFound)

	router.Register(engine, db)

	return engine
}
