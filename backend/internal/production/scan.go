package production

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/httpx"
	"mes/internal/inventory"
	"mes/internal/org"
)

type DefectInput struct {
	DefectCodeID uuid.UUID `json:"defectCodeId" binding:"required"`
	Qty          float64   `json:"qty" binding:"required,gt=0"`
	Disposition  string    `json:"disposition" binding:"required,oneof=REWORK SCRAP ACCEPT"`
}

type ScanInput struct {
	FactoryID uuid.UUID
	CardNo    string
	LineID    uuid.UUID
	GoodQty   float64
	Defects   []DefectInput
	UserID    uuid.UUID
	DeviceID  string
	At        time.Time
	// BypassAuth 為總部管理員，不受「人員授權」管制
	BypassAuth bool
}

type ScanResult struct {
	ScanID      uuid.UUID `json:"scanId"`
	WONo        string    `json:"woNo"`
	CardNo      string    `json:"cardNo"`
	Station     string    `json:"station"`
	StepSeq     int       `json:"stepSeq"`
	GoodQty     float64   `json:"goodQty"`
	DefectQty   float64   `json:"defectQty"`
	Remaining   float64   `json:"remaining"` // 本卡本站剩餘可報
	FinalStep   bool      `json:"finalStep"` // 末站：良品已入庫
	WOCompleted bool      `json:"woCompleted"`
	LaborEnded  bool      `json:"laborEnded"` // 本製令在本站已全數報完，本線自動下工
}

