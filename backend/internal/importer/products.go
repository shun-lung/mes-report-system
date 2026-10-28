package importer

import (
	"context"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
	"mes/internal/httpx"
)

var productSheet = &Sheet{
	Entity: "products", Title: "產品資料",
	Note: "依「品號」比對：已存在則更新，不存在則新增。更新時空白的選填欄位沿用原值。",
	Columns: []Column{
		{"itemNo", "品號", true, "唯一代碼"},
		{"name", "品名", true, ""},
		{"category", "類別", true, "成品／半成品／基本材料／輔助料件"},
		{"unit", "單位", true, "例如 雙、碼、KG、個"},
		{"barcode", "條碼", false, "不可與其他品項重複"},
		{"spec", "規格", false, ""},
		{"timing", "預設扣帳時機", false, "到站扣帳／出庫扣帳（新增時預設出庫扣帳）"},
		{"cardQty", "每卡數量", false, "成品、半成品流程卡每張數量（新增時預設 100）"},
		{"alertPct", "預警緩衝%", false, "缺料預警緩衝（新增時預設 3）"},
		{"safety", "安全庫存", false, "參考用（新增時預設 0）"},
		{"active", "使用中", false, "是／否（新增時預設 是）"},
	},
	Examples: [][]string{
		{"SL-003", "EVA 一字拖鞋-藍", "成品", "雙", "4712345000035", "EVA 一體大底／布面一字帶", "出庫扣帳", "100", "3", "0", "是"},
		{"SO-BL", "EVA 大底-藍", "基本材料", "雙", "4712345200046", "EVA 射出大底", "到站扣帳", "", "", "500", "是"},
	},
}

type productVals struct {
	name, barcode, spec, unit, category, timing string
	safety, alertPct                            float64
	cardQty                                     int
	active                                      bool
}

