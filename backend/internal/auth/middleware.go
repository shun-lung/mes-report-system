package auth

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mes/internal/httpx"
)

const (
	ctxUserID      = "auth.userID"
	ctxSuperAdmin  = "auth.superAdmin"
	ctxFactoryID   = "auth.factoryID"
	ctxPermissions = "auth.permissions"
)

// Authenticate 驗證 Bearer JWT。
func Authenticate(tokens *TokenIssuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			httpx.Abort(c, http.StatusUnauthorized, "未登入")
			return
		}
		claims, err := tokens.Parse(raw)
		if err != nil {
			httpx.Abort(c, http.StatusUnauthorized, "登入已失效")
			return
		}
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			httpx.Abort(c, http.StatusUnauthorized, "登入已失效")
			return
		}
		c.Set(ctxUserID, id)
		c.Set(ctxSuperAdmin, claims.SuperAdmin)
		c.Next()
	}
}

// FactoryScope 依 X-Factory-ID 標頭確認工廠存取權並載入權限。
func FactoryScope(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		factoryID, err := uuid.Parse(c.GetHeader("X-Factory-ID"))
		if err != nil {
			httpx.Abort(c, http.StatusBadRequest, "缺少或錯誤的 X-Factory-ID")
			return
		}
		perms, member, err := store.FactoryPermissions(c, UserID(c), factoryID, IsSuperAdmin(c))
		if err != nil {
			log.Printf("load permissions: %v", err)
			httpx.Abort(c, http.StatusInternalServerError, "系統錯誤")
			return
		}
		if !member {
			httpx.Abort(c, http.StatusForbidden, "無此工廠存取權")
			return
		}
		c.Set(ctxFactoryID, factoryID)
		c.Set(ctxPermissions, perms)
		c.Next()
	}
}

// Require 需在 FactoryScope 之後使用。
func Require(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		perms, _ := c.Get(ctxPermissions)
		if m, ok := perms.(map[string]bool); !ok || !m[perm] {
			httpx.Abort(c, http.StatusForbidden, "權限不足："+perm)
			return
		}
		c.Next()
	}
}

// RequireAny 具備其中任一權限即可（需在 FactoryScope 之後使用）。
func RequireAny(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, p := range perms {
			if Can(c, p) {
				c.Next()
				return
			}
		}
		httpx.Abort(c, http.StatusForbidden, "權限不足："+strings.Join(perms, " 或 "))
	}
}

// Can 目前使用者在本工廠是否具備權限。
func Can(c *gin.Context, perm string) bool {
	perms, _ := c.Get(ctxPermissions)
	m, ok := perms.(map[string]bool)
	return ok && m[perm]
}

func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsSuperAdmin(c) {
			httpx.Abort(c, http.StatusForbidden, "需要總部管理員權限")
			return
		}
		c.Next()
	}
}

func UserID(c *gin.Context) uuid.UUID {
	return c.MustGet(ctxUserID).(uuid.UUID)
}

func IsSuperAdmin(c *gin.Context) bool {
	return c.GetBool(ctxSuperAdmin)
}

func FactoryID(c *gin.Context) uuid.UUID {
	return c.MustGet(ctxFactoryID).(uuid.UUID)
}
