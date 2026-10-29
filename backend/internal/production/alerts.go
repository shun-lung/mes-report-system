package production

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/httpx"
	"mes/internal/purchasing"
)

// availabilitySQL 計算料件可用量：
//
//	可用量 = 全廠庫存 + 在途（未完工的半成品製令＋未到貨的採購單）− 其他已開立製令的未扣料需求 × (1 + 緩衝%)
//
// 緩衝套用在全部需求上，與即時缺料清單（loadShortages）的淨缺口一致：缺口 = 本張需求×(1+緩衝%) − 可用量。
// 在途採購單含草稿，避免同一缺口重複請購。$1 = product_id，$2 = factory_id，$3 = 排除的製令（本張）
const availabilitySQL = `
	SELECT
	  COALESCE((SELECT sum(b.qty) FROM inventory_balances b JOIN warehouses w ON w.id = b.warehouse_id
	            WHERE b.product_id = $1 AND w.factory_id = $2), 0)
	+ COALESCE((SELECT sum(w.planned_qty - COALESCE((
	              SELECT sum(fp.good_qty) FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
	              WHERE c.work_order_id = w.id
	                AND fp.step_seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = w.routing_id)), 0))
	            FROM work_orders w
	            WHERE w.product_id = $1 AND w.factory_id = $2 AND w.status IN ('DRAFT','RELEASED','SCHEDULED','IN_PROGRESS')), 0)
	+ COALESCE((SELECT sum(l.qty - l.received_qty) FROM purchase_order_lines l JOIN purchase_orders po ON po.id = l.po_id
	            WHERE l.product_id = $1 AND po.factory_id = $2 AND po.status IN ('DRAFT','ORDERED','PARTIAL')), 0)
	- COALESCE((SELECT sum(m.required_qty - m.issued_qty) FROM work_order_materials m JOIN work_orders w ON w.id = m.work_order_id
	            WHERE m.product_id = $1 AND w.factory_id = $2 AND w.id <> $3
	              AND w.status IN ('RELEASED','SCHEDULED','IN_PROGRESS') AND m.required_qty > m.issued_qty), 0)
	  * (1 + (SELECT alert_pct FROM products WHERE id = $1) / 100)`

type materialNeed struct {
	WOMaterialID uuid.UUID
	ProductID    uuid.UUID
	AlertPct     float64
	Need         float64
	Purchasable  bool // 沒有生效途程＝外購料件
}

