package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/dali/go_clean_arch_sample/internal/log"
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

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration

	RedisAddr     string
	RedisPassword string
}

func (c *Config) DatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
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

		DBHost:     mustGetenv("DB_HOST"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBUser:     mustGetenv("DB_USER"),
		DBPassword: mustGetenv("DB_PASSWORD"),
		DBName:     mustGetenv("DB_NAME"),
		DBSSLMode:  getenv("DB_SSL_MODE", "require"),

		DBMaxOpenConns:    25,
		DBMaxIdleConns:    5,
		DBConnMaxLifetime: 5 * time.Minute,

		RedisAddr:     mustGetenv("REDIS_ADDR"),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
	}
}

func (c *Config) IsProd() bool { return c.Env == EnvProd }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func mustGetenv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatal("required env var not set", "key", key)
	}
	return v
}
