package inventory

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
	"mes/internal/org"
)

// 倉管手動作業：收料、發料（含製令發料）、移轉、盤點調整、沖銷。
// 手動出庫不可超過現有庫存；現場掃描的自動扣帳則允許負庫存（見 Post）。

type qtyLine struct {
	ProductID uuid.UUID `json:"productId" binding:"required"`
	Qty       float64   `json:"qty" binding:"gt=0"`
}

type receiptInput struct {
	WarehouseID uuid.UUID `json:"warehouseId" binding:"required"`
	Note        string    `json:"note" binding:"max=200"`
	Lines       []qtyLine `json:"lines" binding:"required,min=1,dive"`
}

type issueInput struct {
	WarehouseID uuid.UUID  `json:"warehouseId" binding:"required"`
	WorkOrderID *uuid.UUID `json:"workOrderId"`
	Note        string     `json:"note" binding:"max=200"`
	Lines       []qtyLine  `json:"lines" binding:"required,min=1,dive"`
}

type transferInput struct {
	FromWarehouseID uuid.UUID `json:"fromWarehouseId" binding:"required"`
	ToWarehouseID   uuid.UUID `json:"toWarehouseId" binding:"required"`
	Note            string    `json:"note" binding:"max=200"`
	Lines           []qtyLine `json:"lines" binding:"required,min=1,dive"`
}

type countLine struct {
	ProductID  uuid.UUID `json:"productId" binding:"required"`
	CountedQty float64   `json:"countedQty" binding:"gte=0"`
}

type adjustInput struct {
	WarehouseID uuid.UUID   `json:"warehouseId" binding:"required"`
	Note        string      `json:"note" binding:"max=200"`
	Lines       []countLine `json:"lines" binding:"required,min=1,dive"`
}

// opContext 為一次作業的共用資訊。
type opContext struct {
	factoryID, userID, refID uuid.UUID
	at, workDate             time.Time
	posted                   int
}

// run 在單一交易內執行作業，失敗則整筆還原。
func (h *Handler) run(c *gin.Context, fn func(ctx context.Context, tx pgx.Tx, op *opContext) error) {
	op := &opContext{factoryID: auth.FactoryID(c), userID: auth.UserID(c), refID: httpx.NewID(), at: time.Now()}
	err := pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		var err error
		if _, op.workDate, err = org.Clock(c, tx, op.factoryID, op.at); err != nil {
			return err
		}
		return fn(c, tx, op)
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"posted": op.posted, "refId": op.refID})
}

