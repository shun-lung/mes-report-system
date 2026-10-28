// demo 重建一套「拖鞋」示範資料，並模擬今日早班現場回報。
//
//	go run ./cmd/demo          （會清空資料庫所有資料！）
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"mes/internal/auth"
	"mes/internal/config"
	"mes/internal/database"
	"mes/internal/httpx"
	"mes/internal/inventory"
	"mes/internal/org"
	"mes/internal/production"
)

type demo struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	svc     *production.Service
	rng     *rand.Rand
	loc     *time.Location
	factory uuid.UUID
	users   map[string]uuid.UUID // 角色代碼 → 使用者
	station map[string]uuid.UUID
	product map[string]uuid.UUID
	whWIP   uuid.UUID
	defects map[string][]defectCode // 站點代碼 → 不良代碼
	lines   []*simLine
	start   time.Time // 早班開始
}

type defectCode struct {
	id          uuid.UUID
	disposition string
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	if err := database.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	d := &demo{
		ctx: ctx, pool: pool, svc: production.NewService(pool),
		rng:   rand.New(rand.NewPCG(20261027, 8)),
		users: map[string]uuid.UUID{}, station: map[string]uuid.UUID{}, product: map[string]uuid.UUID{},
		defects: map[string][]defectCode{},
	}
	d.loc, _ = time.LoadLocation("Asia/Taipei")
	now := time.Now().In(d.loc)
	d.start = time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, d.loc)
	if now.Before(d.start.Add(time.Hour)) {
		d.start = d.start.AddDate(0, 0, -1)
	}

	d.must(d.exec(`TRUNCATE factories, users, permissions, audit_logs CASCADE`))
	d.must(auth.Seed(ctx, pool, cfg.AdminEmail, cfg.AdminPassword))
	d.setupOrg()
	d.setupAccess(cfg.AdminEmail, cfg.DemoPassword)
	d.setupMasterData()
	d.orderFlow()
	d.simulate(now)
	d.summary(cfg.AdminPassword == "admin1234" && cfg.DemoPassword == "Demo1234!")
}

func (d *demo) exec(sql string, args ...any) error {
	_, err := d.pool.Exec(d.ctx, sql, args...)
	return err
}

func (d *demo) must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (d *demo) at(minutesFromStart int) time.Time {
	return d.start.Add(time.Duration(minutesFromStart) * time.Minute)
}

func step(format string, args ...any) {
	fmt.Printf("▸ "+format+"\n", args...)
}

// ───────────────────────── 組織與權限 ─────────────────────────

func (d *demo) setupOrg() {
	d.factory = httpx.NewID()
	d.must(d.exec(`INSERT INTO factories (id, code, name, timezone) VALUES ($1, 'TW1', '台灣一廠', 'Asia/Taipei')`, d.factory))
	for i, s := range []struct {
		code, name, start, end string
		offset                 int
	}{
		{"D", "早班", "08:00", "16:00", 0},
		{"M", "中班", "16:00", "00:00", 0},
		{"N", "夜班", "00:00", "08:00", -1},
	} {
		d.must(d.exec(`INSERT INTO shifts (id, factory_id, code, name, start_time, end_time, work_date_offset, sort_order)
			VALUES ($1, $2, $3, $4, $5::time, $6::time, $7, $8)`, httpx.NewID(), d.factory, s.code, s.name, s.start, s.end, s.offset, i))
	}
	step("工廠 TW1 台灣一廠（Asia/Taipei），三班制：早班 08–16、中班 16–24、夜班 00–08（算前一天）")
}

