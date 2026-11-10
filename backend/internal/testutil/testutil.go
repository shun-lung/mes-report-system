// Package testutil 整合測試共用：連到專用測試資料庫、每個測試重建最小基本資料。
//
// 用法：設定 TEST_DATABASE_URL（例如本機 Docker 的 mes_test 資料庫）後執行 go test ./...；
// 沒設定時整合測試自動略過，單元測試照常執行。
package testutil

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mes/internal/database"
	"mes/internal/httpx"
	"mes/internal/inventory"
)

// Fixture 最小工廠：成型 → 整理 兩站，一個成品 T-FG。
//
//	T-FG 用料：大底 T-SOLE（到站扣帳，成型站用 1 雙）、鞋盒 T-BOX（倉庫發料，整理站用 1 個）
//	線別：成型 A、成型 B、整理 A；倉庫：原物料、成型暫存、成品、完工暫存
//	期初庫存：大底 1000、鞋盒 1000
type Fixture struct {
	Pool     *pgxpool.Pool
	Factory  uuid.UUID
	User     uuid.UUID
	Customer uuid.UUID

	StationForm, StationFin   uuid.UUID
	LineFormA, LineFormB      uuid.UUID
	LineFinA                  uuid.UUID
	Product, Sole, Box        uuid.UUID
	WhRaw, WhWIP, WhFG, WhStg uuid.UUID
	DefectForm                uuid.UUID

	// Now 固定在早班時段內（台北 10:00），測試結果不受執行時間影響
	Now time.Time
}

var (
	once    sync.Once
	pool    *pgxpool.Pool
	initErr error
)

// Setup 連線、套用 migration、清空資料並建立 Fixture；沒有 TEST_DATABASE_URL 時略過測試。
func Setup(t *testing.T) *Fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("未設定 TEST_DATABASE_URL，略過整合測試")
	}
	if !strings.Contains(url, "test") {
		t.Fatal("TEST_DATABASE_URL 的資料庫名稱必須含 test，避免誤清正式資料")
	}
	ctx := context.Background()
	once.Do(func() {
		if initErr = database.Migrate(url); initErr != nil {
			return
		}
		pool, initErr = database.Connect(ctx, url)
	})
	if initErr != nil {
		t.Fatalf("測試資料庫連線失敗：%v", initErr)
	}
	if err := truncateAll(ctx); err != nil {
		t.Fatalf("清空資料失敗：%v", err)
	}
	f, err := seed(ctx)
	if err != nil {
		t.Fatalf("建立測試資料失敗：%v", err)
	}
	return f
}

