package main

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/log"
	"github.com/dali/go_project_sample/internal/middleware"
	"github.com/dali/go_project_sample/internal/router"
)

const port = "8080"

func main() {
	gin.DefaultWriter = log.Writer(log.LevelInfo)
	gin.DefaultErrorWriter = log.Writer(log.LevelError)

	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)

	engine.Use(middleware.RequestLogger(), middleware.Recovery())
	engine.NoRoute(middleware.NotFound)

	router.Register(engine)

	log.Info("starting server", "port", port)
	if err := engine.Run(":" + port); err != nil {
		log.Fatal("failed to start server", "err", err)
	}
}
