package org

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Register：secured 已登入；scoped 另帶工廠範圍與權限。
func (h *Handler) Register(secured, scoped *gin.RouterGroup) {
	admin := secured.Group("/factories", auth.RequireSuperAdmin())
	admin.GET("", h.listFactories)
	admin.POST("", h.createFactory)
	admin.PUT("/:id", h.updateFactory)

	scoped.GET("/shifts", h.listShifts)
	scoped.POST("/shifts", auth.Require("factory:edit"), h.createShift)
	scoped.PUT("/shifts/:id", auth.Require("factory:edit"), h.updateShift)
}

type Factory struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Timezone string    `json:"timezone"`
	IsActive bool      `json:"isActive"`
}

type factoryInput struct {
	Code     string `json:"code" binding:"required,max=10"`
	Name     string `json:"name" binding:"required,max=100"`
	Timezone string `json:"timezone" binding:"required"`
	IsActive *bool  `json:"isActive"`
}

func (in factoryInput) validate(c *gin.Context) bool {
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		httpx.Abort(c, http.StatusBadRequest, "時區錯誤，請使用 IANA 名稱，如 Asia/Taipei")
		return false
	}
	return true
}

func (h *Handler) listFactories(c *gin.Context) {
	rows, err := h.pool.Query(c, `SELECT id, code, name, timezone, is_active FROM factories ORDER BY code`)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Factory, error) {
		var f Factory
		err := r.Scan(&f.ID, &f.Code, &f.Name, &f.Timezone, &f.IsActive)
		return f, err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) createFactory(c *gin.Context) {
	var in factoryInput
	if !httpx.Bind(c, &in) || !in.validate(c) {
		return
	}
	f := Factory{ID: httpx.NewID(), Code: in.Code, Name: in.Name, Timezone: in.Timezone, IsActive: in.IsActive == nil || *in.IsActive}
	_, err := h.pool.Exec(c, `INSERT INTO factories (id, code, name, timezone, is_active) VALUES ($1, $2, $3, $4, $5)`,
		f.ID, f.Code, f.Name, f.Timezone, f.IsActive)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, f)
}

func (h *Handler) updateFactory(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in factoryInput
	if !httpx.Bind(c, &in) || !in.validate(c) {
		return
	}
	f := Factory{ID: id, Code: in.Code, Name: in.Name, Timezone: in.Timezone, IsActive: in.IsActive == nil || *in.IsActive}
	tag, err := h.pool.Exec(c, `
		UPDATE factories SET code = $2, name = $3, timezone = $4, is_active = $5, updated_at = now() WHERE id = $1`,
		f.ID, f.Code, f.Name, f.Timezone, f.IsActive)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.DBError(c, pgx.ErrNoRows)
		return
	}
	c.JSON(http.StatusOK, f)
}

type Shift struct {
	ID             uuid.UUID `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	StartTime      string    `json:"startTime"`
	EndTime        string    `json:"endTime"`
	WorkDateOffset int       `json:"workDateOffset"`
	SortOrder      int       `json:"sortOrder"`
	IsActive       bool      `json:"isActive"`
}

type shiftInput struct {
	Code           string `json:"code" binding:"required,max=10"`
	Name           string `json:"name" binding:"required,max=50"`
	StartTime      string `json:"startTime" binding:"required"`
	EndTime        string `json:"endTime" binding:"required"`
	WorkDateOffset int    `json:"workDateOffset" binding:"oneof=0 -1"`
	SortOrder      int    `json:"sortOrder"`
	IsActive       *bool  `json:"isActive"`
}

func (in shiftInput) validate(c *gin.Context) bool {
	for _, s := range []string{in.StartTime, in.EndTime} {
		if _, err := ParseClock(s); err != nil {
			httpx.Abort(c, http.StatusBadRequest, err.Error())
			return false
		}
	}
	return true
}

func (h *Handler) listShifts(c *gin.Context) {
	rows, err := h.pool.Query(c, `
		SELECT id, code, name, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI'),
		       work_date_offset, sort_order, is_active
		FROM shifts WHERE factory_id = $1 ORDER BY sort_order, start_time`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Shift, error) {
		var s Shift
		err := r.Scan(&s.ID, &s.Code, &s.Name, &s.StartTime, &s.EndTime, &s.WorkDateOffset, &s.SortOrder, &s.IsActive)
		return s, err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) createShift(c *gin.Context) {
	var in shiftInput
	if !httpx.Bind(c, &in) || !in.validate(c) {
		return
	}
	s := in.toShift(httpx.NewID())
	_, err := h.pool.Exec(c, `
		INSERT INTO shifts (id, factory_id, code, name, start_time, end_time, work_date_offset, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5::time, $6::time, $7, $8, $9)`,
		s.ID, auth.FactoryID(c), s.Code, s.Name, s.StartTime, s.EndTime, s.WorkDateOffset, s.SortOrder, s.IsActive)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, s)
}

func (h *Handler) updateShift(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in shiftInput
	if !httpx.Bind(c, &in) || !in.validate(c) {
		return
	}
	s := in.toShift(id)
	tag, err := h.pool.Exec(c, `
		UPDATE shifts SET code = $3, name = $4, start_time = $5::time, end_time = $6::time,
		       work_date_offset = $7, sort_order = $8, is_active = $9
		WHERE id = $1 AND factory_id = $2`,
		s.ID, auth.FactoryID(c), s.Code, s.Name, s.StartTime, s.EndTime, s.WorkDateOffset, s.SortOrder, s.IsActive)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.DBError(c, pgx.ErrNoRows)
		return
	}
	c.JSON(http.StatusOK, s)
}

func (in shiftInput) toShift(id uuid.UUID) Shift {
	return Shift{
		ID: id, Code: in.Code, Name: in.Name, StartTime: in.StartTime, EndTime: in.EndTime,
		WorkDateOffset: in.WorkDateOffset, SortOrder: in.SortOrder, IsActive: in.IsActive == nil || *in.IsActive,
	}
}
