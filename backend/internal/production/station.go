package production

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
	"mes/internal/org"
)

// 現場掃描畫面用的查詢：線別目前狀態、流程卡在本站的進度。

type stationLine struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	StationID    uuid.UUID `json:"stationId"`
	StationName  string    `json:"stationName"`
	Capacity     float64   `json:"capacityPerHour"`
	StdHeadcount int       `json:"stdHeadcount"`
}

type laborSession struct {
	WOID      uuid.UUID `json:"woId"`
	WONo      string    `json:"woNo"`
	ItemNo    string    `json:"itemNo"`
	ItemName  string    `json:"itemName"`
	Headcount int       `json:"headcount"`
	StartedAt time.Time `json:"startedAt"`
}

type stationScan struct {
	ID         uuid.UUID  `json:"id"`
	At         time.Time  `json:"at"`
	CardNo     string     `json:"cardNo"`
	WONo       string     `json:"woNo"`
	Good       float64    `json:"good"`
	Defect     float64    `json:"defect"`
	User       string     `json:"user"`
	ReversalOf *uuid.UUID `json:"reversalOf"`
	Reversed   bool       `json:"reversed"`
}

type stationWO struct {
	ID         uuid.UUID `json:"id"`
	WONo       string    `json:"woNo"`
	ItemNo     string    `json:"itemNo"`
	ItemName   string    `json:"itemName"`
	PlannedQty float64   `json:"plannedQty"`
	Remaining  float64   `json:"remaining"` // 本站可報未報
	DoneHere   float64   `json:"doneHere"`  // 本站累計良品
	Upstream   float64   `json:"upstream"`  // 尚在上游站、還沒流到本站的數量
}

func (h *Handler) lineOf(c *gin.Context) (stationLine, bool) {
	var l stationLine
	id, err := uuid.Parse(c.Query("lineId"))
	if err != nil {
		httpx.Abort(c, http.StatusBadRequest, "請指定線別")
		return l, false
	}
	err = h.svc.pool.QueryRow(c, `
		SELECT l.id, l.code, l.name, s.id, s.name, l.capacity_per_hour, l.std_headcount
		FROM production_lines l JOIN work_stations s ON s.id = l.station_id
		WHERE l.id = $1 AND l.factory_id = $2 AND l.is_active`, id, auth.FactoryID(c)).
		Scan(&l.ID, &l.Code, &l.Name, &l.StationID, &l.StationName, &l.Capacity, &l.StdHeadcount)
	if err == pgx.ErrNoRows {
		httpx.Fail(c, httpx.Biz("線別不存在或已停用"))
		return l, false
	}
	if err != nil {
		httpx.DBError(c, err)
		return l, false
	}
	return l, true
}

