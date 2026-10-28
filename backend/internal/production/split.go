package production

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

// 拆卡：從流程卡某一站的待做數量分出子卡，子卡從該站開始往下走；原卡數量相應減少。
// 合併：子卡尚無任何回報時，可併回原卡（子卡作廢）。
// 待做量只是換一張卡承載，製令總量、庫存與統計都不受影響。

type SplitResult struct {
	CardNo      string    `json:"cardNo"` // 新子卡
	ParentNo    string    `json:"parentCardNo"`
	Qty         float64   `json:"qty"`
	Station     string    `json:"station"`
	ParentQty   float64   `json:"parentQty"`  // 拆後原卡數量
	ParentLeft  float64   `json:"parentLeft"` // 拆後原卡在該站的待做量
	WorkOrderID uuid.UUID `json:"workOrderId"`
}

type lockedCard struct {
	id, woID   uuid.UUID
	no, status string
	qty        float64
	parentID   *uuid.UUID
	woStatus   string
}

func lockCard(ctx context.Context, tx pgx.Tx, factoryID uuid.UUID, cardNo string) (lockedCard, error) {
	var c lockedCard
	err := tx.QueryRow(ctx, `
		SELECT fc.id, fc.work_order_id, fc.card_no, fc.status, fc.qty, fc.parent_card_id, w.status
		FROM flow_cards fc JOIN work_orders w ON w.id = fc.work_order_id
		WHERE fc.card_no = $1 AND fc.factory_id = $2
		FOR UPDATE OF fc`, cardNo, factoryID).
		Scan(&c.id, &c.woID, &c.no, &c.status, &c.qty, &c.parentID, &c.woStatus)
	if err == pgx.ErrNoRows {
		return c, httpx.Biz("找不到流程卡 %s", cardNo)
	}
	return c, err
}

// SplitCard 在 stepSeq 這一站把 qty 從原卡分到新子卡。
func (s *Service) SplitCard(ctx context.Context, factoryID uuid.UUID, cardNo string, stepSeq int, qty float64, reason string, at time.Time) (SplitResult, error) {
	var res SplitResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := lockCard(ctx, tx, factoryID, cardNo)
		if err != nil {
			return err
		}
		if c.status == "VOID" || c.status == "SPLIT" {
			return httpx.Biz("流程卡 %s 已作廢，不可拆卡", c.no)
		}
		if c.woStatus != "RELEASED" && c.woStatus != "IN_PROGRESS" {
			return httpx.Biz("製令狀態不可拆卡")
		}
		if qty <= 0 {
			return httpx.Biz("拆出數量必須大於 0")
		}
		var remaining float64
		err = tx.QueryRow(ctx, `
			SELECT fp.remaining_qty, st.name FROM flow_card_progress fp JOIN work_stations st ON st.id = fp.station_id
			WHERE fp.card_id = $1 AND fp.step_seq = $2 FOR UPDATE OF fp`, c.id, stepSeq).Scan(&remaining, &res.Station)
		if err == pgx.ErrNoRows {
			return httpx.Biz("此卡途程沒有這一站")
		}
		if err != nil {
			return err
		}
		if qty > remaining {
			return httpx.Biz("「%s」待做量只有 %g，不能拆出 %g", res.Station, remaining, qty)
		}

		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM flow_cards WHERE parent_card_id = $1`, c.id).Scan(&n); err != nil {
			return err
		}
		res.CardNo = fmt.Sprintf("%s-S%d", c.no, n+1)
		res.ParentNo, res.Qty, res.WorkOrderID = c.no, qty, c.woID

		if err := tx.QueryRow(ctx, `
			UPDATE flow_card_progress SET input_qty = input_qty - $3 WHERE card_id = $1 AND step_seq = $2
			RETURNING remaining_qty`, c.id, stepSeq, qty).Scan(&res.ParentLeft); err != nil {
			return err
		}
		note := fmt.Sprintf("%s 於「%s」拆出 %g 至 %s", at.Format("01/02 15:04"), res.Station, qty, res.CardNo)
		if reason != "" {
			note += "：" + reason
		}
		if err := tx.QueryRow(ctx, `
			UPDATE flow_cards SET qty = qty - $2, note = concat_ws(E'\n', note, $3::text) WHERE id = $1 RETURNING qty`,
			c.id, qty, note).Scan(&res.ParentQty); err != nil {
			return err
		}

		childID := httpx.NewID()
		childNote := fmt.Sprintf("由 %s 於「%s」拆出", c.no, res.Station)
		if reason != "" {
			childNote += "：" + reason
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO flow_cards (id, factory_id, work_order_id, card_no, parent_card_id, qty, status, note, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'OPEN', $7, $8)`,
			childID, factoryID, c.woID, res.CardNo, c.id, qty, childNote, at); err != nil {
			return err
		}
		// 子卡沿用原卡的途程站序，從拆卡那一站開始
		if _, err := tx.Exec(ctx, `
			INSERT INTO flow_card_progress (card_id, step_seq, station_id, input_qty)
			SELECT $2, step_seq, station_id, CASE WHEN step_seq = $3 THEN $4 ELSE 0 END
			FROM flow_card_progress WHERE card_id = $1`, c.id, childID, stepSeq, qty); err != nil {
			return err
		}
		if _, err := updateStatuses(ctx, tx, c.id, c.woID, at); err != nil {
			return err
		}
		_, err = updateStatuses(ctx, tx, childID, c.woID, at)
		return err
	})
	return res, err
}

