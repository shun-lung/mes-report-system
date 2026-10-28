package dashboard

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mes/internal/auth"
	"mes/internal/httpx"
	"mes/internal/org"
	"mes/internal/production"
)

// 生產 8HR 看板：以班別為時間窗，逐小時追蹤各站各線的產出對目標。
type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) Register(scoped *gin.RouterGroup) {
	scoped.GET("/dashboard/shift", auth.Require("report:view"), h.shift)
}

type Cell struct {
	Good   float64 `json:"good"`
	Defect float64 `json:"defect"`
	Target float64 `json:"target"`
}

type Line struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Capacity     float64   `json:"capacity"`
	Hourly       []Cell    `json:"hourly"`
	Good         float64   `json:"good"`
	Defect       float64   `json:"defect"`
	Target       float64   `json:"target"`
	TargetToDate float64   `json:"targetToDate"`
	ManHours     float64   `json:"manHours"`
	EarnedHours  float64   `json:"earnedHours"`
	Headcount    int       `json:"headcount"`
	CurrentWO    *string   `json:"currentWo"`
	CurrentItem  *string   `json:"currentItem"`
	currentSince time.Time
	LastScanAt   *time.Time `json:"lastScanAt"`
	Status       string     `json:"status"` // RUNNING / IDLE / OFF
}

type Station struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Hourly       []Cell    `json:"hourly"`
	Good         float64   `json:"good"`
	Defect       float64   `json:"defect"`
	Target       float64   `json:"target"`
	TargetToDate float64   `json:"targetToDate"`
	WIP          float64   `json:"wip"`
	IsFinal      bool      `json:"isFinal"` // 成品途程的最後一站（完工站）
	Lines        []*Line   `json:"lines"`
}

type Response struct {
	Factory struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	} `json:"factory"`
	Shift struct {
		ID       uuid.UUID `json:"id"`
		Code     string    `json:"code"`
		Name     string    `json:"name"`
		WorkDate string    `json:"workDate"`
		Start    time.Time `json:"start"`
		End      time.Time `json:"end"`
	} `json:"shift"`
	Shifts []ShiftOption `json:"shifts"`
	Prev   ShiftRef      `json:"prev"`
	Next   ShiftRef      `json:"next"`
	Now    time.Time     `json:"now"`
	Hours  []time.Time   `json:"hours"`
	KPI    struct {
		FGOutput       float64 `json:"fgOutput"`
		SFGOutput      float64 `json:"sfgOutput"`
		FGTarget       float64 `json:"fgTarget"`
		FGTargetToDate float64 `json:"fgTargetToDate"`
		Moves          float64 `json:"moves"`
		Defects        float64 `json:"defects"`
		Yield          float64 `json:"yield"`
		ManHours       float64 `json:"manHours"`
		EarnedHours    float64 `json:"earnedHours"`
		Efficiency     float64 `json:"efficiency"`
		WIP            float64 `json:"wip"`
		PendingAlerts  int     `json:"pendingAlerts"`
		RunningLines   int     `json:"runningLines"`
		TotalLines     int     `json:"totalLines"`
	} `json:"kpi"`
	Stations   []*Station                `json:"stations"`
	WorkOrders []production.WorkOrderRow `json:"workOrders"`
	Defects    []DefectRow               `json:"defects"`
	Recent     []RecentScan              `json:"recent"`
}

type ShiftRef struct {
	ID       uuid.UUID `json:"shiftId"`
	WorkDate string    `json:"date"`
}

type ShiftOption struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type DefectRow struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Station string  `json:"station"`
	Qty     float64 `json:"qty"`
}

type RecentScan struct {
	At      time.Time `json:"at"`
	Line    string    `json:"line"`
	Station string    `json:"station"`
	WONo    string    `json:"woNo"`
	CardNo  string    `json:"cardNo"`
	ItemNo  string    `json:"itemNo"`
	Good    float64   `json:"good"`
	Defect  float64   `json:"defect"`
	User    string    `json:"user"`
}

// 超過此時間沒有掃描視為停滯
const idleAfter = 30 * time.Minute