// Scan 現場掃描回報。單一交易內完成：
// 站點管制 → 寫掃描與不良 → 更新本站/下站進度 → 到站扣帳 → 末站入庫 → 小時匯總 → 卡片/製令/訂單狀態。
func (s *Service) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	res := ScanResult{CardNo: in.CardNo, GoodQty: in.GoodQty}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var cardID, woID, productID, routingID uuid.UUID
		var woStatus, category string
		var ctl Control
		err := tx.QueryRow(ctx, `
			SELECT c.id, w.id, w.wo_no, w.status, w.product_id, w.routing_id, p.category, w.require_auth, w.require_labor
			FROM flow_cards c
			JOIN work_orders w ON w.id = c.work_order_id
			JOIN products p ON p.id = w.product_id
			WHERE c.card_no = $1 AND c.factory_id = $2 AND c.status NOT IN ('VOID','SPLIT')
			FOR UPDATE OF c`, in.CardNo, in.FactoryID).
			Scan(&cardID, &woID, &res.WONo, &woStatus, &productID, &routingID, &category, &ctl.RequireAuth, &ctl.RequireLabor)
		if err == pgx.ErrNoRows {
			return httpx.Biz("找不到流程卡 %s", in.CardNo)
		}
		if err != nil {
			return err
		}
		if woStatus != "RELEASED" && woStatus != "IN_PROGRESS" {
			return httpx.Biz("製令 %s 狀態為 %s，不可回報", res.WONo, woStatus)
		}

		var stationID uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT l.station_id, s.name FROM production_lines l JOIN work_stations s ON s.id = l.station_id
			WHERE l.id = $1 AND l.factory_id = $2 AND l.is_active`, in.LineID, in.FactoryID).Scan(&stationID, &res.Station)
		if err == pgx.ErrNoRows {
			return httpx.Biz("線別不存在或已停用")
		}
		if err != nil {
			return err
		}
		if err := checkControl(ctx, tx, ctl, res.WONo, in.UserID, in.LineID, woID, in.BypassAuth); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT seq FROM routing_steps WHERE routing_id = $1 AND station_id = $2`,
			routingID, stationID).Scan(&res.StepSeq)
		if err == pgx.ErrNoRows {
			return httpx.Biz("此流程卡的途程不經過「%s」", res.Station)
		}
		if err != nil {
			return err
		}

		// 站點管制：可報量 = 上站良品流入 − 本站已報
		var remaining float64
		if err := tx.QueryRow(ctx, `
			SELECT remaining_qty FROM flow_card_progress WHERE card_id = $1 AND step_seq = $2 FOR UPDATE`,
			cardID, res.StepSeq).Scan(&remaining); err != nil {
			return err
		}
		var reworkQty, scrapQty float64
		for _, d := range in.Defects {
			res.DefectQty += d.Qty
			switch d.Disposition {
			case "REWORK":
				reworkQty += d.Qty
			case "SCRAP":
				scrapQty += d.Qty
			}
		}
		total := in.GoodQty + res.DefectQty
		if total <= 0 {
			return httpx.Biz("回報數量必須大於 0")
		}
		if total > remaining {
			if remaining == 0 {
				return httpx.Biz("流程卡 %s 在「%s」沒有可回報數量（上站尚未完成或本站已報完）", in.CardNo, res.Station)
			}
			return httpx.Biz("超報：流程卡 %s 在「%s」最多可報 %g", in.CardNo, res.Station, remaining)
		}

		shiftID, workDate, err := org.Clock(ctx, tx, in.FactoryID, in.At)
		if err != nil {
			return err
		}
		res.ScanID = httpx.NewID()
		_, err = tx.Exec(ctx, `
			INSERT INTO production_scans (id, factory_id, work_order_id, card_id, step_seq, station_id, line_id,
				product_id, good_qty, defect_qty, scanned_at, work_date, shift_id, user_id, device_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NULLIF($15, ''))`,
			res.ScanID, in.FactoryID, woID, cardID, res.StepSeq, stationID, in.LineID, productID,
			in.GoodQty, res.DefectQty, in.At, workDate, shiftID, in.UserID, in.DeviceID)
		if err != nil {
			return err
		}
		for _, d := range in.Defects {
			var rework *uuid.UUID
			if d.Disposition == "REWORK" {
				rework = &stationID
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO defect_records (id, scan_id, defect_code_id, qty, disposition, rework_station_id)
				VALUES ($1, $2, $3, $4, $5, $6)`, httpx.NewID(), res.ScanID, d.DefectCodeID, d.Qty, d.Disposition, rework)
			if err != nil {
				return err
			}
		}

		// 重工數量回到本站可報量，修好後再回報
		if err := tx.QueryRow(ctx, `
			UPDATE flow_card_progress
			SET good_qty = good_qty + $3, defect_qty = defect_qty + $4, input_qty = input_qty + $5,
			    first_scan_at = COALESCE(first_scan_at, $6), last_scan_at = $6
			WHERE card_id = $1 AND step_seq = $2
			RETURNING remaining_qty`,
			cardID, res.StepSeq, in.GoodQty, res.DefectQty, reworkQty, in.At).Scan(&res.Remaining); err != nil {
			return err
		}

		// 良品流入下一站；沒有下一站代表末站，良品入庫
		var nextSeq int
		err = tx.QueryRow(ctx, `SELECT seq FROM routing_steps WHERE routing_id = $1 AND seq > $2 ORDER BY seq LIMIT 1`,
			routingID, res.StepSeq).Scan(&nextSeq)
		switch {
		case err == pgx.ErrNoRows:
			res.FinalStep = true
			if in.GoodQty > 0 {
				whType := inventory.WarehouseTypeFor(category)
				wh, err := inventory.Warehouse(ctx, tx, in.FactoryID, whType)
				if err != nil {
					return err
				}
				txnType := "FG_IN"
				if whType == "WIP" {
					txnType = "STATION_IN"
				}
				if _, err := inventory.Post(ctx, tx, inventory.Txn{
					FactoryID: in.FactoryID, Type: txnType, WarehouseID: wh, ProductID: productID, Qty: in.GoodQty,
					WorkOrderID: &woID, RefType: "scan", RefID: &res.ScanID, At: in.At, WorkDate: workDate,
					UserID: in.UserID, Note: "完工入庫 " + in.CardNo,
				}); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		default:
			if in.GoodQty > 0 {
				if _, err := tx.Exec(ctx, `
					UPDATE flow_card_progress SET input_qty = input_qty + $3 WHERE card_id = $1 AND step_seq = $2`,
					cardID, nextSeq, in.GoodQty); err != nil {
					return err
				}
			}
		}

		// 到站扣帳：本站耗用的料件，依實際加工量（良品 + 報廢）扣帳
		if processed := in.GoodQty + scrapQty; processed > 0 {
			if err := consumeOnArrival(ctx, tx, in, woID, stationID, processed, workDate, res.ScanID); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO output_hourly (factory_id, work_date, shift_id, hour_start, line_id, station_id,
				work_order_id, product_id, good_qty, defect_qty)
			SELECT $1, $2, $3, date_trunc('hour', $4::timestamptz AT TIME ZONE f.timezone) AT TIME ZONE f.timezone,
			       $5, $6, $7, $8, $9, $10
			FROM factories f WHERE f.id = $1
			ON CONFLICT (factory_id, hour_start, line_id, work_order_id)
			DO UPDATE SET good_qty = output_hourly.good_qty + EXCLUDED.good_qty,
			              defect_qty = output_hourly.defect_qty + EXCLUDED.defect_qty`,
			in.FactoryID, workDate, shiftID, in.At, in.LineID, stationID, woID, productID, in.GoodQty, res.DefectQty); err != nil {
			return err
		}

		res.WOCompleted, err = updateStatuses(ctx, tx, cardID, woID, in.At)
		if err != nil {
			return err
		}
		res.LaborEnded, err = endLaborIfStationDone(ctx, tx, in, woID, res.StepSeq)
		return err
	})
	return res, err
}

