package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/log"
)

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Error("server panic recovered", "err", err)

		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
	})
}
