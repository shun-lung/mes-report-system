package production

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/docno"
	"mes/internal/httpx"
	"mes/internal/org"
)

type SalesOrderLineInput struct {
	ProductID uuid.UUID `json:"productId" binding:"required"`
	Qty       float64   `json:"qty" binding:"required,gt=0"`
	DueDate   string    `json:"dueDate" binding:"required"` // YYYY-MM-DD
}

type SalesOrderInput struct {
	CustomerID uuid.UUID             `json:"customerId" binding:"required"`
	CustomerPO string                `json:"customerPo"`
	Note       string                `json:"note"`
	Lines      []SalesOrderLineInput `json:"lines" binding:"required,min=1,dive"`
}

// CreateSalesOrder 建立客戶訂單。
func (s *Service) CreateSalesOrder(ctx context.Context, factoryID uuid.UUID, in SalesOrderInput, userID uuid.UUID, at time.Time) (uuid.UUID, string, error) {
	id := httpx.NewID()
	var soNo string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := checkCustomer(ctx, tx, factoryID, in.CustomerID); err != nil {
			return err
		}
		day, err := org.LocalDate(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		if soNo, err = docno.Next(ctx, tx, factoryID, "SO", day); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sales_orders (id, factory_id, so_no, customer_id, customer_po, order_date, note, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, NULLIF($7, ''), $8, $9, $9)`,
			id, factoryID, soNo, in.CustomerID, in.CustomerPO, day, in.Note, userID, at)
		if err != nil {
			return err
		}
		return insertLines(ctx, tx, factoryID, id, in.Lines)
	})
	return id, soNo, err
}

type ConvertedWorkOrder struct {
	ID   uuid.UUID `json:"id"`
	WONo string    `json:"woNo"`
}

// ConvertSalesOrder 將訂單中尚未轉製令的明細，各建立一張 DRAFT 製令。
func (s *Service) ConvertSalesOrder(ctx context.Context, factoryID, soID uuid.UUID, ctl Control, userID uuid.UUID, at time.Time) ([]ConvertedWorkOrder, error) {
	var list []ConvertedWorkOrder
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		list, err = convertOne(ctx, tx, factoryID, soID, ctl, userID, at)
		return err
	})
	return list, err
}

// ConvertSalesOrders 批量轉製令：多張訂單在同一交易內轉換，任一張失敗則全部還原。
func (s *Service) ConvertSalesOrders(ctx context.Context, factoryID uuid.UUID, soIDs []uuid.UUID, ctl Control, userID uuid.UUID, at time.Time) ([]ConvertedWorkOrder, error) {
	list := []ConvertedWorkOrder{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, id := range soIDs {
			got, err := convertOne(ctx, tx, factoryID, id, ctl, userID, at)
			if err != nil {
				return err
			}
			list = append(list, got...)
		}
		return nil
	})
	return list, err
}

// convertOne 將一張訂單尚未轉製令的明細各建立一張草稿製令（套用管制選項）。
func convertOne(ctx context.Context, tx pgx.Tx, factoryID, soID uuid.UUID, ctl Control, userID uuid.UUID, at time.Time) ([]ConvertedWorkOrder, error) {
	var list []ConvertedWorkOrder
	err := func() error {
		var status, soNo string
		if err := tx.QueryRow(ctx, `SELECT status, so_no FROM sales_orders WHERE id = $1 AND factory_id = $2 FOR UPDATE`,
			soID, factoryID).Scan(&status, &soNo); err != nil {
			return err
		}
		if status == "CANCELLED" || status == "CLOSED" {
			return httpx.Biz("訂單 %s 已%s", soNo, map[string]string{"CANCELLED": "取消", "CLOSED": "結案"}[status])
		}
		rows, err := tx.Query(ctx, `
			SELECT l.id, l.product_id, l.qty, l.due_date FROM sales_order_lines l
			WHERE l.sales_order_id = $1
			  AND NOT EXISTS (SELECT 1 FROM work_orders w WHERE w.sales_order_line_id = l.id AND w.status <> 'CANCELLED')
			ORDER BY l.line_no`, soID)
		if err != nil {
			return err
		}
		type line struct {
			ID, ProductID uuid.UUID
			Qty           float64
			Due           time.Time
		}
		lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[line])
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return httpx.Biz("訂單 %s 的明細皆已轉製令", soNo)
		}
		for _, l := range lines {
			id, no, err := createWorkOrder(ctx, tx, NewWorkOrder{
				FactoryID: factoryID, ProductID: l.ProductID, Qty: l.Qty, SalesOrderLineID: &l.ID,
				DueDate: &l.Due, Control: ctl, UserID: userID, At: at,
			})
			if err != nil {
				return err
			}
			list = append(list, ConvertedWorkOrder{ID: id, WONo: no})
		}
		return nil
	}()
	return list, err
}