// endLaborIfStationDone 本製令在本站（含所有上游站）已無剩餘可報量，代表本站數量已等於總數量：
// 自動結束本線對此製令的上工，工時算到這次回報為止。
func endLaborIfStationDone(ctx context.Context, tx pgx.Tx, in ScanInput, woID uuid.UUID, stepSeq int) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE line_labor_sessions SET ended_at = $4, ended_by = $5
		WHERE factory_id = $1 AND line_id = $2 AND work_order_id = $3 AND ended_at IS NULL AND started_at < $4
		  AND NOT EXISTS (
			SELECT 1 FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
			WHERE c.work_order_id = $3 AND c.status NOT IN ('VOID','SPLIT')
			  AND fp.step_seq <= $6 AND fp.remaining_qty > 0)`,
		in.FactoryID, in.LineID, woID, in.At, in.UserID, stepSeq)
	return tag.RowsAffected() > 0, err
}

// checkControl 生管管制：人員授權（使用者須被授權此線別）、強制上工（此線須已上工本製令）。
func checkControl(ctx context.Context, tx pgx.Tx, ctl Control, woNo string, userID, lineID, woID uuid.UUID, bypassAuth bool) error {
	if ctl.RequireAuth && !bypassAuth {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_line_assignments WHERE user_id = $1 AND line_id = $2)`,
			userID, lineID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.Biz("製令 %s 為生管管制（人員授權）：你未被授權在此線別回報，請洽管理員在「權限管理」設定授權線別", woNo)
		}
	}
	if ctl.RequireLabor {
		var ok bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM line_labor_sessions WHERE line_id = $1 AND work_order_id = $2 AND ended_at IS NULL)`,
			lineID, woID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.Biz("製令 %s 為生管管制（強制上工）：本線須先上工此製令才能回報", woNo)
		}
	}
	return nil
}

func consumeOnArrival(ctx context.Context, tx pgx.Tx, in ScanInput, woID, stationID uuid.UUID, processed float64, workDate time.Time, scanID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.product_id, p.category, m.qty_per
		FROM work_order_materials m JOIN products p ON p.id = m.product_id
		WHERE m.work_order_id = $1 AND m.deduct_timing = 'ON_ARRIVAL' AND m.consume_station_id = $2
		FOR UPDATE OF m`, woID, stationID)
	if err != nil {
		return err
	}
	type material struct {
		ID, ProductID uuid.UUID
		Category      string
		QtyPer        float64
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[material])
	if err != nil {
		return err
	}
	for _, m := range list {
		qty := round4(m.QtyPer * processed)
		whType := inventory.WarehouseTypeFor(m.Category)
		wh, err := inventory.Warehouse(ctx, tx, in.FactoryID, whType)
		if err != nil {
			return err
		}
		txnType := "CONSUME"
		if whType == "WIP" {
			txnType = "STATION_OUT" // 半成品自暫存倉出庫
		}
		if _, err := inventory.Post(ctx, tx, inventory.Txn{
			FactoryID: in.FactoryID, Type: txnType, WarehouseID: wh, ProductID: m.ProductID, Qty: -qty,
			WorkOrderID: &woID, RefType: "scan", RefID: &scanID, At: in.At, WorkDate: workDate,
			UserID: in.UserID, Note: "到站扣帳 " + in.CardNo,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE work_order_materials SET issued_qty = issued_qty + $2 WHERE id = $1`, m.ID, qty); err != nil {
			return err
		}
	}
	return nil
}

// updateStatuses 依進度重算卡片、製令與客戶訂單狀態；回傳製令是否完工。
// 卡片：無剩餘＝完成；淨回報為 0（例如全部撤銷）＝未開始；其餘＝進行中。製令同理（未開始＝已開立）。
func updateStatuses(ctx context.Context, tx pgx.Tx, cardID, woID uuid.UUID, at time.Time) (bool, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE flow_cards SET status = CASE
			WHEN NOT EXISTS (SELECT 1 FROM flow_card_progress WHERE card_id = $1 AND remaining_qty > 0) THEN 'DONE'
			WHEN NOT EXISTS (SELECT 1 FROM flow_card_progress WHERE card_id = $1 AND good_qty + defect_qty > 0) THEN 'OPEN'
			ELSE 'IN_PROGRESS' END
		WHERE id = $1`, cardID); err != nil {
		return false, err
	}
	var completed bool
	err := tx.QueryRow(ctx, `
		WITH p AS (
			SELECT fp.* FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
			WHERE c.work_order_id = $1 AND c.status NOT IN ('VOID','SPLIT'))
		UPDATE work_orders SET
			status = CASE
				WHEN NOT EXISTS (SELECT 1 FROM p WHERE remaining_qty > 0) THEN 'COMPLETED'
				WHEN NOT EXISTS (SELECT 1 FROM p WHERE good_qty + defect_qty > 0) THEN 'RELEASED'
				ELSE 'IN_PROGRESS' END,
			updated_at = $2
		WHERE id = $1
		RETURNING status = 'COMPLETED'`, woID, at).Scan(&completed)
	if err != nil {
		return false, err
	}
	if completed {
		if _, err := tx.Exec(ctx, `UPDATE work_orders SET completed_at = $2 WHERE id = $1 AND completed_at IS NULL`, woID, at); err != nil {
			return false, err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE work_orders SET completed_at = NULL WHERE id = $1`, woID); err != nil {
		return false, err // 撤銷後由完工退回
	}
	return completed, refreshSalesOrder(ctx, tx, woID, at)
}