// stationState 線別目前狀態：上工中的製令、本班統計、最近掃描、本站可做的製令。
func (h *Handler) stationState(c *gin.Context) {
	line, ok := h.lineOf(c)
	if !ok {
		return
	}
	factoryID := auth.FactoryID(c)
	pool := h.svc.pool
	shiftID, workDate, err := org.Clock(c, pool, factoryID, time.Now())
	if err != nil {
		httpx.Fail(c, httpx.Biz("%s", err.Error()))
		return
	}
	authorized, err := userAuthorized(c, h, line.ID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	res := gin.H{"line": line, "workDate": workDate.Format(time.DateOnly), "authorized": authorized}

	var shiftName string
	if err := pool.QueryRow(c, `SELECT name FROM shifts WHERE id = $1`, shiftID).Scan(&shiftName); err != nil {
		httpx.DBError(c, err)
		return
	}
	res["shift"] = shiftName

	var s laborSession
	err = pool.QueryRow(c, `
		SELECT w.id, w.wo_no, p.item_no, p.name, ls.headcount, ls.started_at
		FROM line_labor_sessions ls
		JOIN work_orders w ON w.id = ls.work_order_id
		JOIN products p ON p.id = w.product_id
		WHERE ls.line_id = $1 AND ls.ended_at IS NULL`, line.ID).
		Scan(&s.WOID, &s.WONo, &s.ItemNo, &s.ItemName, &s.Headcount, &s.StartedAt)
	switch {
	case err == pgx.ErrNoRows:
		res["session"] = nil
	case err != nil:
		httpx.DBError(c, err)
		return
	default:
		res["session"] = s
	}

	var good, defect float64
	var scans int
	if err := pool.QueryRow(c, `
		SELECT COALESCE(sum(good_qty), 0), COALESCE(sum(defect_qty), 0), count(*)
		FROM production_scans WHERE line_id = $1 AND work_date = $2 AND shift_id = $3`,
		line.ID, workDate, shiftID).Scan(&good, &defect, &scans); err != nil {
		httpx.DBError(c, err)
		return
	}
	res["totals"] = gin.H{"good": good, "defect": defect, "scans": scans}

	rows, err := pool.Query(c, `
		SELECT ps.id, ps.scanned_at, fc.card_no, w.wo_no, ps.good_qty, ps.defect_qty, u.name, ps.reversal_of,
		       EXISTS (SELECT 1 FROM production_scans r WHERE r.reversal_of = ps.id)
		FROM production_scans ps
		JOIN flow_cards fc ON fc.id = ps.card_id
		JOIN work_orders w ON w.id = ps.work_order_id
		JOIN users u ON u.id = ps.user_id
		WHERE ps.line_id = $1 AND ps.work_date = $2 AND ps.shift_id = $3
		ORDER BY ps.scanned_at DESC, ps.created_at DESC LIMIT 12`, line.ID, workDate, shiftID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res["recent"], err = pgx.CollectRows(rows, pgx.RowToStructByPos[stationScan]); err != nil {
		httpx.DBError(c, err)
		return
	}

	rows, err = pool.Query(c, `
		SELECT w.id, w.wo_no, p.item_no, p.name, w.planned_qty,
		       COALESCE(sum(fp.remaining_qty) FILTER (WHERE fp.station_id = $2), 0),
		       COALESCE(sum(fp.good_qty) FILTER (WHERE fp.station_id = $2), 0),
		       COALESCE(sum(fp.remaining_qty) FILTER (WHERE fp.step_seq < rs.seq), 0)
		FROM work_orders w
		JOIN products p ON p.id = w.product_id
		JOIN routing_steps rs ON rs.routing_id = w.routing_id AND rs.station_id = $2
		JOIN flow_cards c ON c.work_order_id = w.id AND c.status NOT IN ('VOID','SPLIT')
		JOIN flow_card_progress fp ON fp.card_id = c.id
		WHERE w.factory_id = $1 AND w.status IN ('RELEASED','IN_PROGRESS')
		GROUP BY w.id, w.wo_no, p.item_no, p.name, w.planned_qty, w.due_date
		ORDER BY w.due_date NULLS LAST, w.wo_no`, factoryID, line.StationID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res["workOrders"], err = pgx.CollectRows(rows, pgx.RowToStructByPos[stationWO]); err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

type cardStep struct {
	Seq       int     `json:"seq"`
	Station   string  `json:"station"`
	Input     float64 `json:"input"`
	Good      float64 `json:"good"`
	Defect    float64 `json:"defect"`
	Remaining float64 `json:"remaining"`
	Here      bool    `json:"here"`
}

// cardInfo 流程卡資訊與各站進度；here 標示本線所屬站點的那一站。
func (h *Handler) cardInfo(c *gin.Context) {
	line, ok := h.lineOf(c)
	if !ok {
		return
	}
	cardNo := strings.TrimSpace(c.Param("cardNo"))
	var res struct {
		CardNo    string     `json:"cardNo"`
		CardQty   float64    `json:"cardQty"`
		Status    string     `json:"status"`
		WOID      uuid.UUID  `json:"woId"`
		WONo      string     `json:"woNo"`
		WOStatus  string     `json:"woStatus"`
		ItemNo    string     `json:"itemNo"`
		ItemName  string     `json:"itemName"`
		Unit      string     `json:"unit"`
		ImageID   *uuid.UUID `json:"imageId"`
		OnRoute   bool       `json:"onRoute"`
		Remaining float64    `json:"remaining"`
		StepSeq   int        `json:"stepSeq"` // 本線所屬站點在途程中的站序
		Control
		ParentNo *string    `json:"parentCardNo"`
		Steps    []cardStep `json:"steps"`
	}
	var cardID uuid.UUID
	load := func(no string) error {
		return h.svc.pool.QueryRow(c, `
		SELECT fc.id, fc.card_no, fc.qty, fc.status, w.id, w.wo_no, w.status, p.item_no, p.name, p.unit,
		       (SELECT i.id FROM product_images i WHERE i.product_id = p.id ORDER BY i.is_primary DESC, i.sort_order LIMIT 1),
		       (SELECT pc.card_no FROM flow_cards pc WHERE pc.id = fc.parent_card_id), w.require_auth, w.require_labor
		FROM flow_cards fc
		JOIN work_orders w ON w.id = fc.work_order_id
		JOIN products p ON p.id = w.product_id
		WHERE fc.card_no = $1 AND fc.factory_id = $2`, no, auth.FactoryID(c)).
			Scan(&cardID, &res.CardNo, &res.CardQty, &res.Status, &res.WOID, &res.WONo, &res.WOStatus,
				&res.ItemNo, &res.ItemName, &res.Unit, &res.ImageID, &res.ParentNo, &res.RequireAuth, &res.RequireLabor)
	}
	err := load(cardNo)
	if err == pgx.ErrNoRows {
		// 掃到的是製令號：本站只有一張可報的卡就直接帶出，多張則讓現場挑選
		picks, isWO, perr := h.woCardsAt(c, cardNo, line.StationID)
		switch {
		case perr != nil:
			httpx.DBError(c, perr)
			return
		case !isWO:
			httpx.Fail(c, httpx.Biz("找不到流程卡「%s」", cardNo))
			return
		case len(picks) == 0:
			httpx.Fail(c, httpx.Biz("製令 %s 在「%s」沒有可回報的流程卡（上站尚未完成或本站已報完）", cardNo, line.StationName))
			return
		case len(picks) > 1:
			c.JSON(http.StatusOK, gin.H{"woNo": cardNo, "pick": picks})
			return
		}
		err = load(picks[0].CardNo)
	}
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT fp.step_seq, s.name, fp.input_qty, fp.good_qty, fp.defect_qty, fp.remaining_qty, fp.station_id = $2
		FROM flow_card_progress fp JOIN work_stations s ON s.id = fp.station_id
		WHERE fp.card_id = $1 ORDER BY fp.step_seq`, cardID, line.StationID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res.Steps, err = pgx.CollectRows(rows, pgx.RowToStructByPos[cardStep]); err != nil {
		httpx.DBError(c, err)
		return
	}
	for _, s := range res.Steps {
		if s.Here {
			res.OnRoute, res.Remaining, res.StepSeq = true, s.Remaining, s.Seq
		}
	}
	c.JSON(http.StatusOK, res)
}

type cardPick struct {
	CardNo    string  `json:"cardNo"`
	Qty       float64 `json:"qty"`
	Remaining float64 `json:"remaining"`
}

// woCardsAt 依製令號找出在指定站點仍有可報數量的流程卡；isWO 表示此製令號存在。
func (h *Handler) woCardsAt(c *gin.Context, woNo string, stationID uuid.UUID) ([]cardPick, bool, error) {
	var isWO bool
	if err := h.svc.pool.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM work_orders WHERE wo_no = $1 AND factory_id = $2)`,
		woNo, auth.FactoryID(c)).Scan(&isWO); err != nil || !isWO {
		return nil, false, err
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT fc.card_no, fc.qty, fp.remaining_qty
		FROM flow_cards fc
		JOIN work_orders w ON w.id = fc.work_order_id
		JOIN flow_card_progress fp ON fp.card_id = fc.id AND fp.station_id = $3
		WHERE w.wo_no = $1 AND w.factory_id = $2 AND fc.status IN ('OPEN','IN_PROGRESS') AND fp.remaining_qty > 0
		ORDER BY fc.card_no`, woNo, auth.FactoryID(c), stationID)
	if err != nil {
		return nil, true, err
	}
	picks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[cardPick])
	return picks, true, err
}

// userAuthorized 目前使用者是否被授權此線別（總部管理員一律視為已授權）。
func userAuthorized(c *gin.Context, h *Handler, lineID uuid.UUID) (bool, error) {
	if auth.IsSuperAdmin(c) {
		return true, nil
	}
	var ok bool
	err := h.svc.pool.QueryRow(c, `SELECT EXISTS (SELECT 1 FROM user_line_assignments WHERE user_id = $1 AND line_id = $2)`,
		auth.UserID(c), lineID).Scan(&ok)
	return ok, err
}

type stationLineOption struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	StationName string    `json:"stationName"`
	Authorized  bool      `json:"authorized"`
	StationID   uuid.UUID `json:"stationId"`
	StdHead     int       `json:"stdHeadcount"`
}

// stationLines 掃描畫面可選的線別：有授權線別的人只列授權的；沒有任何授權（或總部管理員）列出全部。
func (h *Handler) stationLines(c *gin.Context) {
	userID, superAdmin := auth.UserID(c), auth.IsSuperAdmin(c)
	rows, err := h.svc.pool.Query(c, `
		SELECT l.id, l.code, l.name, s.name,
		       $3 OR EXISTS (SELECT 1 FROM user_line_assignments a WHERE a.user_id = $2 AND a.line_id = l.id),
		       s.id, l.std_headcount
		FROM production_lines l JOIN work_stations s ON s.id = l.station_id
		WHERE l.factory_id = $1 AND l.is_active AND s.is_active
		ORDER BY s.sort_order, l.code`, auth.FactoryID(c), userID, superAdmin)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[stationLineOption])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	var mine []stationLineOption
	for _, l := range all {
		if l.Authorized {
			mine = append(mine, l)
		}
	}
	if superAdmin || len(mine) == 0 {
		c.JSON(http.StatusOK, gin.H{"lines": all, "restricted": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lines": mine, "restricted": true})
}

type stationCard struct {
	WorkOrderID uuid.UUID `json:"workOrderId"`
	CardNo      string    `json:"cardNo"`
	Qty         float64   `json:"qty"`
	Remaining   float64   `json:"remaining"`
}

// stationCards 現場報數：多張製令在指定站點仍有可報數量的流程卡（?stationId=&wo=id,id）。
func (h *Handler) stationCards(c *gin.Context) {
	stationID, err := uuid.Parse(c.Query("stationId"))
	if err != nil {
		httpx.Fail(c, httpx.Biz("請指定站點"))
		return
	}
	var woIDs []uuid.UUID
	for _, v := range strings.Split(c.Query("wo"), ",") {
		if id, err := uuid.Parse(strings.TrimSpace(v)); err == nil {
			woIDs = append(woIDs, id)
		}
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT fc.work_order_id, fc.card_no, fc.qty, fp.remaining_qty
		FROM flow_cards fc
		JOIN flow_card_progress fp ON fp.card_id = fc.id AND fp.station_id = $3
		WHERE fc.factory_id = $1 AND fc.work_order_id = ANY ($2)
		  AND fc.status IN ('OPEN','IN_PROGRESS') AND fp.remaining_qty > 0
		ORDER BY fc.card_no`, auth.FactoryID(c), woIDs, stationID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[stationCard])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}