func (h *Handler) shift(c *gin.Context) {
	factoryID := auth.FactoryID(c)
	now := time.Now()
	var res Response
	res.Now = now

	loc, err := org.Location(c, h.pool, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if err := h.pool.QueryRow(c, `SELECT code, name, timezone FROM factories WHERE id = $1`, factoryID).
		Scan(&res.Factory.Code, &res.Factory.Name, &res.Factory.Timezone); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 班別與生產日：未指定時取目前所在班別
	shiftID, workDate, err := org.Clock(c, h.pool, factoryID, now)
	if err != nil {
		httpx.Fail(c, httpx.Biz("%s", err.Error()))
		return
	}
	if s := c.Query("shiftId"); s != "" {
		if shiftID, err = uuid.Parse(s); err != nil {
			httpx.Abort(c, http.StatusBadRequest, "shiftId 格式錯誤")
			return
		}
	}
	if s := c.Query("date"); s != "" {
		if workDate, err = time.Parse(time.DateOnly, s); err != nil {
			httpx.Abort(c, http.StatusBadRequest, "date 格式錯誤")
			return
		}
	}
	var startClock, endClock string
	var offset int
	err = h.pool.QueryRow(c, `
		SELECT id, code, name, to_char(start_time, 'HH24:MI:SS'), to_char(end_time, 'HH24:MI:SS'), work_date_offset
		FROM shifts WHERE id = $1 AND factory_id = $2`, shiftID, factoryID).
		Scan(&res.Shift.ID, &res.Shift.Code, &res.Shift.Name, &startClock, &endClock, &offset)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	startD, _ := org.ParseClock(startClock)
	endD, _ := org.ParseClock(endClock)
	length := endD - startD
	if length <= 0 {
		length += 24 * time.Hour
	}
	startDate := workDate.AddDate(0, 0, -offset)
	start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, loc).Add(startD)
	end := start.Add(length)
	res.Shift.WorkDate = workDate.Format(time.DateOnly)
	res.Shift.Start, res.Shift.End = start, end
	nHours := int(math.Ceil(length.Hours()))
	for i := 0; i < nHours; i++ {
		res.Hours = append(res.Hours, start.Add(time.Duration(i)*time.Hour))
	}
	upTo := minTime(now, end) // 已發生的區間 [start, upTo)
	for _, ref := range []struct {
		dst *ShiftRef
		at  time.Time
	}{{&res.Prev, start.Add(-time.Minute)}, {&res.Next, end.Add(time.Minute)}} {
		id, wd, err := org.Clock(c, h.pool, factoryID, ref.at)
		if err != nil {
			httpx.DBError(c, err)
			return
		}
		*ref.dst = ShiftRef{ID: id, WorkDate: wd.Format(time.DateOnly)}
	}

	rows, err := h.pool.Query(c, `SELECT id, code, name FROM shifts WHERE factory_id = $1 AND is_active ORDER BY sort_order, start_time`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res.Shifts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ShiftOption]); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 站點與線別骨架
	rows, err = h.pool.Query(c, `
		SELECT s.id, s.code, s.name, l.id, l.code, l.name, l.capacity_per_hour
		FROM work_stations s JOIN production_lines l ON l.station_id = s.id AND l.is_active
		WHERE s.factory_id = $1 AND s.is_active
		ORDER BY s.sort_order, s.code, l.code`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	lines := map[uuid.UUID]*Line{}
	stationOf := map[uuid.UUID]*Station{}
	for rows.Next() {
		var sid uuid.UUID
		var scode, sname string
		l := &Line{Hourly: make([]Cell, nHours), Status: "OFF"}
		if err := rows.Scan(&sid, &scode, &sname, &l.ID, &l.Code, &l.Name, &l.Capacity); err != nil {
			httpx.DBError(c, err)
			return
		}
		if len(res.Stations) == 0 || res.Stations[len(res.Stations)-1].ID != sid {
			res.Stations = append(res.Stations, &Station{ID: sid, Code: scode, Name: sname, Hourly: make([]Cell, nHours)})
		}
		st := res.Stations[len(res.Stations)-1]
		st.Lines = append(st.Lines, l)
		lines[l.ID] = l
		stationOf[l.ID] = st
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 實際產出（小時匯總）
	rows, err = h.pool.Query(c, `
		SELECT line_id, hour_start, sum(good_qty), sum(defect_qty) FROM output_hourly
		WHERE factory_id = $1 AND hour_start >= $2 AND hour_start < $3
		GROUP BY 1, 2`, factoryID, start, end)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	for rows.Next() {
		var lineID uuid.UUID
		var hour time.Time
		var good, defect float64
		if err := rows.Scan(&lineID, &hour, &good, &defect); err != nil {
			httpx.DBError(c, err)
			return
		}
		l, i := lines[lineID], int(hour.Sub(start)/time.Hour)
		if l == nil || i < 0 || i >= nHours {
			continue
		}
		l.Hourly[i].Good += good
		l.Hourly[i].Defect += defect
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 目標與人時：依上工紀錄 × 線別時產能；進行中的上工視為持續到班末
	rows, err = h.pool.Query(c, `
		SELECT s.line_id, s.headcount, s.started_at, s.ended_at, w.wo_no, p.item_no || ' ' || p.name
		FROM line_labor_sessions s
		JOIN work_orders w ON w.id = s.work_order_id
		JOIN products p ON p.id = w.product_id
		WHERE s.factory_id = $1 AND s.started_at < $3 AND (s.ended_at IS NULL OR s.ended_at > $2)`,
		factoryID, start, end)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	for rows.Next() {
		var lineID uuid.UUID
		var headcount int
		var started time.Time
		var ended *time.Time
		var woNo, item string
		if err := rows.Scan(&lineID, &headcount, &started, &ended, &woNo, &item); err != nil {
			httpx.DBError(c, err)
			return
		}
		l := lines[lineID]
		if l == nil {
			continue
		}
		planEnd, actualEnd := end, now
		if ended != nil {
			planEnd, actualEnd = *ended, *ended
		} else {
			l.CurrentWO, l.CurrentItem, l.Headcount, l.currentSince = &woNo, &item, headcount, started
		}
		for i := range l.Hourly {
			hs := start.Add(time.Duration(i) * time.Hour)
			l.Hourly[i].Target += overlap(started, planEnd, hs, minTime(hs.Add(time.Hour), end)).Hours() * l.Capacity
		}
		elapsed := overlap(started, minTime(actualEnd, upTo), start, upTo).Hours()
		l.TargetToDate += elapsed * l.Capacity
		l.ManHours += elapsed * float64(headcount)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 標準人時（良品 × 途程標準人秒）與最後掃描時間
	rows, err = h.pool.Query(c, `
		SELECT ps.line_id, sum(ps.good_qty * rs.std_seconds_per_unit) / 3600, max(ps.scanned_at)
		FROM production_scans ps
		JOIN work_orders w ON w.id = ps.work_order_id
		JOIN routing_steps rs ON rs.routing_id = w.routing_id AND rs.seq = ps.step_seq
		WHERE ps.factory_id = $1 AND ps.scanned_at >= $2 AND ps.scanned_at < $3
		GROUP BY ps.line_id`, factoryID, start, end)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	for rows.Next() {
		var lineID uuid.UUID
		var earned float64
		var last time.Time
		if err := rows.Scan(&lineID, &earned, &last); err != nil {
			httpx.DBError(c, err)
			return
		}
		if l := lines[lineID]; l != nil {
			l.EarnedHours, l.LastScanAt = earned, &last
		}
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 在製量：各站可報未報數量
	rows, err = h.pool.Query(c, `
		SELECT fp.station_id, sum(fp.remaining_qty)
		FROM flow_card_progress fp
		JOIN flow_cards c ON c.id = fp.card_id
		JOIN work_orders w ON w.id = c.work_order_id
		WHERE w.factory_id = $1 AND w.status IN ('RELEASED','IN_PROGRESS') AND c.status NOT IN ('VOID','SPLIT')
		GROUP BY fp.station_id`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	wip := map[uuid.UUID]float64{}
	for rows.Next() {
		var id uuid.UUID
		var qty float64
		if err := rows.Scan(&id, &qty); err != nil {
			httpx.DBError(c, err)
			return
		}
		wip[id] = qty
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 彙總到站與全廠
	k := &res.KPI
	isCurrent := now.After(start) && now.Before(end)
	for _, st := range res.Stations {
		st.WIP = wip[st.ID]
		k.WIP += st.WIP
		for _, l := range st.Lines {
			for i, cell := range l.Hourly {
				l.Good += cell.Good
				l.Defect += cell.Defect
				l.Target += cell.Target
				st.Hourly[i].Good += cell.Good
				st.Hourly[i].Defect += cell.Defect
				st.Hourly[i].Target += cell.Target
			}
			st.Good += l.Good
			st.Defect += l.Defect
			st.Target += l.Target
			st.TargetToDate += l.TargetToDate
			switch {
			case l.CurrentWO == nil:
				l.Status = "OFF"
			case isCurrent && now.Sub(lastActivity(l, start)) > idleAfter:
				l.Status = "IDLE"
			default:
				l.Status = "RUNNING"
			}
			k.TotalLines++
			if l.Status == "RUNNING" {
				k.RunningLines++
			}
			k.ManHours += l.ManHours
			k.EarnedHours += l.EarnedHours
		}
		k.Moves += st.Good
		k.Defects += st.Defect
	}
	if k.Moves+k.Defects > 0 {
		k.Yield = k.Moves / (k.Moves + k.Defects)
	}
	if k.ManHours > 0 {
		k.Efficiency = k.EarnedHours / k.ManHours
	}
	// 完工站：生效中成品途程的最後一站
	rows, err = h.pool.Query(c, `
		SELECT DISTINCT rs.station_id FROM routings r
		JOIN products p ON p.id = r.product_id AND p.category = 'FG'
		JOIN routing_steps rs ON rs.routing_id = r.id
		WHERE r.factory_id = $1 AND r.is_active
		  AND rs.seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = r.id)`, factoryID)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	finals, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	for _, st := range res.Stations {
		for _, id := range finals {
			if st.ID == id {
				st.IsFinal = true
				k.FGTarget += st.Target
				k.FGTargetToDate += st.TargetToDate
			}
		}
	}

	err = h.pool.QueryRow(c, `
		SELECT COALESCE(sum(qty) FILTER (WHERE txn_type = 'FG_IN'), 0),
		       COALESCE(sum(qty) FILTER (WHERE txn_type = 'STATION_IN'), 0)
		FROM inventory_transactions
		WHERE factory_id = $1 AND occurred_at >= $2 AND occurred_at < $3`, factoryID, start, end).
		Scan(&k.FGOutput, &k.SFGOutput)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if err := h.pool.QueryRow(c, `SELECT count(*) FROM material_alerts WHERE factory_id = $1 AND status = 'PENDING'`,
		factoryID).Scan(&k.PendingAlerts); err != nil {
		httpx.DBError(c, err)
		return
	}

	// 製令進度：未結製令 + 本班完工
	rows, err = h.pool.Query(c, production.WorkOrderSelect+`
		WHERE w.factory_id = $1
		  AND (w.status IN ('DRAFT','RELEASED','SCHEDULED','IN_PROGRESS') OR w.completed_at >= $2)
		ORDER BY w.due_date NULLS LAST, w.wo_no`, factoryID, start)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res.WorkOrders, err = pgx.CollectRows(rows, pgx.RowToStructByPos[production.WorkOrderRow]); err != nil {
		httpx.DBError(c, err)
		return
	}

	rows, err = h.pool.Query(c, `
		SELECT dc.code, dc.name, st.name, sum(dr.qty)
		FROM defect_records dr
		JOIN production_scans ps ON ps.id = dr.scan_id
		JOIN defect_codes dc ON dc.id = dr.defect_code_id
		JOIN work_stations st ON st.id = ps.station_id
		WHERE ps.factory_id = $1 AND ps.scanned_at >= $2 AND ps.scanned_at < $3
		GROUP BY dc.code, dc.name, st.name, st.sort_order
		ORDER BY sum(dr.qty) DESC LIMIT 8`, factoryID, start, end)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res.Defects, err = pgx.CollectRows(rows, pgx.RowToStructByPos[DefectRow]); err != nil {
		httpx.DBError(c, err)
		return
	}

	rows, err = h.pool.Query(c, `
		SELECT ps.scanned_at, l.code, st.name, w.wo_no, fc.card_no, p.item_no, ps.good_qty, ps.defect_qty, u.name
		FROM production_scans ps
		JOIN production_lines l ON l.id = ps.line_id
		JOIN work_stations st ON st.id = ps.station_id
		JOIN work_orders w ON w.id = ps.work_order_id
		JOIN flow_cards fc ON fc.id = ps.card_id
		JOIN products p ON p.id = ps.product_id
		JOIN users u ON u.id = ps.user_id
		WHERE ps.factory_id = $1 AND ps.scanned_at >= $2 AND ps.scanned_at < $3
		ORDER BY ps.scanned_at DESC LIMIT 15`, factoryID, start, end)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	if res.Recent, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RecentScan]); err != nil {
		httpx.DBError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func overlap(aStart, aEnd, bStart, bEnd time.Time) time.Duration {
	s, e := maxTime(aStart, bStart), minTime(aEnd, bEnd)
	if e.Before(s) {
		return 0
	}
	return e.Sub(s)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// lastActivity 為判斷停滯的基準：最後掃描、本次上工、班別開始三者取最晚。
func lastActivity(l *Line, shiftStart time.Time) time.Time {
	t := maxTime(l.currentSince, shiftStart)
	if l.LastScanAt != nil {
		t = maxTime(t, *l.LastScanAt)
	}
	return t
}
