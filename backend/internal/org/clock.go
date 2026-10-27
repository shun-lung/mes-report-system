package org

import (
	"context"
	"time"

	"github.com/google/uuid"

	"mes/internal/database"
)

// Location 取得工廠時區。
func Location(ctx context.Context, q database.Querier, factoryID uuid.UUID) (*time.Location, error) {
	var tz string
	if err := q.QueryRow(ctx, `SELECT timezone FROM factories WHERE id = $1`, factoryID).Scan(&tz); err != nil {
		return nil, err
	}
	return time.LoadLocation(tz)
}

// Clock 依工廠時區與班別設定，算出時間點 t 所屬的班別與生產日。
func Clock(ctx context.Context, q database.Querier, factoryID uuid.UUID, t time.Time) (uuid.UUID, time.Time, error) {
	loc, err := Location(ctx, q, factoryID)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, to_char(start_time, 'HH24:MI:SS'), work_date_offset
		FROM shifts WHERE factory_id = $1 AND is_active`, factoryID)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	defer rows.Close()
	var rules []ShiftRule
	for rows.Next() {
		var r ShiftRule
		var start string
		if err := rows.Scan(&r.ID, &start, &r.WorkDateOffset); err != nil {
			return uuid.Nil, time.Time{}, err
		}
		if r.Start, err = ParseClock(start); err != nil {
			return uuid.Nil, time.Time{}, err
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return ResolveWorkDate(rules, t, loc)
}

// LocalDate 取得 t 在工廠當地的日曆日期（單號用，不同於生產日）。
func LocalDate(ctx context.Context, q database.Querier, factoryID uuid.UUID, t time.Time) (time.Time, error) {
	loc, err := Location(ctx, q, factoryID)
	if err != nil {
		return time.Time{}, err
	}
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC), nil
}