// checkMaterials 檢查製令各料件：可用量 < 未扣需求 × (1 + 緩衝%) 時：
//   - 外購料件（材料）自動加入採購單草稿，預警記為「已建立採購單」；
//   - 自製件（半成品）建立待處理預警，由生管決定開製令或 Pass。
//
// 兩者都通知有預警決策權限者。回傳新預警與自動產生的採購單號。
func checkMaterials(ctx context.Context, tx pgx.Tx, woID, userID uuid.UUID, at time.Time) ([]uuid.UUID, []string, error) {
	var factoryID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT factory_id FROM work_orders WHERE id = $1`, woID).Scan(&factoryID); err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.product_id, p.alert_pct, m.required_qty - m.issued_qty,
		       NOT EXISTS (SELECT 1 FROM routings r WHERE r.product_id = p.id AND r.is_active)
		FROM work_order_materials m JOIN products p ON p.id = m.product_id
		WHERE m.work_order_id = $1 AND m.required_qty > m.issued_qty`, woID)
	if err != nil {
		return nil, nil, err
	}
	needs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[materialNeed])
	if err != nil {
		return nil, nil, err
	}

	created := []uuid.UUID{}
	pos := []string{}
	for _, n := range needs {
		var available float64
		if err := tx.QueryRow(ctx, availabilitySQL, n.ProductID, factoryID, woID).Scan(&available); err != nil {
			return nil, nil, err
		}
		threshold := round4(n.Need * (1 + n.AlertPct/100))
		if available >= threshold {
			continue
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM material_alerts WHERE wo_material_id = $1 AND status = 'PENDING')`,
			n.WOMaterialID).Scan(&exists); err != nil {
			return nil, nil, err
		}
		if exists {
			continue
		}

		id := httpx.NewID()
		status, title := "PENDING", "缺料預警："
		var poID *uuid.UUID
		var decidedAt *time.Time
		var decidedBy *uuid.UUID
		note := ""
		if n.Purchasable {
			pid, poNo, err := purchasing.AddShortage(ctx, tx, factoryID, n.ProductID, math.Ceil(threshold-available), &woID, userID, at)
			if err != nil {
				return nil, nil, err
			}
			status, title, note = "CREATED_PO", "缺料已自動請購：", "自動加入採購單 "+poNo
			poID, decidedAt, decidedBy = &pid, &at, &userID
			pos = appendUnique(pos, poNo)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO material_alerts (id, factory_id, work_order_id, wo_material_id, product_id,
				required_qty, available_qty, threshold_qty, status, created_po_id, decided_by, decided_at, note, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''), $14)`,
			id, factoryID, woID, n.WOMaterialID, n.ProductID, n.Need, available, threshold,
			status, poID, decidedBy, decidedAt, note, at); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (id, factory_id, user_id, type, title, body, ref_type, ref_id, created_at)
			SELECT gen_random_uuid(), $1, u.user_id, 'MATERIAL_SHORTAGE',
			       $8 || p.item_no || ' ' || p.name,
			       '製令 ' || w.wo_no || ' 需求 ' || $4::numeric || '，可用 ' || $5::numeric,
			       'material_alert', $2, $6
			FROM (SELECT DISTINCT ufr.user_id FROM user_factory_roles ufr
			      JOIN role_permissions rp ON rp.role_id = ufr.role_id
			      WHERE ufr.factory_id = $1 AND rp.permission_code = 'alert:decide') u,
			     products p, work_orders w
			WHERE p.id = $3 AND w.id = $7`,
			factoryID, id, n.ProductID, n.Need, available, at, woID, title); err != nil {
			return nil, nil, err
		}
		created = append(created, id)
	}
	return created, pos, nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

type DecideResult struct {
	Status   string     `json:"status"`
	NewWOID  *uuid.UUID `json:"newWorkOrderId,omitempty"`
	NewWONo  string     `json:"newWorkOrderNo,omitempty"`
	NewPONo  string     `json:"newPurchaseOrderNo,omitempty"`
	ShortQty float64    `json:"shortQty,omitempty"`
}

// DecideAlert 生管處理預警：CREATE_WO 依目前缺口建立草稿半成品製令；CREATE_PO 加入採購單草稿；PASS 僅結案。
func (s *Service) DecideAlert(ctx context.Context, factoryID, alertID uuid.UUID, action string, userID uuid.UUID, note string, at time.Time) (DecideResult, error) {
	var res DecideResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var woID, productID, woMaterialID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT status, work_order_id, product_id, wo_material_id FROM material_alerts
			WHERE id = $1 AND factory_id = $2 FOR UPDATE`, alertID, factoryID).
			Scan(&status, &woID, &productID, &woMaterialID)
		if err != nil {
			return err
		}
		if status != "PENDING" {
			return httpx.Biz("此預警已處理")
		}

		// 以決策當下重新計算缺口，避免多張預警重複補量
		shortage := func() (float64, int, *time.Time, error) {
			var need, pct float64
			var cardQty int
			var dueDate *time.Time
			if err := tx.QueryRow(ctx, `
				SELECT m.required_qty - m.issued_qty, p.alert_pct, p.card_qty, w.due_date
				FROM work_order_materials m
				JOIN products p ON p.id = m.product_id
				JOIN work_orders w ON w.id = m.work_order_id
				WHERE m.id = $1`, woMaterialID).Scan(&need, &pct, &cardQty, &dueDate); err != nil {
				return 0, 0, nil, err
			}
			var available float64
			if err := tx.QueryRow(ctx, availabilitySQL, productID, factoryID, woID).Scan(&available); err != nil {
				return 0, 0, nil, err
			}
			return need*(1+pct/100) - available, cardQty, dueDate, nil
		}
		var poID *uuid.UUID
		switch action {
		case "PASS":
			res.Status = "PASSED"
		case "CREATE_WO":
			short, cardQty, dueDate, err := shortage()
			if err != nil {
				return err
			}
			if short <= 0 {
				return httpx.Biz("目前可用量已足夠（含在途製令與採購），不需建單，請選擇 Pass")
			}
			qty := math.Ceil(short/float64(cardQty)) * float64(cardQty)
			var ctl Control
			if err := tx.QueryRow(ctx, `SELECT require_auth, require_labor FROM work_orders WHERE id = $1`, woID).
				Scan(&ctl.RequireAuth, &ctl.RequireLabor); err != nil {
				return err
			}
			id, no, err := createWorkOrder(ctx, tx, NewWorkOrder{
				FactoryID: factoryID, ProductID: productID, Qty: qty, DueDate: dueDate,
				Source: "SHORTAGE", ParentWOID: &woID, Control: ctl, UserID: userID, At: at,
			})
			if err != nil {
				return err
			}
			res.Status, res.NewWOID, res.NewWONo, res.ShortQty = "CREATED_WO", &id, no, round4(short)
		case "CREATE_PO":
			short, _, _, err := shortage()
			if err != nil {
				return err
			}
			if short <= 0 {
				return httpx.Biz("目前可用量已足夠（含在途製令與採購），不需請購，請選擇 Pass")
			}
			pid, no, err := purchasing.AddShortage(ctx, tx, factoryID, productID, math.Ceil(short), &woID, userID, at)
			if err != nil {
				return err
			}
			poID = &pid
			res.Status, res.NewPONo, res.ShortQty = "CREATED_PO", no, round4(short)
		default:
			return httpx.Biz("action 必須為 CREATE_WO、CREATE_PO 或 PASS")
		}

		_, err = tx.Exec(ctx, `
			UPDATE material_alerts SET status = $2, created_wo_id = $3, created_po_id = $4, decided_by = $5, decided_at = $6, note = NULLIF($7, '')
			WHERE id = $1`, alertID, res.Status, res.NewWOID, poID, userID, at, note)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE notifications SET read_at = $2 WHERE ref_type = 'material_alert' AND ref_id = $1 AND read_at IS NULL`,
			alertID, at)
		return err
	})
	return res, err
}
