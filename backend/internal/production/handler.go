package production

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

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(scoped *gin.RouterGroup) {
	scoped.GET("/sales-orders", auth.Require("wo:view"), h.listSalesOrders)
	scoped.POST("/sales-orders", auth.Require("wo:edit"), h.createSalesOrder)
	scoped.POST("/sales-orders/:id/work-orders", auth.Require("wo:edit"), h.convertSalesOrder)
	scoped.PUT("/sales-orders/:id", auth.Require("wo:edit"), h.updateSalesOrder)
	scoped.POST("/sales-orders/:id/cancel", auth.Require("wo:edit"), h.cancelSalesOrder)

	scoped.GET("/customers", auth.Require("wo:view"), h.listCustomers)
	scoped.POST("/customers", auth.Require("wo:edit"), h.saveCustomer)
	scoped.PUT("/customers/:id", auth.Require("wo:edit"), h.saveCustomer)

	scoped.GET("/work-orders", auth.Require("wo:view"), h.listWorkOrders)
	scoped.POST("/work-orders", auth.Require("wo:edit"), h.createWorkOrder)
	scoped.PUT("/work-orders/:id", auth.Require("wo:edit"), h.updateWorkOrder)
	scoped.POST("/work-orders/:id/cancel", auth.Require("wo:edit"), h.cancelWorkOrder)
	scoped.POST("/work-orders/:id/release", auth.Require("wo:release"), h.release)
	scoped.POST("/work-orders/:id/issue", auth.Require("inventory:edit"), h.issue)
	scoped.GET("/work-orders/:id/cards", auth.Require("wo:view"), h.cardsForPrint)
	scoped.POST("/work-orders/:id/cards/printed", auth.Require("wo:release"), h.markPrinted)

	scoped.GET("/station/state", auth.Require("scan:report"), h.stationState)
	scoped.GET("/cards/:cardNo", auth.Require("scan:report"), h.cardInfo)
	scoped.POST("/cards/:cardNo/split", auth.RequireAny("scan:report", "wo:edit"), h.splitCard)
	scoped.POST("/cards/:cardNo/merge", auth.RequireAny("scan:report", "wo:edit"), h.mergeCard)
	scoped.GET("/work-orders/:id/card-progress", auth.Require("wo:view"), h.workOrderCards)
	scoped.POST("/scans", auth.Require("scan:report"), h.scan)
	scoped.GET("/scans", auth.RequireAny("wo:view", "scan:report"), h.listScans)
	scoped.POST("/scans/:id/reverse", auth.RequireAny("scan:report", "wo:edit"), h.reverseScan)
	scoped.POST("/labor/start", auth.Require("labor:report"), h.startLabor)
	scoped.POST("/labor/end", auth.Require("labor:report"), h.endLabor)

	scoped.GET("/alerts", h.listAlerts)
	scoped.POST("/alerts/:id/decide", auth.Require("alert:decide"), h.decide)
	scoped.GET("/shortages", auth.RequireAny("wo:view", "purchase:view", "inventory:view"), h.listShortages)
	scoped.POST("/shortages/purchase", auth.Require("purchase:edit"), h.purchaseShortages)
	scoped.POST("/shortages/work-order", auth.Require("wo:edit"), h.workOrderForShortage)
}

func (h *Handler) createSalesOrder(c *gin.Context) {
	var in SalesOrderInput
	if !httpx.Bind(c, &in) {
		return
	}
	id, no, err := h.svc.CreateSalesOrder(c, auth.FactoryID(c), in, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "soNo": no})
}

func (h *Handler) convertSalesOrder(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	list, err := h.svc.ConvertSalesOrder(c, auth.FactoryID(c), id, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, list)
}

func (h *Handler) release(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	res, err := h.svc.Release(c, auth.FactoryID(c), id, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) issue(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	n, err := h.svc.IssueMaterials(c, auth.FactoryID(c), id, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"issuedLines": n})
}

type scanRequest struct {
	CardNo   string        `json:"cardNo" binding:"required"`
	LineID   uuid.UUID     `json:"lineId" binding:"required"`
	GoodQty  float64       `json:"goodQty" binding:"gte=0"`
	Defects  []DefectInput `json:"defects" binding:"dive"`
	DeviceID string        `json:"deviceId"`
}