// MergeCard 把尚無回報的子卡併回原卡，子卡作廢。
func (s *Service) MergeCard(ctx context.Context, factoryID uuid.UUID, cardNo string, at time.Time) (string, error) {
	var parentNo string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := lockCard(ctx, tx, factoryID, cardNo)
		if err != nil {
			return err
		}
		if c.parentID == nil {
			return httpx.Biz("%s 不是拆出的子卡", c.no)
		}
		if c.status == "VOID" {
			return httpx.Biz("%s 已作廢", c.no)
		}
		var used bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM production_scans WHERE card_id = $1)
			    OR EXISTS (SELECT 1 FROM flow_cards WHERE parent_card_id = $1 AND status <> 'VOID')
			    OR EXISTS (SELECT 1 FROM flow_card_progress WHERE card_id = $1 AND good_qty + defect_qty <> 0)`, c.id).Scan(&used); err != nil {
			return err
		}
		if used {
			return httpx.Biz("%s 已有回報或再拆出子卡，不可合併", c.no)
		}
		var parentStatus string
		if err := tx.QueryRow(ctx, `SELECT card_no, status FROM flow_cards WHERE id = $1 FOR UPDATE`, *c.parentID).
			Scan(&parentNo, &parentStatus); err != nil {
			return err
		}
		if parentStatus == "VOID" {
			return httpx.Biz("原卡 %s 已作廢，不可合併", parentNo)
		}
		// 子卡各站的投入量加回原卡同一站
		if _, err := tx.Exec(ctx, `
			UPDATE flow_card_progress p SET input_qty = p.input_qty + ch.input_qty
			FROM flow_card_progress ch
			WHERE ch.card_id = $1 AND p.card_id = $2 AND p.step_seq = ch.step_seq AND ch.input_qty <> 0`,
			c.id, *c.parentID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE flow_card_progress SET input_qty = 0 WHERE card_id = $1`, c.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE flow_cards SET qty = qty + $2, note = concat_ws(E'\n', note, $3::text) WHERE id = $1`,
			*c.parentID, c.qty, fmt.Sprintf("%s 併回子卡 %s（%g）", at.Format("01/02 15:04"), c.no, c.qty)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE flow_cards SET status = 'VOID', note = concat_ws(E'\n', note, $2::text) WHERE id = $1`,
			c.id, fmt.Sprintf("%s 已併回 %s", at.Format("01/02 15:04"), parentNo)); err != nil {
			return err
		}
		_, err = updateStatuses(ctx, tx, *c.parentID, c.woID, at)
		return err
	})
	return parentNo, err
}

type splitRequest struct {
	StepSeq int     `json:"stepSeq" binding:"required"`
	Qty     float64 `json:"qty" binding:"gt=0"`
	Reason  string  `json:"reason" binding:"required,max=200"`
}

func (h *Handler) splitCard(c *gin.Context) {
	var req splitRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		httpx.Fail(c, httpx.Biz("請填寫拆卡原因"))
		return
	}
	res, err := h.svc.SplitCard(c, auth.FactoryID(c), strings.TrimSpace(c.Param("cardNo")), req.StepSeq, req.Qty, strings.TrimSpace(req.Reason), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) mergeCard(c *gin.Context) {
	parent, err := h.svc.MergeCard(c, auth.FactoryID(c), strings.TrimSpace(c.Param("cardNo")), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"parentCardNo": parent})
}

type cardProgressRow struct {
	CardNo   string     `json:"cardNo"`
	ParentNo *string    `json:"parentCardNo"`
	Qty      float64    `json:"qty"`
	Status   string     `json:"status"`
	Prints   int        `json:"printCount"`
	Note     *string    `json:"note"`
	HasScans bool       `json:"hasScans"`
	Steps    []cardStep `json:"steps"`
}

// workOrderCards 製令的所有流程卡（含子卡、作廢卡）與各站進度。
func (h *Handler) workOrderCards(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT fc.card_no, pc.card_no, fc.qty, fc.status, fc.print_count, fc.note,
		       EXISTS (SELECT 1 FROM production_scans ps WHERE ps.card_id = fc.id),
		       COALESCE((SELECT json_agg(json_build_object('seq', fp.step_seq, 'station', st.name, 'input', fp.input_qty,
		                    'good', fp.good_qty, 'defect', fp.defect_qty, 'remaining', fp.remaining_qty, 'here', false) ORDER BY fp.step_seq)
		                 FROM flow_card_progress fp JOIN work_stations st ON st.id = fp.station_id WHERE fp.card_id = fc.id), '[]')
		FROM flow_cards fc
		JOIN work_orders w ON w.id = fc.work_order_id AND w.factory_id = $2
		LEFT JOIN flow_cards pc ON pc.id = fc.parent_card_id
		WHERE fc.work_order_id = $1
		ORDER BY fc.card_no`, id, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[cardProgressRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}
