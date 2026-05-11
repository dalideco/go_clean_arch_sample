package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/dali/go_project_sample/internal/log"
)

type Env int

const (
	EnvDev Env = iota
	EnvProd
	EnvTest
)

func (e Env) String() string {
	switch e {
	case EnvDev:
		return "dev"
	case EnvProd:
		return "prod"
	case EnvTest:
		return "test"
	default:
		return fmt.Sprintf("Env(%d)", int(e))
	}
}

func parseEnv(s string) (Env, error) {
	switch s {
	case "", "dev":
		return EnvDev, nil
	case "prod":
		return EnvProd, nil
	case "test":
		return EnvTest, nil
	default:
		return 0, fmt.Errorf("invalid APP_ENV: %q (want dev|prod|test)", s)
	}
}

type Config struct {
	Env                 Env
	HTTPPort            string
	HTTPShutdownTimeout time.Duration

	LogFormat string
	LogLevel  string
}

func Load() *Config {
	_ = godotenv.Load()

	env, err := parseEnv(os.Getenv("APP_ENV"))
	if err != nil {
		log.Fatal(err.Error())
	}

	switch env {
	case EnvProd:
		return prodConfig()
	case EnvTest:
		return testConfig()
	default:
		return devConfig()
	}
}

func baseConfig() *Config {
	return &Config{
		HTTPPort:            getenv("HTTP_PORT", "8080"),
		HTTPShutdownTimeout: 30 * time.Second,
		LogFormat:           "json",
		LogLevel:            "info",
	}
}

func (c *Config) IsProd() bool { return c.Env == EnvProd }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