func (d *demo) setupAccess(adminEmail, demoPassword string) {
	all := make([]string, len(auth.Catalog))
	for i, p := range auth.Catalog {
		all[i] = p.Code
	}
	roles := []struct {
		code, name string
		perms      []string
		email      string
		user       string
		empNo      string
	}{
		{"ADMIN", "最高權限", all, adminEmail, "", ""},
		{"PC", "生管", []string{"station:view", "product:view", "bom:view", "routing:view",
			"wo:view", "wo:edit", "wo:release", "alert:decide", "inventory:view", "report:view"},
			"pc@demo.local", "王生管", "E1001"},
		{"WH", "倉庫管理", []string{"product:view", "bom:view", "wo:view",
			"inventory:view", "inventory:edit", "inventory:adjust", "report:view"},
			"wh@demo.local", "李倉管", "E2001"},
		{"LEADER", "現場組長", []string{"station:view", "wo:view", "scan:report", "labor:report", "report:view"},
			"leader@demo.local", "陳組長", "E3001"},
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(demoPassword), bcrypt.DefaultCost)
	for _, r := range roles {
		roleID := httpx.NewID()
		d.must(d.exec(`INSERT INTO roles (id, factory_id, code, name, is_system) VALUES ($1, $2, $3, $4, $5)`,
			roleID, d.factory, r.code, r.name, r.code == "ADMIN"))
		for _, p := range r.perms {
			d.must(d.exec(`INSERT INTO role_permissions (role_id, permission_code) VALUES ($1, $2)`, roleID, p))
		}
		var userID uuid.UUID
		if r.user == "" {
			d.must(d.pool.QueryRow(d.ctx, `SELECT id FROM users WHERE email = $1`, r.email).Scan(&userID))
		} else {
			userID = httpx.NewID()
			d.must(d.exec(`INSERT INTO users (id, email, name, password_hash, employee_no) VALUES ($1, $2, $3, $4, $5)`,
				userID, r.email, r.user, string(hash), r.empNo))
		}
		d.must(d.exec(`INSERT INTO user_factory_roles (user_id, factory_id, role_id) VALUES ($1, $2, $3)`, userID, d.factory, roleID))
		d.users[r.code] = userID
		step("角色「%s」%d 項權限 → %s", r.name, len(r.perms), r.email)
	}
}

// ───────────────────────── 基本資料 ─────────────────────────

