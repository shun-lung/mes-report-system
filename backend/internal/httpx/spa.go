package httpx

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// SPA 提供前端建置檔；找不到的路徑回傳 index.html 交給前端路由，/api 下的未知路徑回 404。
func SPA(dir string) gin.HandlerFunc {
	index := filepath.Join(dir, "index.html")
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			Abort(c, http.StatusNotFound, "找不到此 API")
			return
		}
		file := filepath.Join(dir, filepath.Clean("/"+p))
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			if strings.HasPrefix(p, "/assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable") // 檔名含雜湊
			}
			c.File(file)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(index)
	}
}
