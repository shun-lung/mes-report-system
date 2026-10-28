// Package purchasing 採購：供應商、採購單（草稿 → 下單 → 部分到貨／全部到貨）、採購收料。
// 缺料時由製令開立流程呼叫 AddShortage 自動建立或累加採購單草稿。
package purchasing

import (
	"context"
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

// AddShortage 將缺料加入該料件預設供應商的「缺料自動產生」草稿採購單：
// 同供應商合併成一張，同料件累加數量。回傳採購單 id 與單號。
func AddShortage(ctx context.Context, tx pgx.Tx, factoryID, productID uuid.UUID, qty float64, woID *uuid.UUID, userID uuid.UUID, at time.Time) (uuid.UUID, string, error) {
	var supplierID *uuid.UUID
	var leadDays int
	if err := tx.QueryRow(ctx, `
		SELECT p.default_supplier_id, COALESCE(s.lead_days, 7)
		FROM products p LEFT JOIN suppliers s ON s.id = p.default_supplier_id
		WHERE p.id = $1 AND p.factory_id = $2`, productID, factoryID).Scan(&supplierID, &leadDays); err != nil {
		return uuid.Nil, "", err
	}
	var poID uuid.UUID
	var poNo string
	err := tx.QueryRow(ctx, `
		SELECT id, po_no FROM purchase_orders
		WHERE factory_id = $1 AND status = 'DRAFT' AND source = 'SHORTAGE' AND supplier_id IS NOT DISTINCT FROM $2
		ORDER BY created_at LIMIT 1 FOR UPDATE`, factoryID, supplierID).Scan(&poID, &poNo)
	if err == pgx.ErrNoRows {
		day, err := org.LocalDate(ctx, tx, factoryID, at)
		if err != nil {
			return uuid.Nil, "", err
		}
		if poNo, err = docno.Next(ctx, tx, factoryID, "PO", day); err != nil {
			return uuid.Nil, "", err
		}
		poID = httpx.NewID()
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_orders (id, factory_id, po_no, supplier_id, source, expected_date, note, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'SHORTAGE', $5, '缺料自動產生，請確認數量與供應商後下單', $6, $7, $7)`,
			poID, factoryID, poNo, supplierID, day.AddDate(0, 0, leadDays), userID, at); err != nil {
			return uuid.Nil, "", err
		}
	} else if err != nil {
		return uuid.Nil, "", err
	}

	note := ""
	if woID != nil {
		if err := tx.QueryRow(ctx, `SELECT '缺料：' || wo_no FROM work_orders WHERE id = $1`, *woID).Scan(&note); err != nil {
			return uuid.Nil, "", err
		}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE purchase_order_lines SET qty = qty + $3, note = concat_ws('、', note, NULLIF($4, ''))
		WHERE po_id = $1 AND product_id = $2`, poID, productID, qty, note)
	if err != nil {
		return uuid.Nil, "", err
	}
	if tag.RowsAffected() == 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_order_lines (id, po_id, line_no, product_id, qty, work_order_id, note)
			SELECT $1, $2, COALESCE(max(line_no), 0) + 1, $3, $4, $5, NULLIF($6, '')
			FROM purchase_order_lines WHERE po_id = $2`, httpx.NewID(), poID, productID, qty, woID, note); err != nil {
			return uuid.Nil, "", err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE purchase_orders SET updated_at = $2 WHERE id = $1`, poID, at)
	return poID, poNo, err
}

type LineInput struct {
	ProductID   uuid.UUID  `json:"productId" binding:"required"`
	Qty         float64    `json:"qty" binding:"gt=0"`
	WorkOrderID *uuid.UUID `json:"workOrderId"`
	Note        string     `json:"note" binding:"max=200"`
}

type POInput struct {
	SupplierID   *uuid.UUID  `json:"supplierId"`
	ExpectedDate string      `json:"expectedDate"` // YYYY-MM-DD，可空
	Note         string      `json:"note" binding:"max=500"`
	Lines        []LineInput `json:"lines" binding:"required,min=1,dive"`
}

func (in POInput) expected() (*time.Time, error) {
	if in.ExpectedDate == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, in.ExpectedDate)
	if err != nil {
		return nil, httpx.Biz("預計到貨日格式錯誤")
	}
	return &d, nil
}

