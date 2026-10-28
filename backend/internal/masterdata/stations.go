package masterdata

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type Line struct {
	ID              uuid.UUID `json:"id"`
	StationID       uuid.UUID `json:"stationId"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	CapacityPerHour float64   `json:"capacityPerHour"`
	StdHeadcount    int       `json:"stdHeadcount"`
	IsActive        bool      `json:"isActive"`
}

type Station struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	IsActive  bool      `json:"isActive"`
	Lines     []Line    `json:"lines"`
}

type stationInput struct {
	Code      string `json:"code" binding:"required,max=20"`
	Name      string `json:"name" binding:"required,max=100"`
	SortOrder int    `json:"sortOrder"`
	IsActive  *bool  `json:"isActive"`
}

type lineInput struct {
	StationID       uuid.UUID `json:"stationId" binding:"required"`
	Code            string    `json:"code" binding:"required,max=20"`
	Name            string    `json:"name" binding:"required,max=100"`
	CapacityPerHour float64   `json:"capacityPerHour" binding:"gte=0"`
	StdHeadcount    int       `json:"stdHeadcount" binding:"gte=0"`
	IsActive        *bool     `json:"isActive"`
}

func (h *Handler) listStations(c *gin.Context) {
	factoryID := auth.FactoryID(c)
	rows, err := h.pool.Query(c, `
		SELECT id, code, name, sort_order, is_active FROM work_stations
		WHERE factory_id = $1 ORDER BY sort_order, code`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	stations, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Station, error) {
		s := &Station{Lines: []Line{}}
		err := r.Scan(&s.ID, &s.Code, &s.Name, &s.SortOrder, &s.IsActive)
		return s, err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	byID := map[uuid.UUID]*Station{}
	for _, s := range stations {
		byID[s.ID] = s
	}
	rows, err = h.pool.Query(c, `
		SELECT id, station_id, code, name, capacity_per_hour, std_headcount, is_active
		FROM production_lines WHERE factory_id = $1 ORDER BY code`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Line])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	for _, l := range lines {
		if s := byID[l.StationID]; s != nil {
			s.Lines = append(s.Lines, l)
		}
	}
	c.JSON(http.StatusOK, stations)
}

func (h *Handler) createStation(c *gin.Context) {
	var in stationInput
	if !httpx.Bind(c, &in) {
		return
	}
	id := httpx.NewID()
	_, err := h.pool.Exec(c, `
		INSERT INTO work_stations (id, factory_id, code, name, sort_order, is_active) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, auth.FactoryID(c), in.Code, in.Name, in.SortOrder, active(in.IsActive))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) updateStation(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in stationInput
	if !httpx.Bind(c, &in) {
		return
	}
	tag, err := h.pool.Exec(c, `
		UPDATE work_stations SET code = $3, name = $4, sort_order = $5, is_active = $6
		WHERE id = $1 AND factory_id = $2`,
		id, auth.FactoryID(c), in.Code, in.Name, in.SortOrder, active(in.IsActive))
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

func (h *Handler) createLine(c *gin.Context) {
	var in lineInput
	if !httpx.Bind(c, &in) {
		return
	}
	id := httpx.NewID()
	tag, err := h.pool.Exec(c, `
		INSERT INTO production_lines (id, factory_id, station_id, code, name, capacity_per_hour, std_headcount, is_active)
		SELECT $1, $2, s.id, $4, $5, $6, $7, $8 FROM work_stations s WHERE s.id = $3 AND s.factory_id = $2`,
		id, auth.FactoryID(c), in.StationID, in.Code, in.Name, in.CapacityPerHour, in.StdHeadcount, active(in.IsActive))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(c, httpx.Biz("站點不存在"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) updateLine(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in lineInput
	if !httpx.Bind(c, &in) {
		return
	}
	tag, err := h.pool.Exec(c, `
		UPDATE production_lines l SET station_id = s.id, code = $4, name = $5, capacity_per_hour = $6,
		       std_headcount = $7, is_active = $8
		FROM work_stations s
		WHERE l.id = $1 AND l.factory_id = $2 AND s.id = $3 AND s.factory_id = $2`,
		id, auth.FactoryID(c), in.StationID, in.Code, in.Name, in.CapacityPerHour, in.StdHeadcount, active(in.IsActive))
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
