package masterdata

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type DefectCode struct {
	ID          uuid.UUID  `json:"id"`
	StationID   *uuid.UUID `json:"stationId"`
	StationName *string    `json:"stationName"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Category    *string    `json:"category"`
	IsActive    bool       `json:"isActive"`
}

type defectInput struct {
	StationID *uuid.UUID `json:"stationId"` // 空值 = 全站通用
	Code      string     `json:"code" binding:"required,max=20"`
	Name      string     `json:"name" binding:"required,max=100"`
	Category  string     `json:"category" binding:"max=50"`
	IsActive  *bool      `json:"isActive"`
}

func (h *Handler) listDefectCodes(c *gin.Context) {
	rows, err := h.pool.Query(c, `
		SELECT d.id, d.station_id, s.name, d.code, d.name, d.category, d.is_active
		FROM defect_codes d LEFT JOIN work_stations s ON s.id = d.station_id
		WHERE d.factory_id = $1 ORDER BY s.sort_order NULLS FIRST, d.code`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[DefectCode])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// stationInFactory 確認站點屬於本工廠（nil 視為通用，直接通過）。
func (h *Handler) stationInFactory(c *gin.Context, stationID *uuid.UUID) bool {
	if stationID == nil {
		return true
	}
	var ok bool
	if err := h.pool.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM work_stations WHERE id = $1 AND factory_id = $2)`,
		*stationID, auth.FactoryID(c)).Scan(&ok); err != nil {
		httpx.DBError(c, err)
		return false
	}
	if !ok {
		httpx.Fail(c, httpx.Biz("站點不存在"))
	}
	return ok
}

func (h *Handler) createDefectCode(c *gin.Context) {
	var in defectInput
	if !httpx.Bind(c, &in) || !h.stationInFactory(c, in.StationID) {
		return
	}
	id := httpx.NewID()
	_, err := h.pool.Exec(c, `
		INSERT INTO defect_codes (id, factory_id, station_id, code, name, category, is_active)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
		id, auth.FactoryID(c), in.StationID, in.Code, in.Name, in.Category, active(in.IsActive))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) updateDefectCode(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in defectInput
	if !httpx.Bind(c, &in) || !h.stationInFactory(c, in.StationID) {
		return
	}
	tag, err := h.pool.Exec(c, `
		UPDATE defect_codes SET station_id = $3, code = $4, name = $5, category = NULLIF($6, ''), is_active = $7
		WHERE id = $1 AND factory_id = $2`,
		id, auth.FactoryID(c), in.StationID, in.Code, in.Name, in.Category, active(in.IsActive))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.DBError(c, pgx.ErrNoRows)
		return
	}
	c.Status(http.StatusNoContent)
}