func warehouseName(ctx context.Context, tx pgx.Tx, factoryID, id uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM warehouses WHERE id = $1 AND factory_id = $2 AND is_active`, id, factoryID).Scan(&name)
	if err == pgx.ErrNoRows {
		return "", httpx.Biz("倉庫不存在或已停用")
	}
	return name, err
}

func productLabel(ctx context.Context, tx pgx.Tx, factoryID, id uuid.UUID) (string, error) {
	var label string
	err := tx.QueryRow(ctx, `SELECT item_no || ' ' || name FROM products WHERE id = $1 AND factory_id = $2`, id, factoryID).Scan(&label)
	if err == pgx.ErrNoRows {
		return "", httpx.Biz("料件不存在")
	}
	return label, err
}

// lockedBalance 鎖定並取得目前庫存（尚無紀錄視為 0）。
func lockedBalance(ctx context.Context, tx pgx.Tx, warehouseID, productID uuid.UUID) (float64, error) {
	var qty float64
	err := tx.QueryRow(ctx, `SELECT qty FROM inventory_balances WHERE warehouse_id = $1 AND product_id = $2 FOR UPDATE`,
		warehouseID, productID).Scan(&qty)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return qty, err
}

// takeOut 檢查庫存足夠後出庫。
func (op *opContext) takeOut(ctx context.Context, tx pgx.Tx, typ string, warehouseID, productID uuid.UUID, qty float64,
	woID *uuid.UUID, refType, note string) error {
	label, err := productLabel(ctx, tx, op.factoryID, productID)
	if err != nil {
		return err
	}
	have, err := lockedBalance(ctx, tx, warehouseID, productID)
	if err != nil {
		return err
	}
	if qty > have {
		return httpx.Biz("%s 庫存不足（現有 %g，要出 %g）", label, have, qty)
	}
	return op.post(ctx, tx, typ, warehouseID, productID, -qty, woID, refType, note, nil)
}

func (op *opContext) post(ctx context.Context, tx pgx.Tx, typ string, warehouseID, productID uuid.UUID, qty float64,
	woID *uuid.UUID, refType, note string, reversalOf *uuid.UUID) error {
	_, err := Post(ctx, tx, Txn{
		FactoryID: op.factoryID, Type: typ, WarehouseID: warehouseID, ProductID: productID, Qty: qty,
		WorkOrderID: woID, RefType: refType, RefID: &op.refID, ReversalOf: reversalOf,
		At: op.at, WorkDate: op.workDate, UserID: op.userID, Note: note,
	})
	if err == nil {
		op.posted++
	}
	return err
}

func (h *Handler) receipt(c *gin.Context) {
	var in receiptInput
	if !httpx.Bind(c, &in) {
		return
	}
	h.run(c, func(ctx context.Context, tx pgx.Tx, op *opContext) error {
		if _, err := warehouseName(ctx, tx, op.factoryID, in.WarehouseID); err != nil {
			return err
		}
		for _, l := range in.Lines {
			if _, err := productLabel(ctx, tx, op.factoryID, l.ProductID); err != nil {
				return err
			}
			if err := op.post(ctx, tx, "RECEIPT", in.WarehouseID, l.ProductID, l.Qty, nil, "receipt", in.Note, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// issue 發料出庫；指定製令時同步累加製令用料的已發數量。
func (h *Handler) issue(c *gin.Context) {
	var in issueInput
	if !httpx.Bind(c, &in) {
		return
	}
	h.run(c, func(ctx context.Context, tx pgx.Tx, op *opContext) error {
		if _, err := warehouseName(ctx, tx, op.factoryID, in.WarehouseID); err != nil {
			return err
		}
		if in.WorkOrderID != nil {
			var status string
			err := tx.QueryRow(ctx, `SELECT status FROM work_orders WHERE id = $1 AND factory_id = $2`, *in.WorkOrderID, op.factoryID).Scan(&status)
			if err == pgx.ErrNoRows {
				return httpx.Biz("製令不存在")
			}
			if err != nil {
				return err
			}
			if status != "RELEASED" && status != "IN_PROGRESS" {
				return httpx.Biz("製令狀態不可發料")
			}
		}
		for _, l := range in.Lines {
			if err := op.takeOut(ctx, tx, "ISSUE", in.WarehouseID, l.ProductID, l.Qty, in.WorkOrderID, "issue", in.Note); err != nil {
				return err
			}
			if in.WorkOrderID != nil {
				if err := addIssued(ctx, tx, *in.WorkOrderID, l.ProductID, l.Qty); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// addIssued 調整製令用料的已發數量（同料件取第一筆用料）。
func addIssued(ctx context.Context, tx pgx.Tx, woID, productID uuid.UUID, qty float64) error {
	_, err := tx.Exec(ctx, `
		UPDATE work_order_materials SET issued_qty = GREATEST(issued_qty + $3, 0)
		WHERE id = (SELECT id FROM work_order_materials WHERE work_order_id = $1 AND product_id = $2 ORDER BY id LIMIT 1)`,
		woID, productID, qty)
	return err
}

func (h *Handler) transfer(c *gin.Context) {
	var in transferInput
	if !httpx.Bind(c, &in) {
		return
	}
	if in.FromWarehouseID == in.ToWarehouseID {
		httpx.Fail(c, httpx.Biz("來源與目的倉庫不可相同"))
		return
	}
	h.run(c, func(ctx context.Context, tx pgx.Tx, op *opContext) error {
		from, err := warehouseName(ctx, tx, op.factoryID, in.FromWarehouseID)
		if err != nil {
			return err
		}
		to, err := warehouseName(ctx, tx, op.factoryID, in.ToWarehouseID)
		if err != nil {
			return err
		}
		note := from + " → " + to
		if in.Note != "" {
			note += "：" + in.Note
		}
		for _, l := range in.Lines {
			if err := op.takeOut(ctx, tx, "TRANSFER", in.FromWarehouseID, l.ProductID, l.Qty, nil, "transfer", note); err != nil {
				return err
			}
			if err := op.post(ctx, tx, "TRANSFER", in.ToWarehouseID, l.ProductID, l.Qty, nil, "transfer", note, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// adjust 盤點調整：輸入實盤數量，依與帳面的差異產生調整異動（無差異略過）。
func (h *Handler) adjust(c *gin.Context) {
	var in adjustInput
	if !httpx.Bind(c, &in) {
		return
	}
	h.run(c, func(ctx context.Context, tx pgx.Tx, op *opContext) error {
		if _, err := warehouseName(ctx, tx, op.factoryID, in.WarehouseID); err != nil {
			return err
		}
		for _, l := range in.Lines {
			if _, err := productLabel(ctx, tx, op.factoryID, l.ProductID); err != nil {
				return err
			}
			book, err := lockedBalance(ctx, tx, in.WarehouseID, l.ProductID)
			if err != nil {
				return err
			}
			diff := round4(l.CountedQty - book)
			if diff == 0 {
				continue
			}
			note := "盤點調整"
			if in.Note != "" {
				note += "：" + in.Note
			}
			if err := op.post(ctx, tx, "ADJUST", in.WarehouseID, l.ProductID, diff, nil, "adjust", note, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

type reverseInput struct {
	Note string `json:"note" binding:"max=200"`
}

// reverse 沖銷手動異動：產生反向異動並指向原異動；移轉的出入兩筆一起沖銷。
func (h *Handler) reverse(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var in reverseInput
	if !httpx.Bind(c, &in) {
		return
	}
	h.run(c, func(ctx context.Context, tx pgx.Tx, op *opContext) error {
		var typ string
		var refType *string
		var refID *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT txn_type, ref_type, ref_id FROM inventory_transactions WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			id, op.factoryID).Scan(&typ, &refType, &refID)
		if err == pgx.ErrNoRows {
			return httpx.Biz("異動不存在")
		}
		if err != nil {
			return err
		}
		switch typ {
		case "RECEIPT", "ISSUE", "TRANSFER", "ADJUST":
		case "REVERSAL":
			return httpx.Biz("沖銷單不可再沖銷")
		default:
			return httpx.Biz("生產自動產生的異動（扣帳、完工入庫）不可在此沖銷")
		}

		legs := []uuid.UUID{id}
		if typ == "TRANSFER" && refID != nil {
			rows, err := tx.Query(ctx, `
				SELECT id FROM inventory_transactions WHERE ref_type = 'transfer' AND ref_id = $1 AND txn_type = 'TRANSFER'
				ORDER BY qty`, *refID) // 先處理出庫（負數）那筆
			if err != nil {
				return err
			}
			if legs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return err
			}
		}
		for _, leg := range legs {
			var done bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM inventory_transactions WHERE reversal_of = $1)`, leg).Scan(&done); err != nil {
				return err
			}
			if done {
				return httpx.Biz("此異動已沖銷過")
			}
			var t struct {
				no                     string
				warehouseID, productID uuid.UUID
				qty                    float64
				woID                   *uuid.UUID
				legType                string
			}
			if err := tx.QueryRow(ctx, `
				SELECT txn_no, warehouse_id, product_id, qty, work_order_id, txn_type FROM inventory_transactions WHERE id = $1`, leg).
				Scan(&t.no, &t.warehouseID, &t.productID, &t.qty, &t.woID, &t.legType); err != nil {
				return err
			}
			note := "沖銷 " + t.no
			if in.Note != "" {
				note += "：" + in.Note
			}
			if t.qty > 0 {
				// 原本是入庫，沖銷會減少庫存，需檢查足夠
				have, err := lockedBalance(ctx, tx, t.warehouseID, t.productID)
				if err != nil {
					return err
				}
				if t.qty > have {
					label, _ := productLabel(ctx, tx, op.factoryID, t.productID)
					return httpx.Biz("%s 現有 %g，不足以沖銷 %g（可能已被使用）", label, have, t.qty)
				}
			}
			if err := op.post(ctx, tx, "REVERSAL", t.warehouseID, t.productID, -t.qty, t.woID, "reversal", note, &leg); err != nil {
				return err
			}
			if t.legType == "ISSUE" && t.woID != nil {
				if err := addIssued(ctx, tx, *t.woID, t.productID, t.qty); err != nil { // t.qty 為負，扣回已發
					return err
				}
			}
		}
		return nil
	})
}