func (d *demo) setupMasterData() {
	type lineDef struct {
		code     string
		capacity float64
		hc       int
	}
	stations := []struct {
		code, name string
		lines      []lineDef
	}{
		{"PREP", "前期準備", []lineDef{{"PREP-A", 150, 4}, {"PREP-B", 150, 4}}},
		{"SEW", "針車", []lineDef{{"SEW-A", 60, 8}, {"SEW-B", 60, 8}, {"SEW-C", 60, 8}, {"SEW-D", 60, 8}}},
		{"FORM", "成型", []lineDef{{"FORM-A", 60, 10}, {"FORM-B", 60, 10}, {"FORM-C", 60, 10}, {"FORM-D", 60, 10}}},
		{"FIN", "整理", []lineDef{{"FIN-A", 120, 6}, {"FIN-B", 120, 6}}},
	}
	for i, s := range stations {
		id := httpx.NewID()
		d.station[s.code] = id
		d.must(d.exec(`INSERT INTO work_stations (id, factory_id, code, name, sort_order) VALUES ($1, $2, $3, $4, $5)`,
			id, d.factory, s.code, s.name, (i+1)*10))
		for _, l := range s.lines {
			lid := httpx.NewID()
			d.must(d.exec(`INSERT INTO production_lines (id, factory_id, station_id, code, name, capacity_per_hour, std_headcount)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, lid, d.factory, id, l.code, s.name+" "+l.code[len(l.code)-1:]+" 線", l.capacity, l.hc))
			d.lines = append(d.lines, &simLine{id: lid, code: l.code, station: s.code, stationID: id, capacity: l.capacity, headcount: l.hc, efficiency: 0.92})
		}
	}
	step("站點 4 個（前期準備 → 針車 → 成型 → 整理），線別 12 條")

	products := []struct {
		itemNo, name, category, unit, barcode, spec, deduct string
		safety                                              float64
	}{
		{"SL-001", "EVA 一字拖鞋-黑", "FG", "雙", "4712345000011", "EVA 一體大底／布面一字帶／尺碼 23–28 混碼", "ON_ISSUE", 0},
		{"SL-002", "EVA 一字拖鞋-白", "FG", "雙", "4712345000028", "EVA 一體大底／布面一字帶／尺碼 23–28 混碼", "ON_ISSUE", 0},
		{"UP-001", "一字帶鞋面", "SFG", "雙", "4712345100018", "滌綸布＋泡棉內裡，裁斷後車縫成型", "ON_ARRIVAL", 300},
		{"FB-01", "鞋面布料（滌綸）", "RAW", "碼", "4712345200015", "幅寬 54 吋，黑", "ON_ISSUE", 100},
		{"SO-BK", "EVA 大底-黑", "RAW", "雙", "4712345200022", "EVA 射出大底，23–28 混碼", "ON_ARRIVAL", 500},
		{"SO-WH", "EVA 大底-白", "RAW", "雙", "4712345200039", "EVA 射出大底，23–28 混碼", "ON_ARRIVAL", 500},
		{"TH-01", "車縫線", "AUX", "M", "4712345300014", "聚酯縫線 #40", "ON_ARRIVAL", 2000},
		{"GL-01", "PU 膠水", "AUX", "KG", "4712345300021", "PU 熱熔膠", "ON_ARRIVAL", 10},
		{"GL-02", "水性膠（替代料）", "AUX", "KG", "4712345300038", "水性膠，用量較 PU 膠多 10%", "ON_ARRIVAL", 0},
		{"BX-01", "鞋盒", "AUX", "個", "4712345300045", "瓦楞紙盒 30×12×8 cm", "ON_ISSUE", 500},
		{"TG-01", "吊牌", "AUX", "張", "4712345300052", "品牌吊牌含尺碼貼", "ON_ISSUE", 500},
	}
	for _, p := range products {
		id := httpx.NewID()
		d.product[p.itemNo] = id
		d.must(d.exec(`INSERT INTO products (id, factory_id, item_no, name, barcode, spec, unit, category,
				default_deduct_timing, safety_stock, card_qty)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 100)`,
			id, d.factory, p.itemNo, p.name, p.barcode, p.spec, p.unit, p.category, p.deduct, p.safety))
	}
	step("產品 %d 筆：成品 2、半成品 1、基本材料 3、輔助料件 5", len(products))

	bom := func(parent, child, materialType string, qtyPer, scrap float64, station string) uuid.UUID {
		id := httpx.NewID()
		d.must(d.exec(`INSERT INTO product_bom (id, factory_id, parent_product_id, child_product_id, material_type,
				qty_per, scrap_rate, consume_station_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, d.factory, d.product[parent], d.product[child], materialType, qtyPer, scrap, d.station[station]))
		return id
	}
	bom("UP-001", "FB-01", "MAIN", 0.08, 3, "PREP")
	bom("UP-001", "TH-01", "AUX", 1.5, 0, "SEW")
	for _, fg := range []struct{ item, sole string }{{"SL-001", "SO-BK"}, {"SL-002", "SO-WH"}} {
		bom(fg.item, "UP-001", "MAIN", 1, 0, "FORM")
		bom(fg.item, fg.sole, "MAIN", 1, 0, "FORM")
		glue := bom(fg.item, "GL-01", "AUX", 0.012, 0, "FORM")
		d.must(d.exec(`INSERT INTO bom_substitutes (id, bom_id, substitute_product_id, ratio, priority) VALUES ($1, $2, $3, 1.1, 1)`,
			httpx.NewID(), glue, d.product["GL-02"]))
		bom(fg.item, "BX-01", "AUX", 1, 0, "FIN")
		bom(fg.item, "TG-01", "AUX", 1, 0, "FIN")
	}
	step("BOM：成品 ← 鞋面(半成品) + EVA 大底 + 膠水(可替代水性膠) + 鞋盒 + 吊牌；鞋面 ← 布料 + 車縫線")

	// 標準工時為「標準人秒／雙」= 人數 × 3600 ÷ 時產能 × 約 94%
	routing := func(item string, steps ...struct {
		station string
		std     float64
	}) {
		rid := httpx.NewID()
		d.must(d.exec(`INSERT INTO routings (id, factory_id, product_id, version, is_active, note) VALUES ($1, $2, $3, 1, true, '初版')`,
			rid, d.factory, d.product[item]))
		for i, s := range steps {
			d.must(d.exec(`INSERT INTO routing_steps (id, routing_id, seq, station_id, std_seconds_per_unit) VALUES ($1, $2, $3, $4, $5)`,
				httpx.NewID(), rid, (i+1)*10, d.station[s.station], s.std))
		}
	}
	type st = struct {
		station string
		std     float64
	}
	routing("UP-001", st{"PREP", 90}, st{"SEW", 450})
	routing("SL-001", st{"FORM", 565}, st{"FIN", 170})
	routing("SL-002", st{"FORM", 565}, st{"FIN", 170})
	step("途程：鞋面 = 前期準備 → 針車；拖鞋 = 成型 → 整理")

	for _, dc := range []struct{ station, code, name, disposition string }{
		{"PREP", "P01", "裁斷尺寸不符", "SCRAP"},
		{"SEW", "S01", "跳針", "REWORK"},
		{"SEW", "S02", "車線歪斜", "REWORK"},
		{"FORM", "F01", "溢膠", "REWORK"},
		{"FORM", "F02", "大底貼合不良", "SCRAP"},
		{"FIN", "N01", "鞋面髒污", "REWORK"},
		{"FIN", "N02", "包裝錯誤", "REWORK"},
	} {
		id := httpx.NewID()
		d.must(d.exec(`INSERT INTO defect_codes (id, factory_id, station_id, code, name) VALUES ($1, $2, $3, $4, $5)`,
			id, d.factory, d.station[dc.station], dc.code, dc.name))
		d.defects[dc.station] = append(d.defects[dc.station], defectCode{id, dc.disposition})
	}

	d.whWIP = httpx.NewID()
	d.must(d.exec(`INSERT INTO warehouses (id, factory_id, code, name, type, station_id) VALUES
		($1, $2, 'W01', '原物料倉', 'RAW', NULL),
		($3, $2, 'W02', '成型暫存倉', 'WIP', $4),
		($5, $2, 'W03', '成品倉', 'FG', NULL)`,
		httpx.NewID(), d.factory, d.whWIP, d.station["FORM"], httpx.NewID()))
	step("不良代碼 7 筆；倉庫：原物料倉、成型暫存倉、成品倉")
}

// ───────────────────────── 訂單 → 製令 → 預警 → 發料 ─────────────────────────

func (d *demo) orderFlow() {
	wh, pc := d.users["WH"], d.users["PC"]
	t := d.at(-60) // 07:00 倉管收料

	d.must(pgx.BeginFunc(d.ctx, d.pool, func(tx pgx.Tx) error {
		_, workDate, err := org.Clock(d.ctx, tx, d.factory, t)
		if err != nil {
			return err
		}
		raw, err := inventory.Warehouse(d.ctx, tx, d.factory, "RAW")
		if err != nil {
			return err
		}
		for _, r := range []struct {
			item string
			qty  float64
		}{{"FB-01", 300}, {"SO-BK", 1500}, {"SO-WH", 1000}, {"TH-01", 5000}, {"GL-01", 20}, {"BX-01", 2500}, {"TG-01", 2500}} {
			if _, err := inventory.Post(d.ctx, tx, inventory.Txn{FactoryID: d.factory, Type: "RECEIPT", WarehouseID: raw,
				ProductID: d.product[r.item], Qty: r.qty, At: t, WorkDate: workDate, UserID: wh, Note: "供應商交貨"}); err != nil {
				return err
			}
		}
		_, err = inventory.Post(d.ctx, tx, inventory.Txn{FactoryID: d.factory, Type: "ADJUST", WarehouseID: d.whWIP,
			ProductID: d.product["UP-001"], Qty: 600, At: t, WorkDate: workDate, UserID: wh, Note: "期初：昨日結存鞋面"})
		return err
	}))
	step("07:00 李倉管 收料入原物料倉 7 項；成型暫存倉期初鞋面 600 雙")

	custID := httpx.NewID()
	d.must(d.exec(`INSERT INTO customers (id, factory_id, code, name) VALUES ($1, $2, 'C001', '好走鞋業（量販通路）')`, custID, d.factory))
	due := d.start.AddDate(0, 0, 7).Format(time.DateOnly)
	soID, soNo, err := d.svc.CreateSalesOrder(d.ctx, d.factory, production.SalesOrderInput{
		CustomerID: custID, CustomerPO: "PO-HZ-2611-018", Note: "秋冬檔期補貨",
		Lines: []production.SalesOrderLineInput{
			{ProductID: d.product["SL-001"], Qty: 1200, DueDate: due},
			{ProductID: d.product["SL-002"], Qty: 800, DueDate: due},
		},
	}, pc, d.at(-50))
	d.must(err)
	step("07:10 王生管 建立客戶訂單 %s：SL-001 黑 1,200 雙、SL-002 白 800 雙，交期 %s", soNo, due)

	wos, err := d.svc.ConvertSalesOrder(d.ctx, d.factory, soID, pc, d.at(-48))
	d.must(err)
	for i, wo := range wos {
		res, err := d.svc.Release(d.ctx, d.factory, wo.ID, pc, d.at(-45+i))
		d.must(err)
		step("07:%02d 開立製令 %s，產生流程卡 %d 張，觸發缺料預警 %d 筆", 15+i, wo.WONo, res.Cards, len(res.Alerts))
	}

	rows, err := d.pool.Query(d.ctx, `
		SELECT a.id, p.item_no, a.required_qty, a.available_qty FROM material_alerts a JOIN products p ON p.id = a.product_id
		WHERE a.status = 'PENDING' ORDER BY a.created_at, p.item_no`)
	d.must(err)
	type alert struct {
		ID        uuid.UUID
		Item      string
		Need, Avl float64
	}
	alerts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[alert])
	d.must(err)
	var sfgWOs []uuid.UUID
	for i, a := range alerts {
		if a.Item != "UP-001" {
			step("07:20 預警 %s 需求 %g、可用 %g → 保留待生管處理（登入後會彈窗）", a.Item, a.Need, a.Avl)
			continue
		}
		res, err := d.svc.DecideAlert(d.ctx, d.factory, a.ID, "CREATE_WO", pc, "鞋面不足，開半成品工單", d.at(-40+i))
		var biz *httpx.BizError
		if errors.As(err, &biz) {
			_, err = d.svc.DecideAlert(d.ctx, d.factory, a.ID, "PASS", pc, "前一張半成品製令已涵蓋", d.at(-40+i))
			d.must(err)
			step("07:2%d 預警 UP-001 鞋面（第二張）→ 系統重算：%s → 生管選「Pass」", i, biz.Msg)
			continue
		}
		d.must(err)
		sfgWOs = append(sfgWOs, *res.NewWOID)
		step("07:2%d 預警 UP-001 鞋面 需求 %g → 生管選「通知建單」→ 依當下缺口 %g 建立半成品製令 %s", i, a.Need, res.ShortQty, res.NewWONo)
	}
	for i, id := range sfgWOs {
		res, err := d.svc.Release(d.ctx, d.factory, id, pc, d.at(-35+i))
		d.must(err)
		step("07:2%d 開立半成品製令 %s，流程卡 %d 張", 5+i, res.WONo, res.Cards)
	}

	all := append([]uuid.UUID{wos[0].ID, wos[1].ID}, sfgWOs...)
	for _, id := range all {
		_, err := d.svc.IssueMaterials(d.ctx, d.factory, id, wh, d.at(-25))
		d.must(err)
	}
	step("07:35 李倉管 發料（出庫扣帳）：布料、鞋盒、吊牌")

	// 上工派線
	assign := map[string]uuid.UUID{
		"PREP-A": sfgWOs[0], "PREP-B": sfgWOs[0],
		"SEW-A": sfgWOs[0], "SEW-B": sfgWOs[0], "SEW-C": sfgWOs[0], "SEW-D": sfgWOs[0],
		"FORM-A": wos[0].ID, "FORM-B": wos[0].ID, "FORM-C": wos[1].ID, "FORM-D": wos[1].ID,
		"FIN-A": wos[0].ID, "FIN-B": wos[1].ID,
	}
	startAfter := map[string]int{"PREP": 0, "SEW": 30, "FORM": 0, "FIN": 45}
	for _, l := range d.lines {
		l.wo = assign[l.code]
		l.onAt = d.at(startAfter[l.station])
		l.nextBatch = 20
	}
	// 個別線況：SEW-D 新手多效率低；FORM-C 10:20–11:05 設備異常停機
	for _, l := range d.lines {
		switch l.code {
		case "SEW-D":
			l.efficiency = 0.74
		case "FORM-C":
			l.downFrom, l.downTo = d.at(140), d.at(185)
		case "FIN-B":
			l.efficiency = 0.85
		}
	}
	step("08:00 陳組長 依站別上工：前期準備、成型 08:00；針車 08:30；整理 08:45（等上站有料）")
}

