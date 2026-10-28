package config

import (
	"bufio"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	JWTSecret     string
	JWTTTL        time.Duration
	AdminEmail    string // 首次啟動、資料庫無使用者時建立的總部管理員
	AdminPassword string
	DemoPassword  string // cmd/demo 建立生管、倉管、組長帳號用
	StaticDir     string // 前端建置目錄；設定後由 API 一併提供網頁
}

// Load 讀取設定；ENV_FILE 可指定設定檔（預設 .env），例如 ENV_FILE=.env.supabase 改連雲端資料庫。
func Load() Config {
	loadDotEnv(getenv("ENV_FILE", ".env"))
	ttl, err := time.ParseDuration(getenv("JWT_TTL", "12h"))
	if err != nil {
		ttl = 12 * time.Hour
	}
	return Config{
		HTTPAddr:      httpAddr(),
		DatabaseURL:   getenv("DATABASE_URL", DefaultDatabaseURL),
		JWTSecret:     getenv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:        ttl,
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@example.com"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin1234"),
		DemoPassword:  getenv("DEMO_PASSWORD", "Demo1234!"),
		StaticDir:     getenv("STATIC_DIR", ""),
	}
}

// httpAddr：HTTP_ADDR 優先；雲端主機（如 Render）只給 PORT 時監聽所有介面；否則只接受本機連線。
func httpAddr() string {
	if v := getenv("HTTP_ADDR", ""); v != "" {
		return v
	}
	if port := getenv("PORT", ""); port != "" {
		return ":" + port
	}
	return "127.0.0.1:8080"
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// DefaultDatabaseURL 為本機開發用的 Docker PostgreSQL。
const DefaultDatabaseURL = "postgres://mes:mes@localhost:5433/mes?sslmode=disable"

// loadDotEnv 將設定檔載入環境變數；已存在的環境變數優先。
func loadDotEnv(path string) {
	values, _ := ReadEnvFile(path)
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

// ReadEnvFile 讀取 KEY=VALUE 格式的設定檔，忽略空行與 # 註解；檔案不存在時回傳錯誤。
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values, sc.Err()
}
