package production

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mes/internal/docno"
	"mes/internal/httpx"
	"mes/internal/inventory"
	"mes/internal/org"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type NewWorkOrder struct {
	FactoryID        uuid.UUID
	ProductID        uuid.UUID
	Qty              float64
	SalesOrderLineID *uuid.UUID
	DueDate          *time.Time
	Source           string // MANUAL / SHORTAGE
	ParentWOID       *uuid.UUID
	Control          Control
	UserID           uuid.UUID
	At               time.Time
}

// Control 生管管制選項（預設皆不管制）。
type Control struct {
	RequireAuth  bool `json:"requireAuth"`  // 只有被授權該線別的人能回報
	RequireLabor bool `json:"requireLabor"` // 須先上工本製令才能回報
}

// createWorkOrder 建立 DRAFT 製令單，鎖定生效途程並複製 BOM 用料。
func createWorkOrder(ctx context.Context, tx pgx.Tx, in NewWorkOrder) (uuid.UUID, string, error) {
	var routingID uuid.UUID
	var cardQty int
	err := tx.QueryRow(ctx, `
		SELECT r.id, p.card_qty FROM products p
		JOIN routings r ON r.product_id = p.id AND r.is_active
		WHERE p.id = $1 AND p.factory_id = $2`, in.ProductID, in.FactoryID).Scan(&routingID, &cardQty)
	if err == pgx.ErrNoRows {
		return uuid.Nil, "", httpx.Biz("產品沒有生效的途程，無法開立製令")
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	day, err := org.LocalDate(ctx, tx, in.FactoryID, in.At)
	if err != nil {
		return uuid.Nil, "", err
	}
	woNo, err := docno.Next(ctx, tx, in.FactoryID, "WO", day)
	if err != nil {
		return uuid.Nil, "", err
	}
	source := in.Source
	if source == "" {
		source = "MANUAL"
	}
	id := httpx.NewID()
	_, err = tx.Exec(ctx, `
		INSERT INTO work_orders (id, factory_id, wo_no, product_id, routing_id, planned_qty, card_qty,
			source, parent_wo_id, sales_order_line_id, due_date, require_auth, require_labor, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)`,
		id, in.FactoryID, woNo, in.ProductID, routingID, in.Qty, cardQty,
		source, in.ParentWOID, in.SalesOrderLineID, in.DueDate, in.Control.RequireAuth, in.Control.RequireLabor, in.UserID, in.At)
	if err != nil {
		return uuid.Nil, "", err
	}

	rows, err := tx.Query(ctx, `
		SELECT b.id, b.child_product_id, b.material_type, b.qty_per, b.scrap_rate,
		       COALESCE(b.deduct_timing, p.default_deduct_timing), b.consume_station_id
		FROM product_bom b JOIN products p ON p.id = b.child_product_id
		WHERE b.parent_product_id = $1 ORDER BY b.sort_order`, in.ProductID)
	if err != nil {
		return uuid.Nil, "", err
	}
	type bomLine struct {
		bomID, productID     uuid.UUID
		materialType, timing string
		qtyPer, scrapRate    float64
		consumeStation       *uuid.UUID
	}
	lines, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (bomLine, error) {
		var b bomLine
		err := r.Scan(&b.bomID, &b.productID, &b.materialType, &b.qtyPer, &b.scrapRate, &b.timing, &b.consumeStation)
		return b, err
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	for _, b := range lines {
		required := round4(in.Qty * b.qtyPer * (1 + b.scrapRate/100))
		_, err := tx.Exec(ctx, `
			INSERT INTO work_order_materials (id, work_order_id, bom_id, product_id, material_type,
				qty_per, required_qty, deduct_timing, consume_station_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			httpx.NewID(), id, b.bomID, b.productID, b.materialType, b.qtyPer, required, b.timing, b.consumeStation)
		if err != nil {
			return uuid.Nil, "", err
		}
	}
	return id, woNo, nil
}

// ReleaseResult 為開立結果，含開立時觸發的缺料預警。
type ReleaseResult struct {
	WONo   string      `json:"woNo"`
	Cards  int         `json:"cards"`
	Alerts []uuid.UUID `json:"alerts"`
	// 缺料時自動建立／累加的採購單草稿
	PurchaseOrders []string `json:"purchaseOrders"`
}

// Release 開立製令：依每卡數量產生流程卡與各站進度，並檢查用料。
func (s *Service) Release(ctx context.Context, factoryID, woID, userID uuid.UUID, at time.Time) (ReleaseResult, error) {
	var res ReleaseResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var qty float64
		var cardQty int
		var routingID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT wo_no, status, planned_qty, card_qty, routing_id FROM work_orders
			WHERE id = $1 AND factory_id = $2 FOR UPDATE`, woID, factoryID).
			Scan(&res.WONo, &status, &qty, &cardQty, &routingID)
		if err != nil {
			return err
		}
		if status != "DRAFT" {
			return httpx.Biz("製令 %s 狀態為 %s，只有草稿可開立", res.WONo, status)
		}

		rows, err := tx.Query(ctx, `SELECT seq, station_id FROM routing_steps WHERE routing_id = $1 ORDER BY seq`, routingID)
		if err != nil {
			return err
		}
		type step struct {
			Seq     int
			Station uuid.UUID
		}
		steps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[step])
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			return httpx.Biz("途程沒有任何站點")
		}

		n := int(math.Ceil(qty / float64(cardQty)))
		for i := 1; i <= n; i++ {
			cq := math.Min(float64(cardQty), qty-float64(cardQty*(i-1)))
			cardID := httpx.NewID()
			_, err := tx.Exec(ctx, `
				INSERT INTO flow_cards (id, factory_id, work_order_id, card_no, qty, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				cardID, factoryID, woID, fmt.Sprintf("%s-%03d", res.WONo, i), cq, at)
			if err != nil {
				return err
			}
			for j, st := range steps {
				input := 0.0
				if j == 0 {
					input = cq
				}
				_, err := tx.Exec(ctx, `
					INSERT INTO flow_card_progress (card_id, step_seq, station_id, input_qty) VALUES ($1, $2, $3, $4)`,
					cardID, st.Seq, st.Station, input)
				if err != nil {
					return err
				}
			}
		}
		res.Cards = n

		if _, err := tx.Exec(ctx, `
			UPDATE work_orders SET status = 'RELEASED', released_at = $2, updated_at = $2 WHERE id = $1`, woID, at); err != nil {
			return err
		}
		res.Alerts, res.PurchaseOrders, err = checkMaterials(ctx, tx, woID, userID, at)
		return err
	})
	return res, err
}

// IssueResult 發料結果：實發明細、因庫存不足而少發或未發的料件，以及由現場掃描自動扣帳（不需發料）的料件。
type IssueResult struct {
	Issued    []IssueLine `json:"issued"`
	Short     []IssueLine `json:"short"`
	OnArrival []string    `json:"onArrival"`
}

type IssueLine struct {
	ItemNo string  `json:"itemNo"`
	Unit   string  `json:"unit"`
	Qty    float64 `json:"qty"`             // 實發量；Short 中為仍未發的量
	Stock  float64 `json:"stock,omitempty"` // Short：發料當下的庫存
}

// IssueMaterials 倉管發料：將製令「出庫扣帳」料件的未發數量自所屬倉扣帳。
// 庫存不足時依現有庫存調整發料量（不發成負數），其餘列為不足；到站扣帳料件由現場掃描自動扣帳，不在此發料。
func (s *Service) IssueMaterials(ctx context.Context, factoryID, woID, userID uuid.UUID, at time.Time) (IssueResult, error) {
	res := IssueResult{Issued: []IssueLine{}, Short: []IssueLine{}, OnArrival: []string{}}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM work_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			woID, factoryID).Scan(&status); err != nil {
			return err
		}
		if status != "RELEASED" && status != "IN_PROGRESS" {
			return httpx.Biz("製令狀態為 %s，無法發料", status)
		}
		_, workDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT m.id, m.product_id, p.item_no, p.unit, p.category, m.deduct_timing, m.required_qty - m.issued_qty
			FROM work_order_materials m JOIN products p ON p.id = m.product_id
			WHERE m.work_order_id = $1 AND m.required_qty > m.issued_qty
			ORDER BY p.item_no
			FOR UPDATE OF m`, woID)
		if err != nil {
			return err
		}
		type pending struct {
			ID, ProductID                  uuid.UUID
			ItemNo, Unit, Category, Timing string
			Qty                            float64
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
		if err != nil {
			return err
		}
		for _, m := range list {
			if m.Timing != "ON_ISSUE" {
				res.OnArrival = append(res.OnArrival, m.ItemNo)
				continue
			}
			wh, err := inventory.Warehouse(ctx, tx, factoryID, inventory.WarehouseTypeFor(m.Category))
			if err != nil {
				return err
			}
			var stock float64
			if err := tx.QueryRow(ctx, `
				SELECT COALESCE((SELECT qty FROM inventory_balances WHERE warehouse_id = $1 AND product_id = $2 FOR UPDATE), 0)`,
				wh, m.ProductID).Scan(&stock); err != nil {
				return err
			}
			qty := round4(math.Min(m.Qty, math.Max(stock, 0)))
			if qty < m.Qty {
				res.Short = append(res.Short, IssueLine{ItemNo: m.ItemNo, Unit: m.Unit, Qty: round4(m.Qty - qty), Stock: stock})
			}
			if qty <= 0 {
				continue
			}
			if _, err := inventory.Post(ctx, tx, inventory.Txn{
				FactoryID: factoryID, Type: "ISSUE", WarehouseID: wh, ProductID: m.ProductID, Qty: -qty,
				WorkOrderID: &woID, RefType: "wo_material", RefID: &m.ID, At: at, WorkDate: workDate, UserID: userID,
				Note: "製令發料",
			}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE work_order_materials SET issued_qty = issued_qty + $2 WHERE id = $1`, m.ID, qty); err != nil {
				return err
			}
			res.Issued = append(res.Issued, IssueLine{ItemNo: m.ItemNo, Unit: m.Unit, Qty: qty})
		}
		return nil
	})
	return res, err
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
