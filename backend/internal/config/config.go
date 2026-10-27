package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	JWTSecret     string
	JWTTTL        time.Duration
	AdminEmail    string // 首次啟動、資料庫無使用者時建立的總部管理員
	AdminPassword string
}

func Load() Config {
	ttl, err := time.ParseDuration(getenv("JWT_TTL", "12h"))
	if err != nil {
		ttl = 12 * time.Hour
	}
	return Config{
		HTTPAddr:      getenv("HTTP_ADDR", "127.0.0.1:8080"), // 預設只接受本機；部署時設 HTTP_ADDR=:8080
		DatabaseURL:   getenv("DATABASE_URL", "postgres://mes:mes@localhost:5433/mes?sslmode=disable"),
		JWTSecret:     getenv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:        ttl,
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@example.com"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin1234"),
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
