package httpx

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func Abort(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// Bind 解析並驗證 JSON；失敗時已回應 400。
func Bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		Abort(c, http.StatusBadRequest, "資料格式錯誤："+err.Error())
		return false
	}
	return true
}

// ParamID 解析路由上的 :id；失敗時已回應 400。
func ParamID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		Abort(c, http.StatusBadRequest, "id 格式錯誤")
		return uuid.Nil, false
	}
	return id, true
}

// DBError 將常見資料庫錯誤轉為 HTTP 回應。
func DBError(c *gin.Context, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		Abort(c, http.StatusNotFound, "資料不存在")
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		Abort(c, http.StatusConflict, "資料重複")
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		Abort(c, http.StatusConflict, "資料被引用或關聯資料不存在")
	case errors.As(err, &pgErr) && pgErr.Code == "23514":
		Abort(c, http.StatusBadRequest, "資料不符合規則")
	default:
		log.Printf("db error: %v", err)
		Abort(c, http.StatusInternalServerError, "系統錯誤")
	}
}

func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// BizError 為業務規則錯誤（超報、跳站、狀態不符…），回應 422 與訊息。
type BizError struct{ Msg string }

func (e *BizError) Error() string { return e.Msg }

func Biz(format string, args ...any) error {
	return &BizError{Msg: fmt.Sprintf(format, args...)}
}

// Fail 依錯誤類型回應：業務錯誤 422，其餘交給 DBError。
func Fail(c *gin.Context, err error) {
	var biz *BizError
	if errors.As(err, &biz) {
		Abort(c, http.StatusUnprocessableEntity, biz.Msg)
		return
	}
	DBError(c, err)
}