// ───────────────────────── 模擬現場回報 ─────────────────────────

type simLine struct {
	id, stationID    uuid.UUID
	code, station    string
	capacity         float64
	headcount        int
	efficiency       float64
	wo               uuid.UUID
	card             string
	acc, nextBatch   float64
	downFrom, downTo time.Time
	onAt             time.Time // 預定上工時間
	working          bool
	finished         bool // 已無工作而下工
}

var defectRate = map[string]float64{"PREP": 0.008, "SEW": 0.03, "FORM": 0.025, "FIN": 0.012}

// simulate 模擬早班（最多 8 小時）的現場作業；班別結束時全部下工，之後的班別保持空白。
func (d *demo) simulate(now time.Time) {
	end := now
	if shiftEnd := d.start.Add(8 * time.Hour); end.After(shiftEnd) {
		end = shiftEnd
	}
	held := map[string]*simLine{}
	scans := 0
	tick := 5 * time.Minute
	for t := d.start; t.Add(tick).Before(end); t = t.Add(tick) {
		for _, l := range d.lines {
			if l.finished || t.Before(l.onAt) {
				continue
			}
			if !l.working {
				_, err := d.svc.StartLabor(d.ctx, d.factory, l.id, l.wo, l.headcount, d.users["LEADER"], t)
				d.must(err)
				l.working = true
			}
			if !t.Before(l.downFrom) && t.Before(l.downTo) {
				continue
			}
			l.acc = math.Min(l.acc+l.capacity/12*l.efficiency*(0.85+d.rng.Float64()*0.25), l.nextBatch*2)
			if l.acc < l.nextBatch {
				continue
			}
			card, remaining := d.pickCard(l, held, t)
			if card == "" {
				continue
			}
			qty := math.Min(math.Floor(l.acc), remaining)
			if l.station == "FORM" { // 成型需要暫存倉有鞋面
				var stock float64
				d.must(d.pool.QueryRow(d.ctx, `SELECT COALESCE(sum(qty), 0) FROM inventory_balances WHERE warehouse_id = $1 AND product_id = $2`,
					d.whWIP, d.product["UP-001"]).Scan(&stock))
				qty = math.Min(qty, math.Floor(stock))
			}
			if qty <= 0 {
				continue
			}
			rate := defectRate[l.station]
			if l.code == "SEW-D" {
				rate *= 1.8
			}
			bad := 0.0
			for i := 0; i < int(qty); i++ {
				if d.rng.Float64() < rate {
					bad++
				}
			}
			var defects []production.DefectInput
			if bad > 0 {
				codes := d.defects[l.station]
				dc := codes[d.rng.IntN(len(codes))]
				defects = append(defects, production.DefectInput{DefectCodeID: dc.id, Qty: bad, Disposition: dc.disposition})
			}
			at := t.Add(time.Duration(d.rng.IntN(280)) * time.Second)
			_, err := d.svc.Scan(d.ctx, production.ScanInput{
				FactoryID: d.factory, CardNo: card, LineID: l.id, GoodQty: qty - bad, Defects: defects,
				UserID: d.users["LEADER"], DeviceID: l.code + "-SCANNER", At: at,
			})
			if err != nil {
				log.Printf("scan %s %s: %v", l.code, card, err)
				continue
			}
			scans++
			l.acc -= qty
			l.nextBatch = 15 + d.rng.Float64()*20
		}
	}
	if end.Before(now) {
		for _, l := range d.lines {
			if l.working && !l.finished {
				d.must(d.svc.EndLabor(d.ctx, d.factory, l.id, d.users["LEADER"], end))
			}
		}
	}
	step("%s 08:00–%s 模擬 12 條線每 5 分鐘作業，共 %d 筆掃描回報", d.start.Format("01/02"), end.Format("15:04"), scans)
}

