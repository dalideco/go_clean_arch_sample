package config

import "time"

func devConfig() *Config {
	cfg := baseConfig()
	cfg.Env = EnvDev
	cfg.HTTPShutdownTimeout = 10 * time.Second
	cfg.LogFormat = "tint"
	cfg.LogLevel = "debug"

	cfg.DBMaxOpenConns = 10
	cfg.DBMaxIdleConns = 2
	return cfg
}
