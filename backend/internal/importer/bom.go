package importer

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
	"mes/internal/httpx"
)

var bomSheet = &Sheet{
	Entity: "bom", Title: "BOM（含替代料）",
	Note: "每列一個「母件＋子件」；同一組母件＋子件可重複多列，用來列出多個替代料。依母件＋子件比對：已存在則更新用量等欄位，不存在則新增。品項需先存在（可先匯入產品資料）。",
	Columns: []Column{
		{"parent", "母件品號", true, "成品或半成品"},
		{"child", "子件品號", true, ""},
		{"type", "類型", false, "基本材料／輔助料件（新增時預設 基本材料）"},
		{"qtyPer", "用量", true, "每 1 單位母件的用量"},
		{"scrap", "損耗率%", false, "新增時預設 0"},
		{"timing", "扣帳時機", false, "到站扣帳／出庫扣帳；空白＝依料件預設"},
		{"station", "耗用站點代碼", false, "到站扣帳時在哪一站扣"},
		{"sub", "替代料品號", false, ""},
		{"subRatio", "替代換算比", false, "替代料用量 = 原用量 × 換算比（預設 1）"},
	},
	Examples: [][]string{
		{"SL-003", "UP-001", "基本材料", "1", "0", "到站扣帳", "FORM", "", ""},
		{"SL-003", "SO-BL", "基本材料", "1", "0", "到站扣帳", "FORM", "", ""},
		{"SL-003", "GL-01", "輔助料件", "0.012", "0", "到站扣帳", "FORM", "GL-02", "1.1"},
	},
}

