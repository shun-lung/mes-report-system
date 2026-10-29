package production

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
	"mes/internal/inventory"
	"mes/internal/org"
)

type ReverseResult struct {
	ReversalID uuid.UUID `json:"reversalId"`
	CardNo     string    `json:"cardNo"`
	Station    string    `json:"station"`
	GoodQty    float64   `json:"goodQty"`
	DefectQty  float64   `json:"defectQty"`
	Remaining  float64   `json:"remaining"` // 撤銷後本卡本站可報量
}

// ReverseScan 撤銷一筆掃描回報，單一交易內還原回報造成的所有影響：
// 本站進度與重工、下一站流入（下一站已回報則拒絕）、到站扣帳、末站入庫、小時統計、不良統計、卡片／製令／訂單狀態。
// 撤銷紀錄沿用原回報的時間、生產日與班別（統計在原時段沖回），實際撤銷時間記在 created_at。
// allowOld=false 時只能撤銷本班的回報。
func (s *Service) ReverseScan(ctx context.Context, factoryID, scanID, userID uuid.UUID, reason string, allowOld bool, at time.Time) (ReverseResult, error) {
	var res ReverseResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var sc struct {
			cardID, woID, stationID, lineID, productID, shiftID uuid.UUID
			stepSeq                                             int
			good, defect                                        float64
			scannedAt, workDate                                 time.Time
			reversalOf                                          *uuid.UUID
		}
		err := tx.QueryRow(ctx, `
			SELECT ps.card_id, ps.work_order_id, ps.station_id, ps.line_id, ps.product_id, ps.shift_id, ps.step_seq,
			       ps.good_qty, ps.defect_qty, ps.scanned_at, ps.work_date, ps.reversal_of, fc.card_no, st.name
			FROM production_scans ps
			JOIN flow_cards fc ON fc.id = ps.card_id
			JOIN work_stations st ON st.id = ps.station_id
			WHERE ps.id = $1 AND ps.factory_id = $2`, scanID, factoryID).
			Scan(&sc.cardID, &sc.woID, &sc.stationID, &sc.lineID, &sc.productID, &sc.shiftID, &sc.stepSeq,
				&sc.good, &sc.defect, &sc.scannedAt, &sc.workDate, &sc.reversalOf, &res.CardNo, &res.Station)
		if err == pgx.ErrNoRows {
			return httpx.Biz("回報紀錄不存在")
		}
		if err != nil {
			return err
		}
		if sc.reversalOf != nil {
			return httpx.Biz("這是撤銷紀錄，不可再撤銷")
		}

		// 鎖卡片，與掃描回報互斥
		var woStatus string
		if err := tx.QueryRow(ctx, `
			SELECT w.status FROM flow_cards fc JOIN work_orders w ON w.id = fc.work_order_id
			WHERE fc.id = $1 FOR UPDATE OF fc`, sc.cardID).Scan(&woStatus); err != nil {
			return err
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM production_scans WHERE reversal_of = $1)`, scanID).Scan(&done); err != nil {
			return err
		}
		if done {
			return httpx.Biz("此筆回報已撤銷")
		}
		if woStatus != "RELEASED" && woStatus != "IN_PROGRESS" && woStatus != "COMPLETED" {
			return httpx.Biz("製令已結案或取消，不可撤銷")
		}

		currentShift, currentDate, err := org.Clock(ctx, tx, factoryID, at)
		if err != nil {
			return err
		}
		if !allowOld && (currentShift != sc.shiftID || !currentDate.Equal(sc.workDate)) {
			return httpx.Biz("只能撤銷本班的回報；較早的回報請生管在「回報紀錄」撤銷")
		}

		var rework float64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(sum(qty), 0) FROM defect_records WHERE scan_id = $1 AND disposition = 'REWORK'`, scanID).Scan(&rework); err != nil {
			return err
		}

		// 下一站：撤回已流入的良品；若下一站已回報這些量則不可撤銷
		var nextSeq int
		var nextStation string
		var nextRemaining float64
		err = tx.QueryRow(ctx, `
			SELECT fp.step_seq, st.name, fp.remaining_qty
			FROM flow_card_progress fp JOIN work_stations st ON st.id = fp.station_id
			WHERE fp.card_id = $1 AND fp.step_seq > $2
			ORDER BY fp.step_seq LIMIT 1 FOR UPDATE OF fp`, sc.cardID, sc.stepSeq).Scan(&nextSeq, &nextStation, &nextRemaining)
		hasNext := err == nil
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if hasNext && sc.good > 0 {
			if nextRemaining < sc.good {
				return httpx.Biz("下一站「%s」可撤回量只有 %g（需要 %g）：下一站已回報或已拆卡，請先撤銷下一站的回報或合併子卡", nextStation, nextRemaining, sc.good)
			}
			if _, err := tx.Exec(ctx, `UPDATE flow_card_progress SET input_qty = input_qty - $3 WHERE card_id = $1 AND step_seq = $2`,
				sc.cardID, nextSeq, sc.good); err != nil {
				return err
			}
		}

		// 本站：扣回良品、不良，以及重工回流的投入量
		if err := tx.QueryRow(ctx, `
			UPDATE flow_card_progress
			SET good_qty = good_qty - $3, defect_qty = defect_qty - $4, input_qty = input_qty - $5
			WHERE card_id = $1 AND step_seq = $2
			RETURNING remaining_qty`, sc.cardID, sc.stepSeq, sc.good, sc.defect, rework).Scan(&res.Remaining); err != nil {
			return err
		}

		// 撤銷紀錄（負數），沿用原時段
		res.ReversalID = httpx.NewID()
		if _, err := tx.Exec(ctx, `
			INSERT INTO production_scans (id, factory_id, work_order_id, card_id, step_seq, station_id, line_id, product_id,
				good_qty, defect_qty, reversal_of, scanned_at, work_date, shift_id, user_id, device_id, created_at, note)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'UNDO', $16, NULLIF($17, ''))`,
			res.ReversalID, factoryID, sc.woID, sc.cardID, sc.stepSeq, sc.stationID, sc.lineID, sc.productID,
			-sc.good, -sc.defect, scanID, sc.scannedAt, sc.workDate, sc.shiftID, userID, at, reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO defect_records (id, scan_id, defect_code_id, qty, disposition, rework_station_id, note)
			SELECT gen_random_uuid(), $2, defect_code_id, -qty, disposition, rework_station_id, '撤銷'
			FROM defect_records WHERE scan_id = $1`, scanID, res.ReversalID); err != nil {
			return err
		}

		// 庫存：回報產生的扣帳與入庫逐筆反向沖銷
		rows, err := tx.Query(ctx, `
			SELECT id, txn_no, txn_type, warehouse_id, product_id, qty FROM inventory_transactions
			WHERE ref_type = 'scan' AND ref_id = $1 ORDER BY txn_no`, scanID)
		if err != nil {
			return err
		}
		type txn struct {
			ID                     uuid.UUID
			No, Type               string
			WarehouseID, ProductID uuid.UUID
			Qty                    float64
		}
		txns, err := pgx.CollectRows(rows, pgx.RowToStructByPos[txn])
		if err != nil {
			return err
		}
		for _, t := range txns {
			if _, err := inventory.Post(ctx, tx, inventory.Txn{
				FactoryID: factoryID, Type: "REVERSAL", WarehouseID: t.WarehouseID, ProductID: t.ProductID, Qty: -t.Qty,
				WorkOrderID: &sc.woID, RefType: "scan", RefID: &res.ReversalID, ReversalOf: &t.ID,
				At: at, WorkDate: currentDate, UserID: userID, Note: "撤銷回報 " + res.CardNo + "（沖銷 " + t.No + "）",
			}); err != nil {
				return err
			}
			if t.Type == "CONSUME" || t.Type == "STATION_OUT" {
				// 到站扣帳退回：扣回製令用料的已發數量（t.Qty 為負數）
				if _, err := tx.Exec(ctx, `
					UPDATE work_order_materials SET issued_qty = GREATEST(issued_qty + $3, 0)
					WHERE id = (SELECT id FROM work_order_materials WHERE work_order_id = $1 AND product_id = $2 ORDER BY id LIMIT 1)`,
					sc.woID, t.ProductID, t.Qty); err != nil {
					return err
				}
			}
		}

		// 小時統計在原時段沖回
		if _, err := tx.Exec(ctx, `
			UPDATE output_hourly SET good_qty = good_qty - $5, defect_qty = defect_qty - $6
			WHERE factory_id = $1 AND line_id = $2 AND work_order_id = $3
			  AND hour_start = (SELECT date_trunc('hour', $4::timestamptz AT TIME ZONE f.timezone) AT TIME ZONE f.timezone
			                    FROM factories f WHERE f.id = $1)`,
			factoryID, sc.lineID, sc.woID, sc.scannedAt, sc.good, sc.defect); err != nil {
			return err
		}

		res.GoodQty, res.DefectQty = sc.good, sc.defect
		_, err = updateStatuses(ctx, tx, sc.cardID, sc.woID, at)
		return err
	})
	return res, err
}

type reverseRequest struct {
	Reason string `json:"reason" binding:"required,max=200"`
}

// reverseScan 現場組長可撤銷本班回報；具製令編輯權限（生管）可撤銷較早的回報。
func (h *Handler) reverseScan(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var req reverseRequest
	if !httpx.Bind(c, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		httpx.Fail(c, httpx.Biz("請填寫撤銷原因"))
		return
	}
	res, err := h.svc.ReverseScan(c, auth.FactoryID(c), id, auth.UserID(c), reason, auth.Can(c, "wo:edit"), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type scanRecord struct {
	ID         uuid.UUID  `json:"id"`
	ScannedAt  time.Time  `json:"scannedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	WorkDate   time.Time  `json:"workDate"`
	Shift      string     `json:"shift"`
	Line       string     `json:"line"`
	Station    string     `json:"station"`
	WONo       string     `json:"woNo"`
	CardNo     string     `json:"cardNo"`
	ItemNo     string     `json:"itemNo"`
	Good       float64    `json:"good"`
	Defect     float64    `json:"defect"`
	Defects    *string    `json:"defects"` // 例：F01×2（重工）、F02×1（報廢）
	User       string     `json:"user"`
	Note       *string    `json:"note"`
	ReversalOf *uuid.UUID `json:"reversalOf"`
	Reversed   bool       `json:"reversed"`
}