func truncateAll(ctx context.Context) error {
	rows, err := pool.Query(ctx, `
		SELECT quote_ident(tablename) FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
	if err != nil {
		return err
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" RESTART IDENTITY CASCADE")
	return err
}

func seed(ctx context.Context) (*Fixture, error) {
	taipei, _ := time.LoadLocation("Asia/Taipei")
	f := &Fixture{Pool: pool, Now: time.Date(2026, 11, 10, 10, 0, 0, 0, taipei)}
	id := func(p *uuid.UUID) uuid.UUID { *p = httpx.NewID(); return *p }

	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var firstErr error
		exec := func(sql string, args ...any) {
			if firstErr != nil {
				return
			}
			_, firstErr = tx.Exec(ctx, sql, args...)
		}
		exec(`INSERT INTO factories (id, code, name, timezone) VALUES ($1, 'T1', '測試廠', 'Asia/Taipei')`, id(&f.Factory))
		for i, s := range [][3]string{{"D", "08:00", "16:00"}, {"M", "16:00", "00:00"}, {"N", "00:00", "08:00"}} {
			offset := 0
			if s[0] == "N" {
				offset = -1
			}
			exec(`INSERT INTO shifts (id, factory_id, code, name, start_time, end_time, work_date_offset, sort_order)
				VALUES ($1, $2, $3, $3, $4::time, $5::time, $6, $7)`, httpx.NewID(), f.Factory, s[0], s[1], s[2], offset, i)
		}
		exec(`INSERT INTO users (id, email, name, password_hash) VALUES ($1, 'tester@test.local', '測試員', 'x')`, id(&f.User))
		exec(`INSERT INTO customers (id, factory_id, code, name) VALUES ($1, $2, 'C1', '測試客戶')`, id(&f.Customer), f.Factory)

		exec(`INSERT INTO work_stations (id, factory_id, code, name, sort_order) VALUES ($1, $2, 'FORM', '成型', 10), ($3, $2, 'FIN', '整理', 20)`,
			id(&f.StationForm), f.Factory, id(&f.StationFin))
		exec(`INSERT INTO production_lines (id, factory_id, station_id, code, name, capacity_per_hour, std_headcount) VALUES
			($1, $2, $3, 'FORM-A', '成型 A 線', 60, 10), ($4, $2, $3, 'FORM-B', '成型 B 線', 60, 10), ($5, $2, $6, 'FIN-A', '整理 A 線', 120, 6)`,
			id(&f.LineFormA), f.Factory, f.StationForm, id(&f.LineFormB), id(&f.LineFinA), f.StationFin)
		exec(`INSERT INTO defect_codes (id, factory_id, station_id, code, name) VALUES ($1, $2, $3, 'D01', '脫膠')`,
			id(&f.DefectForm), f.Factory, f.StationForm)

		exec(`INSERT INTO products (id, factory_id, item_no, name, unit, category, default_deduct_timing, card_qty) VALUES
			($1, $2, 'T-FG', '測試拖鞋', '雙', 'FG', 'ON_ISSUE', 100),
			($3, $2, 'T-SOLE', '測試大底', '雙', 'RAW', 'ON_ARRIVAL', 100),
			($4, $2, 'T-BOX', '測試鞋盒', '個', 'AUX', 'ON_ISSUE', 100)`,
			id(&f.Product), f.Factory, id(&f.Sole), id(&f.Box))
		exec(`INSERT INTO product_bom (id, factory_id, parent_product_id, child_product_id, material_type, qty_per, consume_station_id) VALUES
			($1, $2, $3, $4, 'MAIN', 1, $5), ($6, $2, $3, $7, 'AUX', 1, $8)`,
			httpx.NewID(), f.Factory, f.Product, f.Sole, f.StationForm, httpx.NewID(), f.Box, f.StationFin)
		routing := httpx.NewID()
		exec(`INSERT INTO routings (id, factory_id, product_id, version, is_active) VALUES ($1, $2, $3, 1, true)`, routing, f.Factory, f.Product)
		exec(`INSERT INTO routing_steps (id, routing_id, seq, station_id, std_seconds_per_unit) VALUES ($1, $2, 10, $3, 600), ($4, $2, 20, $5, 180)`,
			httpx.NewID(), routing, f.StationForm, httpx.NewID(), f.StationFin)

		exec(`INSERT INTO warehouses (id, factory_id, code, name, type, station_id) VALUES
			($1, $2, 'W01', '原物料倉', 'RAW', NULL), ($3, $2, 'W02', '成型暫存倉', 'WIP', $4),
			($5, $2, 'W03', '成品倉', 'FG', NULL), ($6, $2, 'W04', '完工暫存倉', 'STAGE', NULL)`,
			id(&f.WhRaw), f.Factory, id(&f.WhWIP), f.StationForm, id(&f.WhFG), id(&f.WhStg))

		if firstErr != nil {
			return firstErr
		}
		for _, p := range []uuid.UUID{f.Sole, f.Box} {
			if _, err := inventory.Post(ctx, tx, inventory.Txn{
				FactoryID: f.Factory, Type: "RECEIPT", WarehouseID: f.WhRaw, ProductID: p, Qty: 1000,
				RefType: "receipt", At: f.Now, WorkDate: f.Now, UserID: f.User, Note: "期初",
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return f, err
}

// Stock 某倉某料件目前庫存。
func (f *Fixture) Stock(t *testing.T, warehouseID, productID uuid.UUID) float64 {
	t.Helper()
	var q float64
	err := f.Pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT qty FROM inventory_balances WHERE warehouse_id = $1 AND product_id = $2), 0)::float8`,
		warehouseID, productID).Scan(&q)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// QueryFloat 執行回傳單一數字的 SQL（測試檢查用）。
func (f *Fixture) QueryFloat(t *testing.T, sql string, args ...any) float64 {
	t.Helper()
	var q float64
	if err := f.Pool.QueryRow(context.Background(), sql, args...).Scan(&q); err != nil {
		t.Fatal(err)
	}
	return q
}

// QueryString 執行回傳單一字串的 SQL（測試檢查用）。
func (f *Fixture) QueryString(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := f.Pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
