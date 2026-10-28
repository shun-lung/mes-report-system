package importer

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
	"mes/internal/httpx"
)

var stationSheet = &Sheet{
	Entity: "stations", Title: "站點與線別",
	Note: "每列一條線別；只填站點欄位（線別代碼空白）則只維護站點。依「站點代碼」「線別代碼」比對：已存在則更新，不存在則新增。",
	Columns: []Column{
		{"stationCode", "站點代碼", true, ""},
		{"stationName", "站點名稱", true, "同一站點在各列需一致"},
		{"order", "站點順序", false, "數字越小越前面（新增時排在最後）"},
		{"lineCode", "線別代碼", false, "全廠唯一；空白表示這列只維護站點"},
		{"lineName", "線別名稱", false, "有線別代碼時必填"},
		{"capacity", "時產能", false, "每小時產量（雙）"},
		{"headcount", "標準人數", false, ""},
		{"active", "使用中", false, "是／否；套用在線別（無線別時套用在站點）"},
	},
	Examples: [][]string{
		{"QC", "品質檢驗", "50", "QC-A", "品檢 A 線", "200", "2", "是"},
		{"QC", "品質檢驗", "50", "QC-B", "品檢 B 線", "200", "2", "是"},
	},
}

func planStations(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) {
	type station struct {
		id     uuid.UUID
		name   string
		order  int
		active bool
	}
	type line struct {
		id, stationID uuid.UUID
		name          string
		capacity      float64
		headcount     int
		active        bool
	}
	stations := map[string]*station{}
	lines := map[string]*line{}
	maxOrder := 0
	rs, err := q.Query(ctx, `SELECT id, code, name, sort_order, is_active FROM work_stations WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for rs.Next() {
		var code string
		s := &station{}
		if err := rs.Scan(&s.id, &code, &s.name, &s.order, &s.active); err != nil {
			return nil, err
		}
		stations[code] = s
		maxOrder = max(maxOrder, s.order)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	rs, err = q.Query(ctx, `
		SELECT id, code, station_id, name, capacity_per_hour, std_headcount, is_active FROM production_lines WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for rs.Next() {
		var code string
		l := &line{}
		if err := rs.Scan(&l.id, &code, &l.stationID, &l.name, &l.capacity, &l.headcount, &l.active); err != nil {
			return nil, err
		}
		lines[code] = l
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}

	// 本檔案要寫入的站點與線別（同代碼只保留一份）
	newStations := map[string]*station{}
	createdStations := map[string]bool{}
	newLines := map[string]*line{}
	createdLines := map[string]bool{}
	var stationOrder, lineOrder []string
	seenLine := map[string]int{}
	plan := &Plan{}

	for _, r := range rows {
		var e errs
		require(r, &e, stationSheet, "stationCode", "stationName")
		code, lineCode := r.get("stationCode"), r.get("lineCode")
		key := code
		if lineCode != "" {
			key += " / " + lineCode
		}
		order := parseNum(r, "order", "站點順序", &e, 0)
		capacity := parseNum(r, "capacity", "時產能", &e, 0)
		headcount := parseNum(r, "headcount", "標準人數", &e, 0)
		active := parseBool(r, "active", "使用中", &e)

		// 站點
		st := newStations[code]
		if st == nil && code != "" {
			if old := stations[code]; old != nil {
				cp := *old
				st = &cp
			} else {
				maxOrder += 10
				st = &station{id: httpx.NewID(), order: maxOrder, active: true}
				createdStations[code] = true
			}
		} else if st != nil && r.get("stationName") != "" && st.name != r.get("stationName") {
			e.add("同一站點「%s」的名稱與前面的列不一致", code)
		}
		if st != nil {
			if s := r.get("stationName"); s != "" {
				st.name = s
			}
			if order != nil {
				st.order = int(*order)
			}
			if lineCode == "" && active != nil {
				st.active = *active
			}
		}

		// 線別
		var ln *line
		if lineCode != "" {
			if n, dup := seenLine[lineCode]; dup {
				e.add("線別代碼與第 %d 列重複", n)
			}
			seenLine[lineCode] = r.Num
			if old := lines[lineCode]; old != nil {
				cp := *old
				ln = &cp
			} else {
				ln = &line{id: httpx.NewID(), active: true}
				if r.get("lineName") == "" {
					e.add("新增線別時「線別名稱」必填")
				}
			}
			if st != nil {
				ln.stationID = st.id
			}
			if s := r.get("lineName"); s != "" {
				ln.name = s
			}
			if capacity != nil {
				ln.capacity = *capacity
			}
			if headcount != nil {
				ln.headcount = int(*headcount)
			}
			if active != nil {
				ln.active = *active
			}
		}

		if len(e) > 0 {
			plan.add(r, key, ActError, e.msg())
			continue
		}
		if _, ok := newStations[code]; !ok {
			stationOrder = append(stationOrder, code)
		}
		newStations[code] = st

		var changes []string
		action := ActSame
		if old := stations[code]; old == nil {
			action, changes = ActCreate, append(changes, "新站點")
		} else if old.name != st.name || old.order != st.order || old.active != st.active {
			action, changes = ActUpdate, append(changes, "站點")
		}
		if ln != nil {
			newLines[lineCode], lineOrder = ln, append(lineOrder, lineCode)
			if old := lines[lineCode]; old == nil {
				createdLines[lineCode] = true
				action = ActCreate
				changes = append(changes, "新線別")
			} else if old.stationID != ln.stationID || old.name != ln.name || old.capacity != ln.capacity ||
				old.headcount != ln.headcount || old.active != ln.active {
				if action == ActSame {
					action = ActUpdate
				}
				changes = append(changes, "線別")
			}
		}
		msg := ""
		if action != ActSame {
			msg = strings.Join(changes, "、")
		}
		plan.add(r, key, action, msg)
	}

	plan.Apply = func(ctx context.Context, tx pgx.Tx) error {
		for _, code := range stationOrder {
			s := newStations[code]
			if createdStations[code] {
				if _, err := tx.Exec(ctx, `INSERT INTO work_stations (id, factory_id, code, name, sort_order, is_active) VALUES ($1, $2, $3, $4, $5, $6)`,
					s.id, factoryID, code, s.name, s.order, s.active); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE work_stations SET name = $2, sort_order = $3, is_active = $4 WHERE id = $1`,
				s.id, s.name, s.order, s.active); err != nil {
				return err
			}
		}
		for _, code := range lineOrder {
			l := newLines[code]
			if createdLines[code] {
				if _, err := tx.Exec(ctx, `
					INSERT INTO production_lines (id, factory_id, station_id, code, name, capacity_per_hour, std_headcount, is_active)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
					l.id, factoryID, l.stationID, code, l.name, l.capacity, l.headcount, l.active); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `
				UPDATE production_lines SET station_id = $2, name = $3, capacity_per_hour = $4, std_headcount = $5, is_active = $6 WHERE id = $1`,
				l.id, l.stationID, l.name, l.capacity, l.headcount, l.active); err != nil {
				return err
			}
		}
		return nil
	}
	return plan, nil
}
