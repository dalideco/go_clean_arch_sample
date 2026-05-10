package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/log"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration", time.Since(start),
			"client", c.ClientIP(),
		}

		switch status := c.Writer.Status(); {
		case status >= 500:
			log.Error("http request", attrs...)
		case status >= 400:
			log.Warn("http request", attrs...)
		default:
			log.Info("http request", attrs...)
		}
	}
}