// listScans 回報紀錄查詢：依生產日（預設今天）、班別、線別、流程卡號。
func (h *Handler) listScans(c *gin.Context) {
	factoryID := auth.FactoryID(c)
	_, workDate, err := org.Clock(c, h.svc.pool, factoryID, time.Now())
	if err != nil {
		httpx.Fail(c, httpx.Biz("%s", err.Error()))
		return
	}
	if d := c.Query("date"); d != "" {
		if workDate, err = time.Parse(time.DateOnly, d); err != nil {
			httpx.Abort(c, http.StatusBadRequest, "date 格式錯誤")
			return
		}
	}
	var shiftID, lineID *uuid.UUID
	for key, dst := range map[string]**uuid.UUID{"shiftId": &shiftID, "lineId": &lineID} {
		if v := c.Query(key); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				httpx.Abort(c, http.StatusBadRequest, key+" 格式錯誤")
				return
			}
			*dst = &id
		}
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT ps.id, ps.scanned_at, ps.created_at, ps.work_date, sh.name, l.name, st.name, w.wo_no, fc.card_no, p.item_no,
		       ps.good_qty, ps.defect_qty,
		       (SELECT string_agg(dc.code || '×' || trim_scale(abs(dr.qty)) || CASE dr.disposition WHEN 'REWORK' THEN '（重工）'
		                         WHEN 'SCRAP' THEN '（報廢）' ELSE '（允收）' END, '、' ORDER BY dc.code)
		        FROM defect_records dr JOIN defect_codes dc ON dc.id = dr.defect_code_id WHERE dr.scan_id = ps.id),
		       u.name, ps.note, ps.reversal_of,
		       EXISTS (SELECT 1 FROM production_scans r WHERE r.reversal_of = ps.id)
		FROM production_scans ps
		JOIN shifts sh ON sh.id = ps.shift_id
		JOIN production_lines l ON l.id = ps.line_id
		JOIN work_stations st ON st.id = ps.station_id
		JOIN work_orders w ON w.id = ps.work_order_id
		JOIN flow_cards fc ON fc.id = ps.card_id
		JOIN products p ON p.id = ps.product_id
		JOIN users u ON u.id = ps.user_id
		WHERE ps.factory_id = $1
		  AND ($2::text <> '' OR ps.work_date = $3)
		  AND ($2::text = '' OR fc.card_no ILIKE '%' || $2 || '%')
		  AND ($4::uuid IS NULL OR ps.shift_id = $4)
		  AND ($5::uuid IS NULL OR ps.line_id = $5)
		ORDER BY ps.scanned_at DESC, ps.created_at DESC
		LIMIT 500`, factoryID, strings.TrimSpace(c.Query("cardNo")), workDate, shiftID, lineID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[scanRecord])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"workDate": workDate.Format(time.DateOnly), "scans": list})
}