// pickCard 取得線別要做的流程卡；本製令在本站已做完且上游也沒有在途量時，換到下一張有量的製令。
func (d *demo) pickCard(l *simLine, held map[string]*simLine, t time.Time) (string, float64) {
	query := func(wo uuid.UUID) (string, float64) {
		rows, err := d.pool.Query(d.ctx, `
			SELECT c.card_no, fp.remaining_qty FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
			WHERE c.work_order_id = $1 AND fp.station_id = $2 AND fp.remaining_qty > 0 ORDER BY c.card_no`, wo, l.stationID)
		d.must(err)
		defer rows.Close()
		for rows.Next() {
			var no string
			var rem float64
			d.must(rows.Scan(&no, &rem))
			if owner := held[no]; owner == nil || owner == l {
				return no, rem
			}
		}
		return "", 0
	}
	if l.card != "" {
		if no, rem := query(l.wo); no == l.card {
			return no, rem
		}
		delete(held, l.card)
		l.card = ""
	}
	if no, rem := query(l.wo); no != "" {
		held[no], l.card = l, no
		return no, rem
	}

	var upstream bool
	d.must(d.pool.QueryRow(d.ctx, `
		SELECT EXISTS (SELECT 1 FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
		WHERE c.work_order_id = $1 AND fp.remaining_qty > 0
		  AND fp.step_seq < (SELECT step_seq FROM flow_card_progress WHERE card_id = c.id AND station_id = $2))`,
		l.wo, l.stationID).Scan(&upstream))
	if upstream {
		return "", 0 // 等上站
	}
	var next uuid.UUID
	err := d.pool.QueryRow(d.ctx, `
		SELECT w.id FROM work_orders w JOIN flow_cards c ON c.work_order_id = w.id
		JOIN flow_card_progress fp ON fp.card_id = c.id
		WHERE w.factory_id = $1 AND w.status IN ('RELEASED','IN_PROGRESS') AND fp.station_id = $2 AND w.id <> $3
		GROUP BY w.id, w.wo_no HAVING sum(fp.remaining_qty) > 0 ORDER BY w.wo_no LIMIT 1`,
		d.factory, l.stationID, l.wo).Scan(&next)
	if err == pgx.ErrNoRows {
		// 沒有任何製令需要本站：下工，停止累計目標與工時
		d.must(d.svc.EndLabor(d.ctx, d.factory, l.id, d.users["LEADER"], t))
		l.finished = true
		return "", 0
	}
	d.must(err)
	_, err = d.svc.StartLabor(d.ctx, d.factory, l.id, next, l.headcount, d.users["LEADER"], t)
	d.must(err)
	l.wo = next
	if no, rem := query(next); no != "" {
		held[no], l.card = l, no
		return no, rem
	}
	return "", 0
}