func checkSupplier(ctx context.Context, tx pgx.Tx, factoryID uuid.UUID, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1 AND factory_id = $2 AND is_active)`, *id, factoryID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return httpx.Biz("供應商不存在或已停用")
	}
	return nil
}

func writeLines(ctx context.Context, tx pgx.Tx, factoryID, poID uuid.UUID, lines []LineInput) error {
	seen := map[uuid.UUID]bool{}
	for i, l := range lines {
		if seen[l.ProductID] {
			return httpx.Biz("第 %d 行料件重複，請合併數量", i+1)
		}
		seen[l.ProductID] = true
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1 AND factory_id = $2)`, l.ProductID, factoryID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.Biz("第 %d 行料件不存在", i+1)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_order_lines (id, po_id, line_no, product_id, qty, work_order_id, note)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))`, httpx.NewID(), poID, i+1, l.ProductID, l.Qty, l.WorkOrderID, l.Note); err != nil {
			return err
		}
	}
	return nil
}

// Create 手動建立採購單草稿。
func (s *Service) Create(ctx context.Context, factoryID uuid.UUID, in POInput, userID uuid.UUID, at time.Time) (uuid.UUID, string, error) {
	id := httpx.NewID()
	var no string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := checkSupplier(ctx, tx, factoryID, in.SupplierID); err != nil {
			return err
		}
		exp, err := in.expected()
		if err != nil {
			return err
		}
		day, err := org.LocalDate(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		if no, err = docno.Next(ctx, tx, factoryID, "PO", day); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_orders (id, factory_id, po_no, supplier_id, expected_date, note, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $8)`, id, factoryID, no, in.SupplierID, exp, in.Note, userID, at); err != nil {
			return err
		}
		return writeLines(ctx, tx, factoryID, id, in.Lines)
	})
	return id, no, err
}

// UpdateDraft 修改草稿採購單（明細整批取代）。
func (s *Service) UpdateDraft(ctx context.Context, factoryID, poID uuid.UUID, in POInput, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockStatus(ctx, tx, factoryID, poID, "DRAFT"); err != nil {
			return err
		}
		if err := checkSupplier(ctx, tx, factoryID, in.SupplierID); err != nil {
			return err
		}
		exp, err := in.expected()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE purchase_orders SET supplier_id = $2, expected_date = $3, note = NULLIF($4, ''), updated_at = $5 WHERE id = $1`,
			poID, in.SupplierID, exp, in.Note, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM purchase_order_lines WHERE po_id = $1`, poID); err != nil {
			return err
		}
		return writeLines(ctx, tx, factoryID, poID, in.Lines)
	})
}

func lockStatus(ctx context.Context, tx pgx.Tx, factoryID, poID uuid.UUID, allowed ...string) error {
	var status, no string
	err := tx.QueryRow(ctx, `SELECT status, po_no FROM purchase_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`, poID, factoryID).Scan(&status, &no)
	if err == pgx.ErrNoRows {
		return httpx.Biz("採購單不存在")
	}
	if err != nil {
		return err
	}
	for _, a := range allowed {
		if status == a {
			return nil
		}
	}
	return httpx.Biz("採購單 %s 目前狀態不可執行此操作", no)
}

// Order 下單：草稿 → 已下單；需已指定供應商。
func (s *Service) Order(ctx context.Context, factoryID, poID uuid.UUID, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockStatus(ctx, tx, factoryID, poID, "DRAFT"); err != nil {
			return err
		}
		var hasSupplier, hasLines bool
		if err := tx.QueryRow(ctx, `
			SELECT supplier_id IS NOT NULL, EXISTS (SELECT 1 FROM purchase_order_lines WHERE po_id = $1)
			FROM purchase_orders WHERE id = $1`, poID).Scan(&hasSupplier, &hasLines); err != nil {
			return err
		}
		if !hasSupplier {
			return httpx.Biz("請先指定供應商再下單")
		}
		if !hasLines {
			return httpx.Biz("採購單沒有明細")
		}
		_, err := tx.Exec(ctx, `UPDATE purchase_orders SET status = 'ORDERED', ordered_at = $2, updated_at = $2 WHERE id = $1`, poID, at)
		return err
	})
}

