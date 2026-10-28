package masterdata

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type Product struct {
	ID                  uuid.UUID  `json:"id"`
	ItemNo              string     `json:"itemNo"`
	Name                string     `json:"name"`
	Barcode             *string    `json:"barcode"`
	Spec                *string    `json:"spec"`
	Unit                string     `json:"unit"`
	Category            string     `json:"category"`
	DefaultDeductTiming string     `json:"defaultDeductTiming"`
	SafetyStock         float64    `json:"safetyStock"`
	AlertPct            float64    `json:"alertPct"`
	CardQty             int        `json:"cardQty"`
	IsActive            bool       `json:"isActive"`
	PrimaryImageID      *uuid.UUID `json:"primaryImageId"`
	BOMLines            int        `json:"bomLines"`
	ActiveRouting       *int       `json:"activeRoutingVersion"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type productInput struct {
	ItemNo              string  `json:"itemNo" binding:"required,max=50"`
	Name                string  `json:"name" binding:"required,max=200"`
	Barcode             string  `json:"barcode" binding:"max=100"`
	Spec                string  `json:"spec"`
	Unit                string  `json:"unit" binding:"required,max=10"`
	Category            string  `json:"category" binding:"required,oneof=FG SFG RAW AUX"`
	DefaultDeductTiming string  `json:"defaultDeductTiming" binding:"required,oneof=ON_ARRIVAL ON_ISSUE"`
	SafetyStock         float64 `json:"safetyStock" binding:"gte=0"`
	AlertPct            float64 `json:"alertPct" binding:"gte=0,lte=100"`
	CardQty             int     `json:"cardQty" binding:"gte=1"`
	IsActive            *bool   `json:"isActive"`
}

const productSelect = `
	SELECT p.id, p.item_no, p.name, p.barcode, p.spec, p.unit, p.category, p.default_deduct_timing,
	       p.safety_stock, p.alert_pct, p.card_qty, p.is_active,
	       (SELECT i.id FROM product_images i WHERE i.product_id = p.id ORDER BY i.is_primary DESC, i.sort_order, i.created_at LIMIT 1),
	       (SELECT count(*) FROM product_bom b WHERE b.parent_product_id = p.id)::int,
	       (SELECT r.version FROM routings r WHERE r.product_id = p.id AND r.is_active),
	       p.updated_at
	FROM products p`

func (h *Handler) listProducts(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	rows, err := h.pool.Query(c, productSelect+`
		WHERE p.factory_id = $1
		  AND ($2 = '' OR p.item_no ILIKE '%' || $2 || '%' OR p.name ILIKE '%' || $2 || '%' OR p.barcode = $2)
		  AND ($3 = '' OR p.category = $3)
		ORDER BY array_position(ARRAY['FG','SFG','RAW','AUX'], p.category::text), p.item_no`,
		auth.FactoryID(c), q, c.Query("category"))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Product])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type ProductImage struct {
	ID        uuid.UUID `json:"id"`
	FileName  string    `json:"fileName"`
	MimeType  string    `json:"mimeType"`
	FileSize  int64     `json:"fileSize"`
	IsPrimary bool      `json:"isPrimary"`
}

func (h *Handler) getProduct(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	rows, err := h.pool.Query(c, productSelect+` WHERE p.id = $1 AND p.factory_id = $2`, id, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Product])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	rows, err = h.pool.Query(c, `
		SELECT id, file_name, mime_type, file_size, is_primary FROM product_images
		WHERE product_id = $1 ORDER BY is_primary DESC, sort_order, created_at`, id)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	images, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ProductImage])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"product": p, "images": images})
}

func (h *Handler) createProduct(c *gin.Context) {
	var in productInput
	if !httpx.Bind(c, &in) {
		return
	}
	id := httpx.NewID()
	_, err := h.pool.Exec(c, `
		INSERT INTO products (id, factory_id, item_no, name, barcode, spec, unit, category, default_deduct_timing,
			safety_stock, alert_pct, card_qty, is_active)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, $11, $12, $13)`,
		id, auth.FactoryID(c), strings.TrimSpace(in.ItemNo), in.Name, strings.TrimSpace(in.Barcode), in.Spec, in.Unit,
		in.Category, in.DefaultDeductTiming, in.SafetyStock, in.AlertPct, in.CardQty, active(in.IsActive))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) updateProduct(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in productInput
	if !httpx.Bind(c, &in) {
		return
	}
	// 已有 BOM 或途程的成品／半成品，不可改成材料類（會使 BOM 與途程失去意義）
	if in.Category == "RAW" || in.Category == "AUX" {
		var used bool
		if err := h.pool.QueryRow(c, `
			SELECT EXISTS (SELECT 1 FROM product_bom WHERE parent_product_id = $1)
			    OR EXISTS (SELECT 1 FROM routings WHERE product_id = $1)`, id).Scan(&used); err != nil {
			httpx.DBError(c, err)
			return
		}
		if used {
			httpx.Fail(c, httpx.Biz("此品項已有 BOM 或途程，不可改為材料類別"))
			return
		}
	}
	tag, err := h.pool.Exec(c, `
		UPDATE products SET item_no = $3, name = $4, barcode = NULLIF($5, ''), spec = NULLIF($6, ''), unit = $7,
		       category = $8, default_deduct_timing = $9, safety_stock = $10, alert_pct = $11, card_qty = $12,
		       is_active = $13, updated_at = now()
		WHERE id = $1 AND factory_id = $2`,
		id, auth.FactoryID(c), strings.TrimSpace(in.ItemNo), in.Name, strings.TrimSpace(in.Barcode), in.Spec, in.Unit,
		in.Category, in.DefaultDeductTiming, in.SafetyStock, in.AlertPct, in.CardQty, active(in.IsActive))
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

// productInFactory 確認產品屬於本工廠，回傳其類別。
func (h *Handler) productInFactory(c *gin.Context, id uuid.UUID) (string, bool) {
	var category string
	err := h.pool.QueryRow(c, `SELECT category FROM products WHERE id = $1 AND factory_id = $2`,
		id, auth.FactoryID(c)).Scan(&category)
	if err == pgx.ErrNoRows {
		httpx.Fail(c, httpx.Biz("產品不存在"))
		return "", false
	}
	if err != nil {
		httpx.DBError(c, err)
		return "", false
	}
	return category, true
}
