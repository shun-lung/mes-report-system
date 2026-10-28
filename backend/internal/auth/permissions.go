package auth

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"mes/internal/httpx"
)

type Permission struct {
	Code   string `json:"code"`
	Module string `json:"module"`
	Name   string `json:"name"`
}

// Catalog 為系統所有權限碼，啟動時同步到 permissions 表。
var Catalog = []Permission{
	{"user:view", "system", "檢視使用者"},
	{"user:edit", "system", "維護使用者與指派角色"},
	{"role:view", "system", "檢視角色"},
	{"role:edit", "system", "維護角色與權限"},
	{"factory:edit", "org", "維護工廠設定與班別"},
	{"station:view", "masterdata", "檢視站點與線別"},
	{"station:edit", "masterdata", "維護站點與線別"},
	{"product:view", "masterdata", "檢視產品"},
	{"product:edit", "masterdata", "維護產品與圖片"},
	{"bom:view", "masterdata", "檢視 BOM"},
	{"bom:edit", "masterdata", "維護 BOM 與替代料"},
	{"routing:view", "masterdata", "檢視途程"},
	{"routing:edit", "masterdata", "維護途程"},
	{"defect:edit", "masterdata", "維護不良代碼"},
	{"masterdata:import", "masterdata", "Excel 匯入基本資料"},
	{"wo:view", "production", "檢視製令單"},
	{"wo:edit", "production", "維護製令單"},
	{"wo:release", "production", "開立製令單與列印流程卡"},
	{"scan:report", "production", "現場掃描回報"},
	{"labor:report", "production", "線別上下工"},
	{"inventory:view", "inventory", "檢視庫存與歷程"},
	{"inventory:edit", "inventory", "入出庫作業"},
	{"inventory:adjust", "inventory", "庫存調整與沖銷"},
	{"alert:decide", "inventory", "處理缺料預警"},
	{"report:view", "report", "檢視報表"},
}

// Seed 同步權限碼；資料庫沒有任何使用者時建立總部管理員。
func Seed(ctx context.Context, pool *pgxpool.Pool, adminEmail, adminPassword string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for i, p := range Catalog {
		_, err := tx.Exec(ctx, `
			INSERT INTO permissions (code, module, name, sort_order) VALUES ($1, $2, $3, $4)
			ON CONFLICT (code) DO UPDATE SET module = EXCLUDED.module, name = EXCLUDED.name, sort_order = EXCLUDED.sort_order`,
			p.Code, p.Module, p.Name, i)
		if err != nil {
			return err
		}
	}

	var hasUser bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&hasUser); err != nil {
		return err
	}
	if !hasUser {
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, email, name, password_hash, is_super_admin) VALUES ($1, $2, '系統管理員', $3, true)`,
			httpx.NewID(), adminEmail, string(hash))
		if err != nil {
			return err
		}
		log.Printf("created super admin %s", adminEmail)
	}
	return tx.Commit(ctx)
}

func allPermissions() map[string]bool {
	m := make(map[string]bool, len(Catalog))
	for _, p := range Catalog {
		m[p.Code] = true
	}
	return m
}
