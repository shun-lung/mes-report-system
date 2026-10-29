package production

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/httpx"
	"mes/internal/org"
)

// StartLabor 線別上工；同線若有進行中的上工會先自動下工（換製令）。
func (s *Service) StartLabor(ctx context.Context, factoryID, lineID, woID uuid.UUID, headcount int, userID uuid.UUID, at time.Time) (uuid.UUID, error) {
	id := httpx.NewID()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM work_orders WHERE id = $1 AND factory_id = $2`, woID, factoryID).Scan(&status)
		if err != nil {
			return err
		}
		if status != "RELEASED" && status != "IN_PROGRESS" {
			return httpx.Biz("製令狀態為 %s，不可上工", status)
		}
		if headcount <= 0 {
			return httpx.Biz("人數必須大於 0")
		}
		if err := endLabor(ctx, tx, factoryID, lineID, userID, at); err != nil {
			return err
		}
		shiftID, workDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO line_labor_sessions (id, factory_id, line_id, work_order_id, headcount, started_at,
				work_date, shift_id, started_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, factoryID, lineID, woID, headcount, at, workDate, shiftID, userID)
		return err
	})
	return id, err
}

// EndLabor 線別下工。
func (s *Service) EndLabor(ctx context.Context, factoryID, lineID, userID uuid.UUID, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return endLabor(ctx, tx, factoryID, lineID, userID, at)
	})
}

func endLabor(ctx context.Context, tx pgx.Tx, factoryID, lineID, userID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE line_labor_sessions SET ended_at = $4, ended_by = $3
		WHERE factory_id = $1 AND line_id = $2 AND ended_at IS NULL AND started_at < $4`,
		factoryID, lineID, userID, at)
	return err
}

type LaborItem struct {
	LineID      uuid.UUID `json:"lineId" binding:"required"`
	WorkOrderID uuid.UUID `json:"workOrderId" binding:"required"`
	Headcount   int       `json:"headcount"`
}

// StartLaborBatch 整批上工：每條線各上工一張製令（同一條線同時只能一張），單一交易；線上原有的製令會先下工。
func (s *Service) StartLaborBatch(ctx context.Context, factoryID uuid.UUID, items []LaborItem, userID uuid.UUID, at time.Time) error {
	if len(items) == 0 {
		return httpx.Biz("請至少選一張製令")
	}
	seen := map[uuid.UUID]bool{}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		shiftID, workDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		for _, it := range items {
			var lineCode, woNo, status string
			err := tx.QueryRow(ctx, `
				SELECT l.code, w.wo_no, w.status FROM production_lines l, work_orders w
				WHERE l.id = $1 AND l.factory_id = $3 AND l.is_active AND w.id = $2 AND w.factory_id = $3`,
				it.LineID, it.WorkOrderID, factoryID).Scan(&lineCode, &woNo, &status)
			if err == pgx.ErrNoRows {
				return httpx.Biz("線別或製令不存在")
			}
			if err != nil {
				return err
			}
			if seen[it.LineID] {
				return httpx.Biz("線別 %s 被指派了兩張製令；同一條線同時只能上工一張", lineCode)
			}
			seen[it.LineID] = true
			if status != "RELEASED" && status != "IN_PROGRESS" {
				return httpx.Biz("製令 %s 尚未開立或已結案，不可上工", woNo)
			}
			if it.Headcount <= 0 {
				return httpx.Biz("%s 的人數必須大於 0", lineCode)
			}
			if err := endLabor(ctx, tx, factoryID, it.LineID, userID, at); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO line_labor_sessions (id, factory_id, line_id, work_order_id, headcount, started_at,
					work_date, shift_id, started_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				httpx.NewID(), factoryID, it.LineID, it.WorkOrderID, it.Headcount, at, workDate, shiftID, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

// EndLaborByWorkOrders 整批下工：結束這些製令在各線別進行中的上工，回傳下工的線別數。
func (s *Service) EndLaborByWorkOrders(ctx context.Context, factoryID uuid.UUID, woIDs []uuid.UUID, userID uuid.UUID, at time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE line_labor_sessions SET ended_at = $4, ended_by = $3
		WHERE factory_id = $1 AND work_order_id = ANY ($2) AND ended_at IS NULL AND started_at < $4`,
		factoryID, woIDs, userID, at)
	return int(tag.RowsAffected()), err
}