func (d *demo) summary(defaultPasswords bool) {
	fmt.Println("\n── 製令現況 ──")
	rows, err := d.pool.Query(d.ctx, `
		SELECT w.wo_no, p.item_no, p.name, w.status, w.planned_qty,
		       COALESCE((SELECT sum(fp.good_qty) FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
		                 WHERE c.work_order_id = w.id AND fp.step_seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = w.routing_id)), 0)
		FROM work_orders w JOIN products p ON p.id = w.product_id ORDER BY w.wo_no`)
	d.must(err)
	for rows.Next() {
		var no, item, name, status string
		var planned, done float64
		d.must(rows.Scan(&no, &item, &name, &status, &planned, &done))
		fmt.Printf("  %-22s %-7s %-14s %-12s %6.0f / %6.0f\n", no, item, name, status, done, planned)
	}
	d.must(rows.Err())

	fmt.Println("\n── 登入帳號 ──")
	fmt.Println("  admin@example.com  最高權限（總部管理員）")
	fmt.Println("  pc@demo.local      生管")
	fmt.Println("  wh@demo.local      倉庫管理")
	fmt.Println("  leader@demo.local  現場組長")
	if defaultPasswords {
		fmt.Println("  密碼：admin1234（最高權限）、Demo1234!（其餘）— 僅供本機開發")
	} else {
		fmt.Println("  密碼：依 .env 的 ADMIN_PASSWORD / DEMO_PASSWORD；個別帳號可用 go run ./cmd/setpass 重設")
	}
}
