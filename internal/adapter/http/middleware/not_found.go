package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/adapter/http/response"
)

func NotFound(c *gin.Context) {
	response.Error(c, http.StatusNotFound, "not_found")
}