func planProducts(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) {
	type existing struct {
		id   uuid.UUID
		v    productVals
		used bool // 已有 BOM 或途程
	}
	ex := map[string]*existing{}
	barcodeOwner := map[string]string{}
	dbRows, err := q.Query(ctx, `
		SELECT p.id, p.item_no, p.name, COALESCE(p.barcode, ''), COALESCE(p.spec, ''), p.unit, p.category,
		       p.default_deduct_timing, p.safety_stock, p.alert_pct, p.card_qty, p.is_active,
		       EXISTS (SELECT 1 FROM product_bom b WHERE b.parent_product_id = p.id)
		       OR EXISTS (SELECT 1 FROM routings r WHERE r.product_id = p.id)
		FROM products p WHERE p.factory_id = $1`, factoryID)
	if err != nil {
		return nil, err
	}
	for dbRows.Next() {
		var itemNo string
		e := &existing{}
		if err := dbRows.Scan(&e.id, &itemNo, &e.v.name, &e.v.barcode, &e.v.spec, &e.v.unit, &e.v.category, &e.v.timing,
			&e.v.safety, &e.v.alertPct, &e.v.cardQty, &e.v.active, &e.used); err != nil {
			return nil, err
		}
		ex[itemNo] = e
		if e.v.barcode != "" {
			barcodeOwner[e.v.barcode] = itemNo
		}
	}
	if err := dbRows.Err(); err != nil {
		return nil, err
	}

	type item struct {
		itemNo string
		id     uuid.UUID
		create bool
		v      productVals
	}
	var items []item
	plan := &Plan{}
	seen := map[string]int{}
	fileBarcode := map[string]string{}
	for _, r := range rows {
		var e errs
		require(r, &e, productSheet, "itemNo", "name", "category", "unit")
		itemNo := r.get("itemNo")
		if n, dup := seen[itemNo]; dup && itemNo != "" {
			e.add("品號與第 %d 列重複", n)
		}
		seen[itemNo] = r.Num

		old := ex[itemNo]
		v := productVals{unit: "雙", category: "FG", timing: "ON_ISSUE", alertPct: 3, cardQty: 100, active: true}
		if old != nil {
			v = old.v
		}
		if s := r.get("name"); s != "" {
			v.name = s
		}
		if s := r.get("unit"); s != "" {
			v.unit = s
		}
		if s := r.get("barcode"); s != "" {
			v.barcode = s
		}
		if s := r.get("spec"); s != "" {
			v.spec = s
		}
		if c := parseEnum(r, "category", "類別", &e, categories); c != nil {
			v.category = *c
		}
		if t := parseEnum(r, "timing", "預設扣帳時機", &e, timings); t != nil {
			v.timing = *t
		}
		if n := parseNum(r, "cardQty", "每卡數量", &e, 1); n != nil {
			if *n != math.Trunc(*n) {
				e.add("「每卡數量」必須是整數")
			}
			v.cardQty = int(*n)
		}
		if n := parseNum(r, "alertPct", "預警緩衝%", &e, 0); n != nil {
			v.alertPct = *n
		}
		if n := parseNum(r, "safety", "安全庫存", &e, 0); n != nil {
			v.safety = *n
		}
		if b := parseBool(r, "active", "使用中", &e); b != nil {
			v.active = *b
		}
		if len([]rune(v.unit)) > 10 {
			e.add("「單位」最多 10 個字")
		}
		if v.barcode != "" {
			if owner, ok := barcodeOwner[v.barcode]; ok && owner != itemNo {
				e.add("條碼已被品號 %s 使用", owner)
			}
			if other, ok := fileBarcode[v.barcode]; ok && other != itemNo {
				e.add("條碼與本檔案品號 %s 重複", other)
			}
			fileBarcode[v.barcode] = itemNo
		}
		if old != nil && old.used && (v.category == "RAW" || v.category == "AUX") {
			e.add("此品項已有 BOM 或途程，不可改為材料類別")
		}

		switch {
		case len(e) > 0:
			plan.add(r, itemNo, ActError, e.msg())
		case old == nil:
			plan.add(r, itemNo, ActCreate, "")
			items = append(items, item{itemNo: itemNo, id: httpx.NewID(), create: true, v: v})
		default:
			if changed := diffProduct(old.v, v); len(changed) > 0 {
				plan.add(r, itemNo, ActUpdate, "更新："+strings.Join(changed, "、"))
				items = append(items, item{itemNo: itemNo, id: old.id, v: v})
			} else {
				plan.add(r, itemNo, ActSame, "")
			}
		}
	}

	plan.Apply = func(ctx context.Context, tx pgx.Tx) error {
		for _, it := range items {
			v := it.v
			if it.create {
				if _, err := tx.Exec(ctx, `
					INSERT INTO products (id, factory_id, item_no, name, barcode, spec, unit, category, default_deduct_timing,
						safety_stock, alert_pct, card_qty, is_active)
					VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, $11, $12, $13)`,
					it.id, factoryID, it.itemNo, v.name, v.barcode, v.spec, v.unit, v.category, v.timing,
					v.safety, v.alertPct, v.cardQty, v.active); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `
				UPDATE products SET name = $2, barcode = NULLIF($3, ''), spec = NULLIF($4, ''), unit = $5, category = $6,
				       default_deduct_timing = $7, safety_stock = $8, alert_pct = $9, card_qty = $10, is_active = $11, updated_at = now()
				WHERE id = $1`,
				it.id, v.name, v.barcode, v.spec, v.unit, v.category, v.timing, v.safety, v.alertPct, v.cardQty, v.active); err != nil {
				return err
			}
		}
		return nil
	}
	return plan, nil
}

func diffProduct(a, b productVals) []string {
	var c []string
	add := func(changed bool, name string) {
		if changed {
			c = append(c, name)
		}
	}
	add(a.name != b.name, "品名")
	add(a.barcode != b.barcode, "條碼")
	add(a.spec != b.spec, "規格")
	add(a.unit != b.unit, "單位")
	add(a.category != b.category, "類別")
	add(a.timing != b.timing, "扣帳時機")
	add(a.cardQty != b.cardQty, "每卡數量")
	add(a.alertPct != b.alertPct, "預警緩衝")
	add(a.safety != b.safety, "安全庫存")
	add(a.active != b.active, "使用中")
	return c
}
