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
