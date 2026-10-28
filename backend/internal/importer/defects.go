package importer

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
	"mes/internal/httpx"
)

var defectSheet = &Sheet{
	Entity: "defects", Title: "不良代碼",
	Note: "依「代碼」比對：已存在則更新，不存在則新增。站點代碼空白表示全站通用。",
	Columns: []Column{
		{"code", "代碼", true, "唯一代碼"},
		{"name", "名稱", true, ""},
		{"stationCode", "站點代碼", false, "需已存在；空白＝全站通用"},
		{"category", "分類", false, "例如 外觀、尺寸、車縫"},
		{"active", "使用中", false, "是／否（新增時預設 是）"},
	},
	Examples: [][]string{
		{"Q01", "尺碼標錯", "QC", "標示", "是"},
		{"G01", "其他", "", "", "是"},
	},
}

func planDefects(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) {
	stations, err := codeMap(ctx, q, `SELECT code, id FROM work_stations WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	type defect struct {
		id             uuid.UUID
		stationID      *uuid.UUID
		name, category string
		active         bool
	}
	ex := map[string]*defect{}
	rs, err := q.Query(ctx, `SELECT id, code, station_id, name, COALESCE(category, ''), is_active FROM defect_codes WHERE factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for rs.Next() {
		var code string
		d := &defect{}
		if err := rs.Scan(&d.id, &code, &d.stationID, &d.name, &d.category, &d.active); err != nil {
			return nil, err
		}
		ex[code] = d
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}

	type item struct {
		code   string
		create bool
		d      defect
	}
	var items []item
	seen := map[string]int{}
	plan := &Plan{}
	for _, r := range rows {
		var e errs
		require(r, &e, defectSheet, "code", "name")
		code := r.get("code")
		if n, dup := seen[code]; dup && code != "" {
			e.add("代碼與第 %d 列重複", n)
		}
		seen[code] = r.Num
		old := ex[code]
		d := defect{id: httpx.NewID(), active: true}
		if old != nil {
			d = *old
		}
		if s := r.get("name"); s != "" {
			d.name = s
		}
		if s := r.get("category"); s != "" {
			d.category = s
		}
		if sc := r.get("stationCode"); sc != "" {
			if id, ok := stations[sc]; ok {
				d.stationID = &id
			} else {
				e.add("站點代碼「%s」不存在", sc)
			}
		} else if old == nil {
			d.stationID = nil
		}
		if b := parseBool(r, "active", "使用中", &e); b != nil {
			d.active = *b
		}
		switch {
		case len(e) > 0:
			plan.add(r, code, ActError, e.msg())
		case old == nil:
			plan.add(r, code, ActCreate, "")
			items = append(items, item{code: code, create: true, d: d})
		default:
			var c []string
			if old.name != d.name {
				c = append(c, "名稱")
			}
			if old.category != d.category {
				c = append(c, "分類")
			}
			if !sameID(old.stationID, d.stationID) {
				c = append(c, "站點")
			}
			if old.active != d.active {
				c = append(c, "使用中")
			}
			if len(c) == 0 {
				plan.add(r, code, ActSame, "")
				continue
			}
			plan.add(r, code, ActUpdate, "更新："+strings.Join(c, "、"))
			items = append(items, item{code: code, d: d})
		}
	}
	plan.Apply = func(ctx context.Context, tx pgx.Tx) error {
		for _, it := range items {
			d := it.d
			if it.create {
				if _, err := tx.Exec(ctx, `
					INSERT INTO defect_codes (id, factory_id, station_id, code, name, category, is_active) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
					d.id, factoryID, d.stationID, it.code, d.name, d.category, d.active); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `
				UPDATE defect_codes SET station_id = $2, name = $3, category = NULLIF($4, ''), is_active = $5 WHERE id = $1`,
				d.id, d.stationID, d.name, d.category, d.active); err != nil {
				return err
			}
		}
		return nil
	}
	return plan, nil
}

// codeMap 查出「代碼 → id」對照。
func codeMap(ctx context.Context, q database.Querier, sql string, factoryID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := q.Query(ctx, sql, factoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]uuid.UUID{}
	for rows.Next() {
		var code string
		var id uuid.UUID
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		m[code] = id
	}
	return m, rows.Err()
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
