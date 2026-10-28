package inventory

import (
	"net/http"
	"strconv"
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

func (h *Handler) Register(scoped *gin.RouterGroup) {
	scoped.GET("/inventory/balances", auth.Require("inventory:view"), h.balances)
	scoped.GET("/inventory/transactions", auth.Require("inventory:view"), h.transactions)
	scoped.GET("/inventory/work-orders/:id/materials", auth.Require("inventory:view"), h.workOrderMaterials)
	scoped.POST("/inventory/receipts", auth.Require("inventory:edit"), h.receipt)
	scoped.POST("/inventory/issues", auth.Require("inventory:edit"), h.issue)
	scoped.POST("/inventory/transfers", auth.Require("inventory:edit"), h.transfer)
	scoped.POST("/inventory/adjustments", auth.Require("inventory:adjust"), h.adjust)
	scoped.POST("/inventory/transactions/:id/reverse", auth.Require("inventory:adjust"), h.reverse)

	scoped.GET("/warehouses", auth.Require("inventory:view"), h.listWarehouses)
	scoped.POST("/warehouses", auth.Require("inventory:adjust"), h.saveWarehouse)
	scoped.PUT("/warehouses/:id", auth.Require("inventory:adjust"), h.saveWarehouse)
}

type balanceRow struct {
	WarehouseCode string    `json:"warehouseCode"`
	WarehouseName string    `json:"warehouseName"`
	WarehouseType string    `json:"warehouseType"`
	ProductID     uuid.UUID `json:"productId"`
	ItemNo        string    `json:"itemNo"`
	ItemName      string    `json:"itemName"`
	Category      string    `json:"category"`
	Unit          string    `json:"unit"`
	Qty           float64   `json:"qty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (h *Handler) balances(c *gin.Context) {
	rows, err := h.pool.Query(c, `
		SELECT w.code, w.name, w.type, p.id, p.item_no, p.name, p.category, p.unit, b.qty, b.updated_at
		FROM inventory_balances b
		JOIN warehouses w ON w.id = b.warehouse_id
		JOIN products p ON p.id = b.product_id
		WHERE w.factory_id = $1
		ORDER BY w.code, p.item_no`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[balanceRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type txnRow struct {
	ID            uuid.UUID  `json:"id"`
	TxnNo         string     `json:"txnNo"`
	TxnType       string     `json:"txnType"`
	WarehouseName string     `json:"warehouseName"`
	ItemNo        string     `json:"itemNo"`
	ItemName      string     `json:"itemName"`
	Unit          string     `json:"unit"`
	Qty           float64    `json:"qty"`
	BalanceAfter  float64    `json:"balanceAfter"`
	WONo          *string    `json:"woNo"`
	OccurredAt    time.Time  `json:"occurredAt"`
	WorkDate      time.Time  `json:"workDate"`
	UserName      string     `json:"userName"`
	Note          *string    `json:"note"`
	ReversalOf    *uuid.UUID `json:"reversalOf"`
	Reversed      bool       `json:"reversed"`
}

func (h *Handler) transactions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "300"))
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	var productID *uuid.UUID
	if s := c.Query("productId"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Abort(c, http.StatusBadRequest, "productId 格式錯誤")
			return
		}
		productID = &id
	}
	var warehouseID *uuid.UUID
	if s := c.Query("warehouseId"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Abort(c, http.StatusBadRequest, "warehouseId 格式錯誤")
			return
		}
		warehouseID = &id
	}
	rows, err := h.pool.Query(c, `
		SELECT t.id, t.txn_no, t.txn_type, w.name, p.item_no, p.name, p.unit, t.qty, t.balance_after,
		       wo.wo_no, t.occurred_at, t.work_date, u.name, t.note, t.reversal_of,
		       EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
		FROM inventory_transactions t
		JOIN warehouses w ON w.id = t.warehouse_id
		JOIN products p ON p.id = t.product_id
		JOIN users u ON u.id = t.user_id
		LEFT JOIN work_orders wo ON wo.id = t.work_order_id
		WHERE t.factory_id = $1 AND ($2::uuid IS NULL OR t.product_id = $2) AND ($3 = '' OR t.txn_type = $3)
		  AND ($5::uuid IS NULL OR t.warehouse_id = $5)
		ORDER BY t.occurred_at DESC, t.txn_no DESC
		LIMIT $4`, auth.FactoryID(c), productID, c.Query("type"), limit, warehouseID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[txnRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}
