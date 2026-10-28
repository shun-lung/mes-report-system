package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/docno"
	"mes/internal/httpx"
)

type Txn struct {
	FactoryID   uuid.UUID
	Type        string // RECEIPT / ISSUE / STATION_IN / STATION_OUT / CONSUME / FG_IN / ADJUST ...
	WarehouseID uuid.UUID
	ProductID   uuid.UUID
	Qty         float64 // 正入負出
	WorkOrderID *uuid.UUID
	RefType     string
	RefID       *uuid.UUID
	ReversalOf  *uuid.UUID // 沖銷時指向被沖銷的異動
	At          time.Time
	WorkDate    time.Time
	UserID      uuid.UUID
	Note        string
}

// Post 寫入異動流水並更新庫存餘額，必須在呼叫端的交易內執行。
// 不阻擋負庫存：現場不能因帳務停線，負庫存由報表與預警揭露。
func Post(ctx context.Context, tx pgx.Tx, t Txn) (uuid.UUID, error) {
	var balance float64
	err := tx.QueryRow(ctx, `
		INSERT INTO inventory_balances (warehouse_id, product_id, qty, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (warehouse_id, product_id)
		DO UPDATE SET qty = inventory_balances.qty + EXCLUDED.qty, updated_at = EXCLUDED.updated_at
		RETURNING qty`, t.WarehouseID, t.ProductID, t.Qty, t.At).Scan(&balance)
	if err != nil {
		return uuid.Nil, err
	}
	no, err := docno.Next(ctx, tx, t.FactoryID, "TX", t.WorkDate)
	if err != nil {
		return uuid.Nil, err
	}
	id := httpx.NewID()
	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_transactions
			(id, factory_id, txn_no, txn_type, warehouse_id, product_id, qty, balance_after,
			 work_order_id, ref_type, ref_id, reversal_of, occurred_at, work_date, user_id, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, $15, NULLIF($16, ''))`,
		id, t.FactoryID, no, t.Type, t.WarehouseID, t.ProductID, t.Qty, balance,
		t.WorkOrderID, t.RefType, t.RefID, t.ReversalOf, t.At, t.WorkDate, t.UserID, t.Note)
	return id, err
}

// Warehouse 取得工廠中指定類型（RAW / WIP / FG）的預設倉。
func Warehouse(ctx context.Context, tx pgx.Tx, factoryID uuid.UUID, typ string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM warehouses WHERE factory_id = $1 AND type = $2 AND is_active ORDER BY code LIMIT 1`,
		factoryID, typ).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, fmt.Errorf("工廠尚未設定 %s 倉", typ)
	}
	return id, err
}

// WarehouseTypeFor 依料件類別決定存放倉：半成品進暫存倉，成品進成品倉，其餘為材料倉。
func WarehouseTypeFor(category string) string {
	switch category {
	case "SFG":
		return "WIP"
	case "FG":
		return "FG"
	default:
		return "RAW"
	}
}
