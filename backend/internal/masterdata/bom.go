package masterdata

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type BOMLine struct {
	ID              uuid.UUID       `json:"id"`
	ChildProductID  uuid.UUID       `json:"childProductId"`
	ItemNo          string          `json:"itemNo"`
	ItemName        string          `json:"itemName"`
	Unit            string          `json:"unit"`
	Category        string          `json:"category"`
	MaterialType    string          `json:"materialType"`
	QtyPer          float64         `json:"qtyPer"`
	ScrapRate       float64         `json:"scrapRate"`
	DeductTiming    *string         `json:"deductTiming"`          // NULL = 依料件預設
	EffectiveTiming string          `json:"effectiveDeductTiming"` // 實際採用
	StationID       *uuid.UUID      `json:"consumeStationId"`
	StationName     *string         `json:"consumeStationName"`
	SortOrder       int             `json:"sortOrder"`
	Substitutes     json.RawMessage `json:"substitutes"`
}

type bomInput struct {
	ParentProductID  uuid.UUID  `json:"parentProductId"`
	ChildProductID   uuid.UUID  `json:"childProductId"`
	MaterialType     string     `json:"materialType" binding:"required,oneof=MAIN AUX"`
	QtyPer           float64    `json:"qtyPer" binding:"gt=0"`
	ScrapRate        float64    `json:"scrapRate" binding:"gte=0,lte=100"`
	DeductTiming     string     `json:"deductTiming" binding:"omitempty,oneof=ON_ARRIVAL ON_ISSUE"`
	ConsumeStationID *uuid.UUID `json:"consumeStationId"`
	SortOrder        int        `json:"sortOrder"`
}

func (h *Handler) listBOM(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if _, ok := h.productInFactory(c, id); !ok {
		return
	}
	rows, err := h.pool.Query(c, `
		SELECT b.id, p.id, p.item_no, p.name, p.unit, p.category, b.material_type, b.qty_per, b.scrap_rate,
		       b.deduct_timing, COALESCE(b.deduct_timing, p.default_deduct_timing), b.consume_station_id, s.name, b.sort_order,
		       COALESCE((SELECT json_agg(json_build_object('id', bs.id, 'productId', sp.id, 'itemNo', sp.item_no,
		                    'itemName', sp.name, 'unit', sp.unit, 'ratio', bs.ratio, 'priority', bs.priority) ORDER BY bs.priority)
		                 FROM bom_substitutes bs JOIN products sp ON sp.id = bs.substitute_product_id
		                 WHERE bs.bom_id = b.id), '[]')
		FROM product_bom b
		JOIN products p ON p.id = b.child_product_id
		LEFT JOIN work_stations s ON s.id = b.consume_station_id
		WHERE b.parent_product_id = $1
		ORDER BY b.sort_order, p.item_no`, id)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BOMLine])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) createBOM(c *gin.Context) {
	var in bomInput
	if !httpx.Bind(c, &in) {
		return
	}
	parentCat, ok := h.productInFactory(c, in.ParentProductID)
	if !ok {
		return
	}
	if parentCat != "FG" && parentCat != "SFG" {
		httpx.Fail(c, httpx.Biz("只有成品或半成品可以建立 BOM"))
		return
	}
	if in.ChildProductID == in.ParentProductID {
		httpx.Fail(c, httpx.Biz("料件不可是自己"))
		return
	}
	if _, ok := h.productInFactory(c, in.ChildProductID); !ok || !h.stationInFactory(c, in.ConsumeStationID) {
		return
	}
	// 循環檢查：子件的展開結構中不可出現母件
	var cycle bool
	if err := h.pool.QueryRow(c, `
		WITH RECURSIVE down(id) AS (
			SELECT child_product_id FROM product_bom WHERE parent_product_id = $1
			UNION
			SELECT b.child_product_id FROM product_bom b JOIN down d ON b.parent_product_id = d.id)
		SELECT EXISTS (SELECT 1 FROM down WHERE id = $2)`, in.ChildProductID, in.ParentProductID).Scan(&cycle); err != nil {
		httpx.DBError(c, err)
		return
	}
	if cycle {
		httpx.Fail(c, httpx.Biz("此料件的 BOM 已包含本品項，加入會形成循環"))
		return
	}
	id := httpx.NewID()
	_, err := h.pool.Exec(c, `
		INSERT INTO product_bom (id, factory_id, parent_product_id, child_product_id, material_type, qty_per,
			scrap_rate, deduct_timing, consume_station_id, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)`,
		id, auth.FactoryID(c), in.ParentProductID, in.ChildProductID, in.MaterialType, in.QtyPer,
		in.ScrapRate, in.DeductTiming, in.ConsumeStationID, in.SortOrder)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// updateBOM 修改用量、損耗、扣帳設定；母件與子件不可改（要換料請刪除後新增）。
func (h *Handler) updateBOM(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in bomInput
	if !httpx.Bind(c, &in) || !h.stationInFactory(c, in.ConsumeStationID) {
		return
	}
	tag, err := h.pool.Exec(c, `
		UPDATE product_bom SET material_type = $3, qty_per = $4, scrap_rate = $5, deduct_timing = NULLIF($6, ''),
		       consume_station_id = $7, sort_order = $8
		WHERE id = $1 AND factory_id = $2`,
		id, auth.FactoryID(c), in.MaterialType, in.QtyPer, in.ScrapRate, in.DeductTiming, in.ConsumeStationID, in.SortOrder)
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

// deleteBOM 刪除 BOM 行；已開立製令的用料是開立當時的複本，不受影響。
func (h *Handler) deleteBOM(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	err := pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		// 製令用料保留來源 BOM 的參照，刪除前先解除
		if _, err := tx.Exec(c, `UPDATE work_order_materials SET bom_id = NULL WHERE bom_id = $1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(c, `DELETE FROM product_bom WHERE id = $1 AND factory_id = $2`, id, auth.FactoryID(c))
		if err == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return err
	})
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type substituteInput struct {
	SubstituteProductID uuid.UUID `json:"substituteProductId" binding:"required"`
	Ratio               float64   `json:"ratio" binding:"gt=0"`
	Priority            int       `json:"priority"`
}

func (h *Handler) addSubstitute(c *gin.Context) {
	bomID, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in substituteInput
	if !httpx.Bind(c, &in) {
		return
	}
	if _, ok := h.productInFactory(c, in.SubstituteProductID); !ok {
		return
	}
	id := httpx.NewID()
	tag, err := h.pool.Exec(c, `
		INSERT INTO bom_substitutes (id, bom_id, substitute_product_id, ratio, priority)
		SELECT $1, b.id, $3, $4, $5 FROM product_bom b
		WHERE b.id = $2 AND b.factory_id = $6 AND b.child_product_id <> $3`,
		id, bomID, in.SubstituteProductID, in.Ratio, in.Priority, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(c, httpx.Biz("BOM 不存在，或替代料與原料件相同"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) deleteSubstitute(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	tag, err := h.pool.Exec(c, `
		DELETE FROM bom_substitutes bs USING product_bom b
		WHERE bs.id = $1 AND b.id = bs.bom_id AND b.factory_id = $2`, id, auth.FactoryID(c))
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