func planBOM(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) {
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

	type line struct {
		id            uuid.UUID
		materialType  string
		qtyPer, scrap float64
		timing        string // 空字串＝依料件預設
		station       *uuid.UUID
		subs          map[uuid.UUID]float64
	}
	type edge struct{ parent, child uuid.UUID }
	ex := map[edge]*line{}
	children := map[uuid.UUID][]uuid.UUID{} // 現有結構，供循環檢查
	maxSort := map[uuid.UUID]int{}
	br, err := q.Query(ctx, `
		SELECT b.id, b.parent_product_id, b.child_product_id, b.material_type, b.qty_per, b.scrap_rate,
		       COALESCE(b.deduct_timing, ''), b.consume_station_id, b.sort_order
		FROM product_bom b WHERE b.factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for br.Next() {
		var k edge
		l := &line{subs: map[uuid.UUID]float64{}}
		var sort int
		if err := br.Scan(&l.id, &k.parent, &k.child, &l.materialType, &l.qtyPer, &l.scrap, &l.timing, &l.station, &sort); err != nil {
			return nil, err
		}
		ex[k] = l
		children[k.parent] = append(children[k.parent], k.child)
		maxSort[k.parent] = max(maxSort[k.parent], sort)
	}
	if err := br.Err(); err != nil {
		return nil, err
	}
	sr, err := q.Query(ctx, `
		SELECT b.parent_product_id, b.child_product_id, s.substitute_product_id, s.ratio
		FROM bom_substitutes s JOIN product_bom b ON b.id = s.bom_id WHERE b.factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for sr.Next() {
		var k edge
		var sub uuid.UUID
		var ratio float64
		if err := sr.Scan(&k.parent, &k.child, &sub, &ratio); err != nil {
			return nil, err
		}
		ex[k].subs[sub] = ratio
	}
	if err := sr.Err(); err != nil {
		return nil, err
	}

	// reaches：from 的展開結構中是否含 to（含本檔案新增的關係）
	reaches := func(from, to uuid.UUID) bool {
		seen := map[uuid.UUID]bool{}
		stack := []uuid.UUID{from}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n == to {
				return true
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			stack = append(stack, children[n]...)
		}
		return false
	}

	type item struct {
		k      edge
		create bool
		l      *line
		subs   []uuid.UUID // 本檔案列出的替代料（依出現順序）
	}
	items := map[edge]*item{}
	var order []edge
	firstRow := map[edge]int{}
	plan := &Plan{}
	for _, r := range rows {
		var e errs
		require(r, &e, bomSheet, "parent", "child", "qtyPer")
		pNo, cNo, sNo := r.get("parent"), r.get("child"), r.get("sub")
		key := pNo + " → " + cNo
		parent, okP := products[pNo]
		child, okC := products[cNo]
		if pNo != "" && !okP {
			e.add("母件品號「%s」不存在", pNo)
		} else if okP && parent.category != "FG" && parent.category != "SFG" {
			e.add("母件須為成品或半成品")
		}
		if cNo != "" && !okC {
			e.add("子件品號「%s」不存在", cNo)
		}
		if okP && okC && parent.id == child.id {
			e.add("子件不可等於母件")
		}
		var sub prod
		if sNo != "" {
			var ok bool
			if sub, ok = products[sNo]; !ok {
				e.add("替代料品號「%s」不存在", sNo)
			} else if sub.id == child.id || sub.id == parent.id {
				e.add("替代料不可等於子件或母件")
			}
		}
		mt := parseEnum(r, "type", "類型", &e, materialTypes)
		qty := parseNum(r, "qtyPer", "用量", &e, 0)
		if qty != nil && *qty == 0 {
			e.add("「用量」必須大於 0")
		}
		scrap := parseNum(r, "scrap", "損耗率%", &e, 0)
		if scrap != nil && *scrap > 100 {
			e.add("「損耗率%%」不可大於 100")
		}
		timing := parseEnum(r, "timing", "扣帳時機", &e, timings)
		var station *uuid.UUID
		if sc := r.get("station"); sc != "" {
			if id, ok := stations[sc]; ok {
				station = &id
			} else {
				e.add("站點代碼「%s」不存在", sc)
			}
		}
		ratio := parseNum(r, "subRatio", "替代換算比", &e, 0)
		if ratio != nil && *ratio == 0 {
			e.add("「替代換算比」必須大於 0")
		}

		k := edge{parent.id, child.id}
		it := items[k]
		if len(e) == 0 && it == nil {
			if okP && okC && ex[k] == nil && reaches(child.id, parent.id) {
				e.add("子件的 BOM 已包含母件，加入會形成循環")
			}
		}
		if len(e) > 0 {
			plan.add(r, key, ActError, e.msg())
			continue
		}

		if it == nil {
			old := ex[k]
			l := &line{materialType: "MAIN", subs: map[uuid.UUID]float64{}}
			if old != nil {
				cp := *old
				cp.subs = map[uuid.UUID]float64{}
				for s, v := range old.subs {
					cp.subs[s] = v
				}
				l = &cp
			} else {
				l.id = httpx.NewID()
				children[k.parent] = append(children[k.parent], k.child)
			}
			if mt != nil {
				l.materialType = *mt
			}
			l.qtyPer = *qty
			if scrap != nil {
				l.scrap = *scrap
			}
			if timing != nil {
				l.timing = *timing
			}
			if station != nil {
				l.station = station
			}
			it = &item{k: k, create: old == nil, l: l}
			items[k] = it
			order = append(order, k)
			firstRow[k] = r.Num
		} else if *qty != it.l.qtyPer || (mt != nil && *mt != it.l.materialType) {
			plan.add(r, key, ActError, "同一母件＋子件的用量或類型與第 "+itoa(firstRow[k])+" 列不一致")
			continue
		}

		var changes []string
		action := ActSame
		if sNo != "" {
			rv := 1.0
			if ratio != nil {
				rv = *ratio
			}
			if oldRatio, had := it.l.subs[sub.id]; !had || oldRatio != rv {
				changes = append(changes, "替代料 "+sNo)
			}
			it.l.subs[sub.id] = rv
			it.subs = append(it.subs, sub.id)
		}
		if it.create {
			action = ActCreate
		} else if old := ex[k]; firstRow[k] == r.Num && (old.materialType != it.l.materialType || old.qtyPer != it.l.qtyPer ||
			old.scrap != it.l.scrap || old.timing != it.l.timing || !sameID(old.station, it.l.station)) {
			action = ActUpdate
			changes = append([]string{"用量／扣帳設定"}, changes...)
		}
		if action == ActSame && len(changes) > 0 {
			action = ActUpdate
		}
		msg := ""
		if action == ActUpdate {
			msg = "更新：" + strings.Join(changes, "、")
		} else if action == ActCreate && len(changes) > 0 {
			msg = "含 " + strings.Join(changes, "、")
		}
		plan.add(r, key, action, msg)
	}

	plan.Apply = func(ctx context.Context, tx pgx.Tx) error {
		for _, k := range order {
			it := items[k]
			l := it.l
			if it.create {
				maxSort[k.parent]++
				if _, err := tx.Exec(ctx, `
					INSERT INTO product_bom (id, factory_id, parent_product_id, child_product_id, material_type, qty_per,
						scrap_rate, deduct_timing, consume_station_id, sort_order)
					VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)`,
					l.id, factoryID, k.parent, k.child, l.materialType, l.qtyPer, l.scrap, l.timing, l.station, maxSort[k.parent]); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `
				UPDATE product_bom SET material_type = $2, qty_per = $3, scrap_rate = $4, deduct_timing = NULLIF($5, ''), consume_station_id = $6
				WHERE id = $1`, l.id, l.materialType, l.qtyPer, l.scrap, l.timing, l.station); err != nil {
				return err
			}
			for i, sub := range it.subs {
				if _, err := tx.Exec(ctx, `
					INSERT INTO bom_substitutes (id, bom_id, substitute_product_id, ratio, priority) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (bom_id, substitute_product_id) DO UPDATE SET ratio = EXCLUDED.ratio, priority = EXCLUDED.priority`,
					httpx.NewID(), l.id, sub, l.subs[sub], i+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return plan, nil
}

func itoa(n int) string { return trimNum(float64(n)) }
