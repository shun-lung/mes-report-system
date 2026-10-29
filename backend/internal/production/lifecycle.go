package production

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/httpx"
)

// 製令與訂單的建立、修改、取消。

// CreateWorkOrder 手動建立製令（不經客戶訂單，例如備庫存、補做），狀態為草稿。
func (s *Service) CreateWorkOrder(ctx context.Context, factoryID, productID uuid.UUID, qty float64, due *time.Time, ctl Control, userID uuid.UUID, at time.Time) (uuid.UUID, string, error) {
	var id uuid.UUID
	var no string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var category string
		err := tx.QueryRow(ctx, `SELECT category FROM products WHERE id = $1 AND factory_id = $2`, productID, factoryID).Scan(&category)
		if err == pgx.ErrNoRows {
			return httpx.Biz("產品不存在")
		}
		if err != nil {
			return err
		}
		if category != "FG" && category != "SFG" {
			return httpx.Biz("只有成品或半成品可以開製令")
		}
		id, no, err = createWorkOrder(ctx, tx, NewWorkOrder{
			FactoryID: factoryID, ProductID: productID, Qty: qty, DueDate: due, Source: "MANUAL", Control: ctl, UserID: userID, At: at,
		})
		return err
	})
	return id, no, err
}

// UpdateDraft 修改草稿製令的數量、交期與管制選項；用料需求依數量等比例重算。
func (s *Service) UpdateDraft(ctx context.Context, factoryID, woID uuid.UUID, qty float64, due *time.Time, ctl Control, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var oldQty float64
		if err := tx.QueryRow(ctx, `SELECT status, planned_qty FROM work_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			woID, factoryID).Scan(&status, &oldQty); err != nil {
			return err
		}
		if status != "DRAFT" {
			return httpx.Biz("只有草稿製令可以修改")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE work_orders SET planned_qty = $2, due_date = $3, require_auth = $4, require_labor = $5, require_issue = $7, updated_at = $6 WHERE id = $1`,
			woID, qty, due, ctl.RequireAuth, ctl.RequireLabor, at, ctl.RequireIssue); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE work_order_materials SET required_qty = round(required_qty * $2 / $3, 4) WHERE work_order_id = $1`,
			woID, qty, oldQty)
		return err
	})
}

// CancelWorkOrder 取消尚未開始生產的製令：沒有掃描、上工、發料或扣帳紀錄。
// 流程卡作廢、待處理的缺料預警結案，所屬訂單狀態重算。
func (s *Service) CancelWorkOrder(ctx context.Context, factoryID, woID, userID uuid.UUID, reason string, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status, woNo string
		if err := tx.QueryRow(ctx, `SELECT status, wo_no FROM work_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			woID, factoryID).Scan(&status, &woNo); err != nil {
			return err
		}
		if status != "DRAFT" && status != "RELEASED" {
			return httpx.Biz("製令 %s 已開始生產或已結束，不可取消", woNo)
		}
		var started bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM production_scans WHERE work_order_id = $1)
			    OR EXISTS (SELECT 1 FROM line_labor_sessions WHERE work_order_id = $1)
			    OR EXISTS (SELECT 1 FROM inventory_transactions WHERE work_order_id = $1)`, woID).Scan(&started); err != nil {
			return err
		}
		if started {
			return httpx.Biz("製令 %s 已有上工、掃描或發料紀錄，不可取消（發料可先到庫存歷程沖銷）", woNo)
		}
		if _, err := tx.Exec(ctx, `UPDATE work_orders SET status = 'CANCELLED', updated_at = $2 WHERE id = $1`, woID, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE flow_cards SET status = 'VOID' WHERE work_order_id = $1`, woID); err != nil {
			return err
		}
		note := "製令取消"
		if reason != "" {
			note += "：" + reason
		}
		if _, err := tx.Exec(ctx, `
			UPDATE material_alerts SET status = 'PASSED', decided_by = $2, decided_at = $3, note = $4
			WHERE work_order_id = $1 AND status = 'PENDING'`, woID, userID, at, note); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE notifications SET read_at = $2
			WHERE ref_type = 'material_alert' AND read_at IS NULL
			  AND ref_id IN (SELECT id FROM material_alerts WHERE work_order_id = $1)`, woID, at); err != nil {
			return err
		}
		return refreshSalesOrder(ctx, tx, woID, at)
	})
}

// refreshSalesOrder 依製令狀態重算所屬訂單狀態（取消製令後使用）。
func refreshSalesOrder(ctx context.Context, tx pgx.Tx, woID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE sales_orders so SET
			status = CASE
				WHEN NOT EXISTS (SELECT 1 FROM sales_order_lines l JOIN work_orders w ON w.sales_order_line_id = l.id
				                 WHERE l.sales_order_id = so.id AND w.status <> 'CANCELLED') THEN 'OPEN'
				WHEN NOT EXISTS (SELECT 1 FROM sales_order_lines l JOIN work_orders w ON w.sales_order_line_id = l.id
				                 WHERE l.sales_order_id = so.id AND w.status NOT IN ('COMPLETED','CLOSED','CANCELLED')) THEN 'COMPLETED'
				WHEN EXISTS (SELECT 1 FROM sales_order_lines l JOIN work_orders w ON w.sales_order_line_id = l.id
				             WHERE l.sales_order_id = so.id AND w.status IN ('IN_PROGRESS','COMPLETED','CLOSED')) THEN 'IN_PRODUCTION'
				ELSE 'OPEN' END,
			updated_at = $2
		WHERE so.status <> 'CANCELLED'
		  AND so.id = (SELECT l.sales_order_id FROM work_orders w JOIN sales_order_lines l ON l.id = w.sales_order_line_id WHERE w.id = $1)`,
		woID, at)
	return err
}

