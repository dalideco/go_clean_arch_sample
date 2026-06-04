package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/response"
	"github.com/dali/go_clean_arch_sample/internal/log"
)

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Error("server panic recovered", "err", err)

		response.AbortError(c, http.StatusInternalServerError, "internal_error")
	})
}
