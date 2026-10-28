package importer

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
	"mes/internal/httpx"
)

var routingSheet = &Sheet{
	Entity: "routings", Title: "途程",
	Note: "每列一個站點，同一品號的各列組成一條途程，依「站序」排列。與目前生效版本相同則不變；不同則建立新版本並設為生效（已開立的製令維持原版本）。",
	Columns: []Column{
		{"itemNo", "品號", true, "成品或半成品"},
		{"seq", "站序", true, "數字，決定先後（例如 10、20、30）"},
		{"station", "站點代碼", true, "需已存在；同一途程不可重複"},
		{"std", "標準工時（人秒）", false, "每單位標準人秒 = 人數 × 3600 ÷ 時產能（預設 0）"},
		{"note", "版本說明", false, "同一品號填一次即可"},
	},
	Examples: [][]string{
		{"SL-003", "10", "FORM", "565", "藍色款初版"},
		{"SL-003", "20", "FIN", "170", ""},
	},
}

func planRoutings(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) {
	type prod struct {
		id       uuid.UUID
		category string
	}
	products := map[string]prod{}
	pr, err := q.Query(ctx, `SELECT item_no, id, category FROM products WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var no string
		var p prod
		if err := pr.Scan(&no, &p.id, &p.category); err != nil {
			return nil, err
		}
		products[no] = p
	}
	if err := pr.Err(); err != nil {
		return nil, err
	}
	stations, err := codeMap(ctx, q, `SELECT code, id FROM work_stations WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}

	type step struct {
		seq     float64
		station uuid.UUID
		std     float64
		row     Row
	}
	type group struct {
		itemNo string
		prod   prod
		steps  []step
		note   string
		bad    bool
	}
	groups := map[string]*group{}
	var order []string
	plan := &Plan{}
	rowErr := map[int]string{}

	for _, r := range rows {
		var e errs
		require(r, &e, routingSheet, "itemNo", "seq", "station")
		itemNo := r.get("itemNo")
		p, ok := products[itemNo]
		if itemNo != "" && !ok {
			e.add("品號「%s」不存在", itemNo)
		} else if ok && p.category != "FG" && p.category != "SFG" {
			e.add("只有成品或半成品需要途程")
		}
		seq := parseNum(r, "seq", "站序", &e, 0)
		std := parseNum(r, "std", "標準工時（人秒）", &e, 0)
		var stID uuid.UUID
		if sc := r.get("station"); sc != "" {
			if id, ok := stations[sc]; ok {
				stID = id
			} else {
				e.add("站點代碼「%s」不存在", sc)
			}
		}
		g := groups[itemNo]
		if g == nil {
			g = &group{itemNo: itemNo, prod: p}
			groups[itemNo] = g
			order = append(order, itemNo)
		}
		if len(e) == 0 {
			for _, s := range g.steps {
				if s.station == stID {
					e.add("站點「%s」在此途程中重複（第 %d 列）", r.get("station"), s.row.Num)
				}
				if s.seq == *seq {
					e.add("站序 %g 重複（第 %d 列）", *seq, s.row.Num)
				}
			}
		}
		if len(e) > 0 {
			g.bad = true
			rowErr[r.Num] = e.msg()
		}
		st := step{row: r, station: stID}
		if seq != nil {
			st.seq = *seq
		}
		if std != nil {
			st.std = *std
		}
		g.steps = append(g.steps, st)
		if n := r.get("note"); n != "" && g.note == "" {
			g.note = n
		}
	}

	type newRouting struct {
		g       *group
		version int
	}
	var creates []newRouting
	for _, itemNo := range order {
		g := groups[itemNo]
		sort.SliceStable(g.steps, func(i, j int) bool { return g.steps[i].seq < g.steps[j].seq })
		if g.bad {
			for _, s := range g.steps {
				msg := rowErr[s.row.Num]
				if msg == "" {
					msg = "同一品號的其他列有錯誤"
				}
				plan.add(s.row, itemNo, ActError, msg)
			}
			continue
		}
		// 與生效版本比較
		var current []step
		var maxVer int
		rs, err := q.Query(ctx, `
			SELECT rs.station_id, rs.std_seconds_per_unit FROM routings r JOIN routing_steps rs ON rs.routing_id = r.id
			WHERE r.product_id = $1 AND r.is_active ORDER BY rs.seq`, g.prod.id)
		if err != nil {
			return nil, err
		}
		for rs.Next() {
			var s step
			if err := rs.Scan(&s.station, &s.std); err != nil {
				return nil, err
			}
			current = append(current, s)
		}
		if err := rs.Err(); err != nil {
			return nil, err
		}
		if err := q.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM routings WHERE product_id = $1`, g.prod.id).Scan(&maxVer); err != nil {
			return nil, err
		}
		same := len(current) == len(g.steps)
		for i := 0; same && i < len(current); i++ {
			same = current[i].station == g.steps[i].station && current[i].std == g.steps[i].std
		}
		action, msg := ActCreate, fmt.Sprintf("建立 v%d 並生效（%d 站）", maxVer+1, len(g.steps))
		switch {
		case same:
			action, msg = ActSame, "與生效版本相同"
		case maxVer > 0:
			action = ActUpdate
		}
		for _, s := range g.steps {
			plan.add(s.row, itemNo, action, msg)
		}
		if action != ActSame {
			creates = append(creates, newRouting{g: g, version: maxVer + 1})
		}
	}

	plan.Apply = func(ctx context.Context, tx pgx.Tx) error {
		for _, c := range creates {
			// 鎖住產品，避免同時建立相同版本號
			if _, err := tx.Exec(ctx, `SELECT 1 FROM products WHERE id = $1 FOR UPDATE`, c.g.prod.id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE routings SET is_active = false WHERE product_id = $1`, c.g.prod.id); err != nil {
				return err
			}
			id := httpx.NewID()
			note := c.g.note
			if note == "" {
				note = "Excel 匯入"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO routings (id, factory_id, product_id, version, is_active, note) VALUES ($1, $2, $3, $4, true, $5)`,
				id, factoryID, c.g.prod.id, c.version, note); err != nil {
				return err
			}
			for i, s := range c.g.steps {
				if _, err := tx.Exec(ctx, `
					INSERT INTO routing_steps (id, routing_id, seq, station_id, std_seconds_per_unit) VALUES ($1, $2, $3, $4, $5)`,
					httpx.NewID(), id, (i+1)*10, s.station, s.std); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return plan, nil
}