// Cancel 取消尚未到貨的採購單；由缺料自動產生者，相關預警回到待處理，重新提醒生管。
func (s *Service) Cancel(ctx context.Context, factoryID, poID uuid.UUID, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockStatus(ctx, tx, factoryID, poID, "DRAFT", "ORDERED"); err != nil {
			return err
		}
		var received bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM purchase_order_lines WHERE po_id = $1 AND received_qty > 0)`, poID).Scan(&received); err != nil {
			return err
		}
		if received {
			return httpx.Biz("已有到貨紀錄，不可取消（請先沖銷收料）")
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET status = 'CANCELLED', updated_at = $2 WHERE id = $1`, poID, at); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE material_alerts SET status = 'PENDING', created_po_id = NULL, decided_by = NULL, decided_at = NULL,
			       note = concat_ws('；', note, '採購單已取消，重新待處理')
			WHERE created_po_id = $1`, poID)
		return err
	})
}

type ReceiveLine struct {
	LineID uuid.UUID `json:"lineId" binding:"required"`
	Qty    float64   `json:"qty" binding:"gt=0"`
}

// Receive 採購收料：入庫並累計已到數量，更新採購單狀態。
func (s *Service) Receive(ctx context.Context, factoryID, poID, warehouseID uuid.UUID, lines []ReceiveLine, note string, userID uuid.UUID, at time.Time) (int, error) {
	posted := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockStatus(ctx, tx, factoryID, poID, "ORDERED", "PARTIAL"); err != nil {
			return err
		}
		var whOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM warehouses WHERE id = $1 AND factory_id = $2 AND is_active)`, warehouseID, factoryID).Scan(&whOK); err != nil {
			return err
		}
		if !whOK {
			return httpx.Biz("倉庫不存在或已停用")
		}
		var poNo string
		if err := tx.QueryRow(ctx, `SELECT po_no FROM purchase_orders WHERE id = $1`, poID).Scan(&poNo); err != nil {
			return err
		}
		_, workDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		for _, l := range lines {
			var productID uuid.UUID
			var remaining float64
			var itemNo string
			err := tx.QueryRow(ctx, `
				SELECT l.product_id, l.qty - l.received_qty, p.item_no FROM purchase_order_lines l JOIN products p ON p.id = l.product_id
				WHERE l.id = $1 AND l.po_id = $2 FOR UPDATE OF l`, l.LineID, poID).Scan(&productID, &remaining, &itemNo)
			if err == pgx.ErrNoRows {
				return httpx.Biz("採購單明細不存在")
			}
			if err != nil {
				return err
			}
			if l.Qty > remaining {
				return httpx.Biz("%s 未到量只有 %g，不可收 %g", itemNo, remaining, l.Qty)
			}
			lineID := l.LineID
			n := "採購收料 " + poNo
			if note != "" {
				n += "：" + note
			}
			if _, err := inventory.Post(ctx, tx, inventory.Txn{
				FactoryID: factoryID, Type: "RECEIPT", WarehouseID: warehouseID, ProductID: productID, Qty: l.Qty,
				RefType: "po_line", RefID: &lineID, At: at, WorkDate: workDate, UserID: userID, Note: n,
			}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_order_lines SET received_qty = received_qty + $2 WHERE id = $1`, l.LineID, l.Qty); err != nil {
				return err
			}
			posted++
		}
		return RefreshStatus(ctx, tx, poID, at)
	})
	return posted, err
}

// RefreshStatus 依到貨數量重算採購單狀態（收料與沖銷收料後使用）。
func RefreshStatus(ctx context.Context, tx pgx.Tx, poID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE purchase_orders SET status = CASE
			WHEN NOT EXISTS (SELECT 1 FROM purchase_order_lines WHERE po_id = $1 AND received_qty < qty) THEN 'RECEIVED'
			WHEN EXISTS (SELECT 1 FROM purchase_order_lines WHERE po_id = $1 AND received_qty > 0) THEN 'PARTIAL'
			ELSE 'ORDERED' END,
			updated_at = $2
		WHERE id = $1 AND status IN ('ORDERED','PARTIAL','RECEIVED')`, poID, at)
	return err
}
