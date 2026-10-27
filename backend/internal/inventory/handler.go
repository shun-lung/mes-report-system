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
	ID            uuid.UUID `json:"id"`
	TxnNo         string    `json:"txnNo"`
	TxnType       string    `json:"txnType"`
	WarehouseName string    `json:"warehouseName"`
	ItemNo        string    `json:"itemNo"`
	ItemName      string    `json:"itemName"`
	Unit          string    `json:"unit"`
	Qty           float64   `json:"qty"`
	BalanceAfter  float64   `json:"balanceAfter"`
	WONo          *string   `json:"woNo"`
	OccurredAt    time.Time `json:"occurredAt"`
	WorkDate      time.Time `json:"workDate"`
	UserName      string    `json:"userName"`
	Note          *string   `json:"note"`
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
	rows, err := h.pool.Query(c, `
		SELECT t.id, t.txn_no, t.txn_type, w.name, p.item_no, p.name, p.unit, t.qty, t.balance_after,
		       wo.wo_no, t.occurred_at, t.work_date, u.name, t.note
		FROM inventory_transactions t
		JOIN warehouses w ON w.id = t.warehouse_id
		JOIN products p ON p.id = t.product_id
		JOIN users u ON u.id = t.user_id
		LEFT JOIN work_orders wo ON wo.id = t.work_order_id
		WHERE t.factory_id = $1 AND ($2::uuid IS NULL OR t.product_id = $2) AND ($3 = '' OR t.txn_type = $3)
		ORDER BY t.occurred_at DESC, t.txn_no DESC
		LIMIT $4`, auth.FactoryID(c), productID, c.Query("type"), limit)
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
