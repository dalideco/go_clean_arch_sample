package config

import "time"

func testConfig() *Config {
	cfg := baseConfig()
	cfg.Env = EnvTest
	cfg.HTTPPort = getenv("HTTP_PORT", "0")
	cfg.HTTPShutdownTimeout = 5 * time.Second
	cfg.LogLevel = "warn"
	return cfg
}
