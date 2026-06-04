package api

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/middleware"
	"github.com/dali/go_clean_arch_sample/internal/adapter/http/response"
	"github.com/dali/go_clean_arch_sample/internal/adapter/http/router"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

func New(deps usecase.Deps) *gin.Engine {
	response.RegisterFieldNames()

	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)

	engine.Use(middleware.RequestLogger(), middleware.Recovery())
	engine.NoRoute(middleware.NotFound)

	router.Register(engine, deps)

	return engine
}
