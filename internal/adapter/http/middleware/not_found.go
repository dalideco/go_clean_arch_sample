package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dali/go_project_sample/internal/adapter/http/httperr"
)

func NotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, httperr.Response{Error: "not_found"})
}