type woMaterial struct {
	ID           uuid.UUID `json:"id"`
	ProductID    uuid.UUID `json:"productId"`
	ItemNo       string    `json:"itemNo"`
	ItemName     string    `json:"itemName"`
	Unit         string    `json:"unit"`
	Category     string    `json:"category"`
	MaterialType string    `json:"materialType"`
	DeductTiming string    `json:"deductTiming"`
	StationName  *string   `json:"consumeStationName"`
	RequiredQty  float64   `json:"requiredQty"`
	IssuedQty    float64   `json:"issuedQty"`
	OnHand       float64   `json:"onHand"` // 全廠庫存
}

// workOrderMaterials 製令用料與發料狀況（製令發料畫面用）。
func (h *Handler) workOrderMaterials(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	rows, err := h.pool.Query(c, `
		SELECT m.id, p.id, p.item_no, p.name, p.unit, p.category, m.material_type, m.deduct_timing, s.name,
		       m.required_qty, m.issued_qty,
		       COALESCE((SELECT sum(b.qty) FROM inventory_balances b JOIN warehouses w ON w.id = b.warehouse_id
		                 WHERE b.product_id = p.id AND w.factory_id = $2), 0)
		FROM work_order_materials m
		JOIN work_orders wo ON wo.id = m.work_order_id AND wo.factory_id = $2
		JOIN products p ON p.id = m.product_id
		LEFT JOIN work_stations s ON s.id = m.consume_station_id
		WHERE m.work_order_id = $1
		ORDER BY m.deduct_timing DESC, p.item_no`, id, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[woMaterial])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
