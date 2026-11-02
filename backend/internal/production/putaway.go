package production

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
	"mes/internal/inventory"
	"mes/internal/org"
)

// 完工入庫：成品末站良品先進完工暫存倉，倉管勾選已完工的製令後，待入庫數量轉進成品倉。

type PutawayRow struct {
	ID       uuid.UUID `json:"id"`
	WoNo     string    `json:"woNo"`
	ItemNo   string    `json:"itemNo"`
	ItemName string    `json:"itemName"`
	Unit     string    `json:"unit"`
	Status   string    `json:"status"`
	Planned  float64   `json:"plannedQty"`
	Good     float64   `json:"goodQty"`    // 完工良品（已入庫＋待入庫）
	Stored   float64   `json:"storedQty"`  // 已入成品倉
	Pending  float64   `json:"pendingQty"` // 在完工暫存倉待入庫
}

// putawayQuery 依製令加總完工暫存倉與成品倉的異動；$2 為空時列全部製令
const putawayQuery = `
	WITH q AS (
		SELECT t.work_order_id,
		       COALESCE(sum(t.qty) FILTER (WHERE wh.type = 'STAGE'), 0) AS pending,
		       COALESCE(sum(t.qty) FILTER (WHERE wh.type = 'FG'), 0) AS stored
		FROM inventory_transactions t
		JOIN warehouses wh ON wh.id = t.warehouse_id AND wh.type IN ('STAGE', 'FG')
		WHERE t.factory_id = $1 AND t.work_order_id IS NOT NULL
		GROUP BY 1)
	SELECT w.id, w.wo_no, p.item_no, p.name, p.unit, w.status, w.planned_qty::float8,
	       (COALESCE(q.pending, 0) + COALESCE(q.stored, 0))::float8, COALESCE(q.stored, 0)::float8, COALESCE(q.pending, 0)::float8
	FROM work_orders w
	JOIN products p ON p.id = w.product_id AND p.category = 'FG'
	LEFT JOIN q ON q.work_order_id = w.id
	WHERE w.factory_id = $1 AND w.status IN ('RELEASED', 'IN_PROGRESS', 'COMPLETED', 'CLOSED')
	  AND ($2::uuid[] IS NULL OR w.id = ANY($2))
	ORDER BY w.wo_no`

func (h *Handler) listPutaway(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, putawayQuery, auth.FactoryID(c), nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PutawayRow])
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type PutawayResult struct {
	ItemNo   string  `json:"itemNo"`
	ItemName string  `json:"itemName"`
	Unit     string  `json:"unit"`
	Qty      float64 `json:"qty"`
	Orders   int     `json:"orders"`
}

// Putaway 勾選的製令（須已完工或結案）把完工暫存倉的待入庫數量全部轉進成品倉，回傳依品號加總。
func (s *Service) Putaway(ctx context.Context, factoryID uuid.UUID, woIDs []uuid.UUID, userID uuid.UUID, at time.Time) ([]PutawayResult, error) {
	var out []PutawayResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM work_orders WHERE factory_id = $1 AND id = ANY($2) FOR UPDATE`, factoryID, woIDs); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, putawayQuery, factoryID, woIDs)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PutawayRow])
		if err != nil {
			return err
		}
		if len(list) != len(woIDs) {
			return httpx.Biz("有製令不存在或不是成品製令，請重新整理")
		}
		for _, r := range list {
			if r.Status != "COMPLETED" && r.Status != "CLOSED" {
				return httpx.Biz("製令 %s 尚未完工，不能入庫", r.WoNo)
			}
			if r.Pending <= 0 {
				return httpx.Biz("製令 %s 沒有待入庫數量", r.WoNo)
			}
		}
		stage, err := inventory.Warehouse(ctx, tx, factoryID, "STAGE")
		if err != nil {
			return err
		}
		fg, err := inventory.Warehouse(ctx, tx, factoryID, "FG")
		if err != nil {
			return err
		}
		_, workDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		sum := map[string]int{}
		for _, r := range list {
			var productID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT product_id FROM work_orders WHERE id = $1`, r.ID).Scan(&productID); err != nil {
				return err
			}
			woID, ref := r.ID, httpx.NewID() // 出入兩筆共用 ref_id，沖銷時一起處理
			for _, leg := range []struct {
				wh  uuid.UUID
				qty float64
			}{{stage, -r.Pending}, {fg, r.Pending}} {
				if _, err := inventory.Post(ctx, tx, inventory.Txn{
					FactoryID: factoryID, Type: "TRANSFER", WarehouseID: leg.wh, ProductID: productID, Qty: leg.qty,
					WorkOrderID: &woID, RefType: "putaway", RefID: &ref, At: at, WorkDate: workDate,
					UserID: userID, Note: "完工入庫 " + r.WoNo,
				}); err != nil {
					return err
				}
			}
			i, ok := sum[r.ItemNo]
			if !ok {
				i = len(out)
				sum[r.ItemNo] = i
				out = append(out, PutawayResult{ItemNo: r.ItemNo, ItemName: r.ItemName, Unit: r.Unit})
			}
			out[i].Qty += r.Pending
			out[i].Orders++
		}
		return nil
	})
	return out, err
}

func (h *Handler) putaway(c *gin.Context) {
	var req struct {
		WorkOrderIDs []uuid.UUID `json:"workOrderIds" binding:"required,min=1,max=200"`
	}
	if !httpx.Bind(c, &req) {
		return
	}
	res, err := h.svc.Putaway(c, auth.FactoryID(c), req.WorkOrderIDs, auth.UserID(c), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// stagedQty 製令目前留在完工暫存倉的數量（撤銷末站回報前檢查是否已入庫）。
func stagedQty(ctx context.Context, tx pgx.Tx, woID, warehouseID uuid.UUID) (float64, error) {
	var q float64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(qty), 0)::float8 FROM inventory_transactions WHERE work_order_id = $1 AND warehouse_id = $2`,
		woID, warehouseID).Scan(&q)
	return q, err
}