// activeWorkOrders 訂單底下未取消的製令數。
func activeWorkOrders(ctx context.Context, tx pgx.Tx, soID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM work_orders w JOIN sales_order_lines l ON l.id = w.sales_order_line_id
		WHERE l.sales_order_id = $1 AND w.status <> 'CANCELLED'`, soID).Scan(&n)
	return n, err
}

// UpdateSalesOrder 修改訂單；已有未取消的製令時不可修改。
func (s *Service) UpdateSalesOrder(ctx context.Context, factoryID, soID uuid.UUID, in SalesOrderInput, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM sales_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			soID, factoryID).Scan(&status); err != nil {
			return err
		}
		if status == "CANCELLED" {
			return httpx.Biz("訂單已取消")
		}
		n, err := activeWorkOrders(ctx, tx, soID)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Biz("訂單已轉製令，請先取消製令再修改")
		}
		if err := checkCustomer(ctx, tx, factoryID, in.CustomerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE sales_orders SET customer_id = $2, customer_po = NULLIF($3, ''), note = NULLIF($4, ''), updated_at = $5
			WHERE id = $1`, soID, in.CustomerID, in.CustomerPO, in.Note, at); err != nil {
			return err
		}
		// 已取消的製令保留，但解除與舊明細的關聯
		if _, err := tx.Exec(ctx, `
			UPDATE work_orders SET sales_order_line_id = NULL
			WHERE sales_order_line_id IN (SELECT id FROM sales_order_lines WHERE sales_order_id = $1)`, soID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sales_order_lines WHERE sales_order_id = $1`, soID); err != nil {
			return err
		}
		return insertLines(ctx, tx, factoryID, soID, in.Lines)
	})
}

// CancelSalesOrder 取消訂單；需先取消所有製令。
func (s *Service) CancelSalesOrder(ctx context.Context, factoryID, soID uuid.UUID, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM sales_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			soID, factoryID).Scan(&status); err != nil {
			return err
		}
		if status == "CANCELLED" {
			return httpx.Biz("訂單已取消")
		}
		n, err := activeWorkOrders(ctx, tx, soID)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Biz("訂單還有 %d 張製令未取消", n)
		}
		_, err = tx.Exec(ctx, `UPDATE sales_orders SET status = 'CANCELLED', updated_at = $2 WHERE id = $1`, soID, at)
		return err
	})
}

func checkCustomer(ctx context.Context, tx pgx.Tx, factoryID, customerID uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1 AND factory_id = $2 AND is_active)`,
		customerID, factoryID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return httpx.Biz("客戶不存在或已停用")
	}
	return nil
}

// insertLines 寫入訂單明細；品項須為本廠成品。
func insertLines(ctx context.Context, tx pgx.Tx, factoryID, soID uuid.UUID, lines []SalesOrderLineInput) error {
	for i, l := range lines {
		due, err := time.Parse(time.DateOnly, l.DueDate)
		if err != nil {
			return httpx.Biz("交期格式錯誤：%s", l.DueDate)
		}
		var category string
		err = tx.QueryRow(ctx, `SELECT category FROM products WHERE id = $1 AND factory_id = $2`, l.ProductID, factoryID).Scan(&category)
		if err == pgx.ErrNoRows {
			return httpx.Biz("第 %d 行品項不存在", i+1)
		}
		if err != nil {
			return err
		}
		if category != "FG" {
			return httpx.Biz("第 %d 行：客戶訂單只能訂成品", i+1)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sales_order_lines (id, sales_order_id, line_no, product_id, qty, due_date)
			VALUES ($1, $2, $3, $4, $5, $6)`, httpx.NewID(), soID, i+1, l.ProductID, l.Qty, due); err != nil {
			return err
		}
	}
	return nil
}
