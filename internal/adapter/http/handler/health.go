package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/dali/go_clean_arch_sample/internal/adapter/http/response"
)

func Health(c *gin.Context) {
	response.OK(c, "status", "ok")
}
