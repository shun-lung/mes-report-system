package purchasing

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

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(scoped *gin.RouterGroup) {
	// 產品維護需要選預設供應商，故 product:edit 也可讀供應商清單
	scoped.GET("/suppliers", auth.RequireAny("purchase:view", "product:edit"), h.listSuppliers)
	scoped.POST("/suppliers", auth.Require("purchase:edit"), h.saveSupplier)
	scoped.PUT("/suppliers/:id", auth.Require("purchase:edit"), h.saveSupplier)

	// 倉管收料需要看採購單，故 inventory:edit 也可讀
	scoped.GET("/purchase-orders", auth.RequireAny("purchase:view", "inventory:edit"), h.listPOs)
	scoped.POST("/purchase-orders", auth.Require("purchase:edit"), h.create)
	scoped.PUT("/purchase-orders/:id", auth.Require("purchase:edit"), h.update)
	scoped.POST("/purchase-orders/:id/order", auth.Require("purchase:edit"), h.order)
	scoped.POST("/purchase-orders/:id/cancel", auth.Require("purchase:edit"), h.cancel)
	scoped.POST("/purchase-orders/:id/receive", auth.Require("inventory:edit"), h.receive)
}

type supplierRow struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Contact  *string   `json:"contact"`
	Phone    *string   `json:"phone"`
	LeadDays int       `json:"leadDays"`
	IsActive bool      `json:"isActive"`
	Products int       `json:"products"` // 以此為預設供應商的料件數
}

type supplierInput struct {
	Code     string `json:"code" binding:"required,max=20"`
	Name     string `json:"name" binding:"required,max=100"`
	Contact  string `json:"contact" binding:"max=50"`
	Phone    string `json:"phone" binding:"max=30"`
	LeadDays int    `json:"leadDays" binding:"gte=0,lte=365"`
	IsActive *bool  `json:"isActive"`
}

func (h *Handler) listSuppliers(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, `
		SELECT s.id, s.code, s.name, s.contact, s.phone, s.lead_days, s.is_active,
		       (SELECT count(*) FROM products p WHERE p.default_supplier_id = s.id)::int
		FROM suppliers s WHERE s.factory_id = $1 ORDER BY s.code`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[supplierRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) saveSupplier(c *gin.Context) {
	var in supplierInput
	if !httpx.Bind(c, &in) {
		return
	}
	active := in.IsActive == nil || *in.IsActive
	factoryID := auth.FactoryID(c)
	if c.Param("id") == "" {
		id := httpx.NewID()
		if _, err := h.svc.pool.Exec(c, `
			INSERT INTO suppliers (id, factory_id, code, name, contact, phone, lead_days, is_active)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)`,
			id, factoryID, in.Code, in.Name, in.Contact, in.Phone, in.LeadDays, active); err != nil {
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
	tag, err := h.svc.pool.Exec(c, `
		UPDATE suppliers SET code = $3, name = $4, contact = NULLIF($5, ''), phone = NULLIF($6, ''), lead_days = $7, is_active = $8
		WHERE id = $1 AND factory_id = $2`, id, factoryID, in.Code, in.Name, in.Contact, in.Phone, in.LeadDays, active)
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

type poRow struct {
	ID           uuid.UUID       `json:"id"`
	PONo         string          `json:"poNo"`
	Status       string          `json:"status"`
	Source       string          `json:"source"`
	SupplierID   *uuid.UUID      `json:"supplierId"`
	Supplier     *string         `json:"supplier"`
	ExpectedDate *time.Time      `json:"expectedDate"`
	Note         *string         `json:"note"`
	CreatedBy    string          `json:"createdBy"`
	CreatedAt    time.Time       `json:"createdAt"`
	OrderedAt    *time.Time      `json:"orderedAt"`
	Lines        json.RawMessage `json:"lines"`
}

func (h *Handler) listPOs(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, `
		SELECT po.id, po.po_no, po.status, po.source, po.supplier_id, s.name, po.expected_date, po.note, u.name, po.created_at, po.ordered_at,
		       COALESCE((SELECT json_agg(json_build_object(
		            'id', l.id, 'lineNo', l.line_no, 'productId', p.id, 'itemNo', p.item_no, 'itemName', p.name, 'unit', p.unit,
		            'qty', l.qty, 'receivedQty', l.received_qty, 'workOrderId', l.work_order_id, 'woNo', w.wo_no, 'note', l.note)
		          ORDER BY l.line_no)
		        FROM purchase_order_lines l JOIN products p ON p.id = l.product_id LEFT JOIN work_orders w ON w.id = l.work_order_id
		        WHERE l.po_id = po.id), '[]')
		FROM purchase_orders po
		LEFT JOIN suppliers s ON s.id = po.supplier_id
		JOIN users u ON u.id = po.created_by
		WHERE po.factory_id = $1 AND ($2 = '' OR po.status = ANY (string_to_array($2, ',')))
		ORDER BY po.created_at DESC LIMIT 300`, auth.FactoryID(c), c.Query("status"))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[poRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) create(c *gin.Context) {
	var in POInput
	if !httpx.Bind(c, &in) {
		return
	}
	id, no, err := h.svc.Create(c, auth.FactoryID(c), in, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "poNo": no})
}

func (h *Handler) update(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in POInput
	if !httpx.Bind(c, &in) {
		return
	}
	if err := h.svc.UpdateDraft(c, auth.FactoryID(c), id, in, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) order(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if err := h.svc.Order(c, auth.FactoryID(c), id, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) cancel(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if err := h.svc.Cancel(c, auth.FactoryID(c), id, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type receiveRequest struct {
	WarehouseID uuid.UUID     `json:"warehouseId" binding:"required"`
	Note        string        `json:"note" binding:"max=200"`
	Lines       []ReceiveLine `json:"lines" binding:"required,min=1,dive"`
}

func (h *Handler) receive(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var req receiveRequest
	if !httpx.Bind(c, &req) {
		return
	}
	n, err := h.svc.Receive(c, auth.FactoryID(c), id, req.WarehouseID, req.Lines, req.Note, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"posted": n})
}