func (h *Handler) scan(c *gin.Context) {
	var req scanRequest
	if !httpx.Bind(c, &req) {
		return
	}
	res, err := h.svc.Scan(c, ScanInput{
		FactoryID: auth.FactoryID(c), CardNo: strings.TrimSpace(req.CardNo), LineID: req.LineID,
		GoodQty: req.GoodQty, Defects: req.Defects, UserID: auth.UserID(c), DeviceID: req.DeviceID, At: time.Now(),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type laborRequest struct {
	LineID      uuid.UUID `json:"lineId" binding:"required"`
	WorkOrderID uuid.UUID `json:"workOrderId"`
	Headcount   int       `json:"headcount"`
}

func (h *Handler) startLabor(c *gin.Context) {
	var req laborRequest
	if !httpx.Bind(c, &req) {
		return
	}
	id, err := h.svc.StartLabor(c, auth.FactoryID(c), req.LineID, req.WorkOrderID, req.Headcount, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) endLabor(c *gin.Context) {
	var req laborRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if err := h.svc.EndLabor(c, auth.FactoryID(c), req.LineID, auth.UserID(c), time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type decideRequest struct {
	Action string `json:"action" binding:"required,oneof=CREATE_WO CREATE_PO PASS"`
	Note   string `json:"note"`
}

func (h *Handler) decide(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var req decideRequest
	if !httpx.Bind(c, &req) {
		return
	}
	res, err := h.svc.DecideAlert(c, auth.FactoryID(c), id, req.Action, auth.UserID(c), req.Note, time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type alertRow struct {
	ID           uuid.UUID  `json:"id"`
	Status       string     `json:"status"`
	WONo         *string    `json:"woNo"`
	ItemNo       string     `json:"itemNo"`
	ItemName     string     `json:"itemName"`
	Category     string     `json:"category"`
	Unit         string     `json:"unit"`
	RequiredQty  float64    `json:"requiredQty"`
	AvailableQty float64    `json:"availableQty"`
	ThresholdQty float64    `json:"thresholdQty"`
	ShortageQty  float64    `json:"shortageQty"`
	CanCreateWO  bool       `json:"canCreateWo"`
	CreatedWONo  *string    `json:"createdWoNo"`
	CreatedPONo  *string    `json:"createdPoNo"`
	DecidedBy    *string    `json:"decidedBy"`
	DecidedAt    *time.Time `json:"decidedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	Note         *string    `json:"note"`
}

func (h *Handler) listAlerts(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, `
		SELECT a.id, a.status, w.wo_no, p.item_no, p.name, p.category, p.unit,
		       a.required_qty, a.available_qty, a.threshold_qty, a.shortage_qty,
		       EXISTS (SELECT 1 FROM routings r WHERE r.product_id = p.id AND r.is_active),
		       cw.wo_no, cp.po_no, u.name, a.decided_at, a.created_at, a.note
		FROM material_alerts a
		JOIN products p ON p.id = a.product_id
		LEFT JOIN work_orders w ON w.id = a.work_order_id
		LEFT JOIN work_orders cw ON cw.id = a.created_wo_id
		LEFT JOIN purchase_orders cp ON cp.id = a.created_po_id
		LEFT JOIN users u ON u.id = a.decided_by
		WHERE a.factory_id = $1 AND ($2 = '' OR a.status = $2)
		ORDER BY a.created_at DESC LIMIT 200`, auth.FactoryID(c), c.Query("status"))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[alertRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) updateSalesOrder(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in SalesOrderInput
	if !httpx.Bind(c, &in) {
		return
	}
	if err := h.svc.UpdateSalesOrder(c, auth.FactoryID(c), id, in, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) cancelSalesOrder(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	if err := h.svc.CancelSalesOrder(c, auth.FactoryID(c), id, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type workOrderInput struct {
	ProductID uuid.UUID `json:"productId"`
	Qty       float64   `json:"qty" binding:"gt=0"`
	DueDate   string    `json:"dueDate"` // YYYY-MM-DD，可空
}

func (in workOrderInput) due() (*time.Time, error) {
	if in.DueDate == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, in.DueDate)
	if err != nil {
		return nil, httpx.Biz("交期格式錯誤：%s", in.DueDate)
	}
	return &d, nil
}

func (h *Handler) createWorkOrder(c *gin.Context) {
	var in workOrderInput
	if !httpx.Bind(c, &in) {
		return
	}
	due, err := in.due()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	id, no, err := h.svc.CreateWorkOrder(c, auth.FactoryID(c), in.ProductID, in.Qty, due, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "woNo": no})
}

func (h *Handler) updateWorkOrder(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in workOrderInput
	if !httpx.Bind(c, &in) {
		return
	}
	due, err := in.due()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.UpdateDraft(c, auth.FactoryID(c), id, in.Qty, due, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type cancelRequest struct {
	Reason string `json:"reason" binding:"max=200"`
}

func (h *Handler) cancelWorkOrder(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var req cancelRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if err := h.svc.CancelWorkOrder(c, auth.FactoryID(c), id, auth.UserID(c), req.Reason, time.Now()); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
