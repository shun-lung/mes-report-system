package inventory

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type warehouseRow struct {
	ID          uuid.UUID  `json:"id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	StationID   *uuid.UUID `json:"stationId"`
	StationName *string    `json:"stationName"`
	IsActive    bool       `json:"isActive"`
	Items       int        `json:"items"` // 有庫存的料件數
}

type warehouseInput struct {
	Code      string     `json:"code" binding:"required,max=20"`
	Name      string     `json:"name" binding:"required,max=100"`
	Type      string     `json:"type" binding:"required,oneof=RAW WIP FG"`
	StationID *uuid.UUID `json:"stationId"`
	IsActive  *bool      `json:"isActive"`
}

func (h *Handler) listWarehouses(c *gin.Context) {
	rows, err := h.pool.Query(c, `
		SELECT w.id, w.code, w.name, w.type, w.station_id, s.name, w.is_active,
		       (SELECT count(*) FROM inventory_balances b WHERE b.warehouse_id = w.id AND b.qty <> 0)::int
		FROM warehouses w LEFT JOIN work_stations s ON s.id = w.station_id
		WHERE w.factory_id = $1 ORDER BY w.code`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[warehouseRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) saveWarehouse(c *gin.Context) {
	var in warehouseInput
	if !httpx.Bind(c, &in) {
		return
	}
	factoryID := auth.FactoryID(c)
	if in.StationID != nil {
		var ok bool
		if err := h.pool.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM work_stations WHERE id = $1 AND factory_id = $2)`,
			*in.StationID, factoryID).Scan(&ok); err != nil || !ok {
			httpx.Fail(c, httpx.Biz("站點不存在"))
			return
		}
	}
	active := in.IsActive == nil || *in.IsActive
	if c.Param("id") == "" {
		id := httpx.NewID()
		_, err := h.pool.Exec(c, `
			INSERT INTO warehouses (id, factory_id, code, name, type, station_id, is_active) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, factoryID, in.Code, in.Name, in.Type, in.StationID, active)
		if err != nil {
			httpx.DBError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": id})
		return
	}
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	tag, err := h.pool.Exec(c, `
		UPDATE warehouses SET code = $3, name = $4, type = $5, station_id = $6, is_active = $7
		WHERE id = $1 AND factory_id = $2`, id, factoryID, in.Code, in.Name, in.Type, in.StationID, active)
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
