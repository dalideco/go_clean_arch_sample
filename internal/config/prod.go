package config

import "github.com/gin-gonic/gin"

func prodConfig() *Config {
	gin.SetMode(gin.ReleaseMode)

	cfg := baseConfig()
	cfg.Env = EnvProd
	return cfg
}
