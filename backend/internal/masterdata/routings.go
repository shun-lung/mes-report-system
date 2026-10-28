package masterdata

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type Routing struct {
	ID         uuid.UUID       `json:"id"`
	Version    int             `json:"version"`
	IsActive   bool            `json:"isActive"`
	Note       *string         `json:"note"`
	CreatedAt  time.Time       `json:"createdAt"`
	WorkOrders int             `json:"workOrders"` // 使用此版本的製令數
	Steps      json.RawMessage `json:"steps"`
}

type routingStepInput struct {
	StationID         uuid.UUID `json:"stationId" binding:"required"`
	StdSecondsPerUnit float64   `json:"stdSecondsPerUnit" binding:"gte=0"`
	IsReportPoint     *bool     `json:"isReportPoint"`
}

type routingInput struct {
	Note     string             `json:"note" binding:"max=500"`
	Activate bool               `json:"activate"`
	Steps    []routingStepInput `json:"steps" binding:"required,min=1,dive"`
}

func (h *Handler) listRoutings(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if _, ok := h.productInFactory(c, id); !ok {
		return
	}
	rows, err := h.pool.Query(c, `
		SELECT r.id, r.version, r.is_active, r.note, r.created_at,
		       (SELECT count(*) FROM work_orders w WHERE w.routing_id = r.id)::int,
		       COALESCE((SELECT json_agg(json_build_object('seq', rs.seq, 'stationId', rs.station_id, 'stationName', s.name,
		                    'stdSecondsPerUnit', rs.std_seconds_per_unit, 'isReportPoint', rs.is_report_point) ORDER BY rs.seq)
		                 FROM routing_steps rs JOIN work_stations s ON s.id = rs.station_id
		                 WHERE rs.routing_id = r.id), '[]')
		FROM routings r WHERE r.product_id = $1 ORDER BY r.version DESC`, id)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Routing])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// createRouting 建立新版途程。途程建立後不可修改（製令鎖定版本），要調整就建新版本。
func (h *Handler) createRouting(c *gin.Context) {
	productID, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in routingInput
	if !httpx.Bind(c, &in) {
		return
	}
	category, ok := h.productInFactory(c, productID)
	if !ok {
		return
	}
	if category != "FG" && category != "SFG" {
		httpx.Fail(c, httpx.Biz("只有成品或半成品需要途程"))
		return
	}
	// 掃描時以「線別所屬站點」找到途程站序，同一站點只能出現一次
	seen := map[uuid.UUID]bool{}
	for _, s := range in.Steps {
		if seen[s.StationID] {
			httpx.Fail(c, httpx.Biz("同一站點在途程中只能出現一次"))
			return
		}
		seen[s.StationID] = true
		if !h.stationInFactory(c, &s.StationID) {
			return
		}
	}

	factoryID := auth.FactoryID(c)
	id := httpx.NewID()
	var version int
	err := pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		// 鎖住產品列，避免同時建立兩個相同版本號
		if _, err := tx.Exec(c, `SELECT 1 FROM products WHERE id = $1 FOR UPDATE`, productID); err != nil {
			return err
		}
		if err := tx.QueryRow(c, `SELECT COALESCE(max(version), 0) + 1 FROM routings WHERE product_id = $1`,
			productID).Scan(&version); err != nil {
			return err
		}
		if in.Activate {
			if _, err := tx.Exec(c, `UPDATE routings SET is_active = false WHERE product_id = $1`, productID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(c, `
			INSERT INTO routings (id, factory_id, product_id, version, is_active, note) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
			id, factoryID, productID, version, in.Activate, in.Note); err != nil {
			return err
		}
		for i, s := range in.Steps {
			if _, err := tx.Exec(c, `
				INSERT INTO routing_steps (id, routing_id, seq, station_id, std_seconds_per_unit, is_report_point)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				httpx.NewID(), id, (i+1)*10, s.StationID, s.StdSecondsPerUnit, active(s.IsReportPoint)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "version": version})
}

// activateRouting 將指定版本設為生效；之後新開立的製令改用此版本。
func (h *Handler) activateRouting(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	err := pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		var productID uuid.UUID
		if err := tx.QueryRow(c, `SELECT product_id FROM routings WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			id, auth.FactoryID(c)).Scan(&productID); err != nil {
			return err
		}
		if _, err := tx.Exec(c, `UPDATE routings SET is_active = false WHERE product_id = $1 AND id <> $2`, productID, id); err != nil {
			return err
		}
		_, err := tx.Exec(c, `UPDATE routings SET is_active = true WHERE id = $1`, id)
		return err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
