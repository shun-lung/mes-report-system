package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"mes/internal/httpx"
)

type Handler struct {
	store  *Store
	tokens *TokenIssuer
}

func NewHandler(store *Store, tokens *TokenIssuer) *Handler {
	return &Handler{store: store, tokens: tokens}
}

// Register：public 不需登入；secured 已套用 Authenticate。
func (h *Handler) Register(public, secured *gin.RouterGroup) {
	public.POST("/auth/login", h.login)
	secured.GET("/auth/me", h.me)
	secured.GET("/permissions", h.permissions)
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      User      `json:"user"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if !httpx.Bind(c, &req) {
		return
	}
	u, err := h.store.userByEmail(c, req.Email)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!u.isActive || bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(req.Password)) != nil)) {
		httpx.Abort(c, http.StatusUnauthorized, "帳號或密碼錯誤")
		return
	}
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	token, exp, err := h.tokens.Issue(u.ID, u.IsSuperAdmin)
	if err != nil {
		httpx.Abort(c, http.StatusInternalServerError, "系統錯誤")
		return
	}
	if err := h.store.touchLogin(c, u.ID); err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, loginResponse{Token: token, ExpiresAt: exp, User: u})
}

func (h *Handler) me(c *gin.Context) {
	u, err := h.store.userByID(c, UserID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if !u.isActive {
		httpx.Abort(c, http.StatusUnauthorized, "帳號已停用")
		return
	}
	factories, err := h.store.Factories(c, u.ID, u.IsSuperAdmin)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u, "factories": factories})
}

func (h *Handler) permissions(c *gin.Context) {
	c.JSON(http.StatusOK, Catalog)
}
