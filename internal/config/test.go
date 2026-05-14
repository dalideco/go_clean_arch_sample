package config

import "time"

func testConfig() *Config {
	cfg := baseConfig()
	cfg.Env = EnvTest
	cfg.HTTPPort = getenv("HTTP_PORT", "0")
	cfg.HTTPShutdownTimeout = 5 * time.Second
	cfg.LogLevel = "warn"

	cfg.DBMaxOpenConns = 5
	cfg.DBMaxIdleConns = 1
	cfg.DBConnMaxLifetime = 1 * time.Minute
	return cfg
}
