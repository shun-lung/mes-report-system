# 報工系統 架構定稿 v3

## 技術組合

| 層 | 選用 |
|---|---|
| 前端 | Vue 3 + Vite + TypeScript + Pinia + Element Plus（後台 `/admin`、現場掃描 `/station` 全螢幕） |
| 後端 | Go 1.27、Gin、pgx/v5（手寫 SQL，類似 Dapper；報表直接寫 SQL） |
| 資料庫 | PostgreSQL 17，goose 管理 migration |
| 背景工作 | River（Postgres 為佇列）：小時匯總、庫存預警 |
| 即時看板 | WebSocket（coder/websocket） |
| PDF | maroto v2（流程卡、製令單，內含 Code128 條碼） |
| 驗證 | golang-jwt/v5，bcrypt |
| 部署 | Docker Compose，各廠一套；總部彙整庫另建 |

## 目錄結構

```
報工系統/
├─ backend/
│  ├─ cmd/api/main.go
│  ├─ internal/
│  │  ├─ config/         設定
│  │  ├─ database/       連線、啟動時自動 migrate
│  │  ├─ httpx/          路由、middleware（JWT、權限、工廠範圍）
│  │  ├─ auth/           登入、使用者、角色、權限
│  │  ├─ org/            工廠、班別、生產日計算
│  │  ├─ masterdata/     站點、線別、產品、BOM、途程、不良代碼、圖片
│  │  ├─ production/     製令單、流程卡、掃描回報、線別工時
│  │  ├─ inventory/      倉庫、入出庫、流水帳
│  │  ├─ alert/          用料預警、通知
│  │  ├─ report/         統計報表
│  │  └─ pdf/            流程卡 / 製令單 PDF
│  └─ migrations/        goose migration（go:embed 打包進執行檔）
├─ frontend/
│  └─ src/{api,router,stores,views/admin,views/station,components}
├─ deploy/docker-compose.yml
└─ docs/
```

## 關鍵規則

1. **生產日**：`work_date` 依工廠時區 + 班別時段決定；跨午夜夜班歸屬開班當日。
2. **流程卡**：製令單開立時依 `card_qty`（預設 100 雙）產生；尾數卡數量為餘數。部分回報 / 不良可拆卡，子卡記 `parent_card_id`。
3. **站點管制**：第一站 `input_qty` = 卡數量；本站良品寫入下一站 `input_qty`；不可跳站、不可超報。
4. **掃描交易**（單一 DB 交易）：寫 `production_scans` → 更新 `flow_card_progress` → 依 BOM 到站扣帳 → 暫存倉入出庫流水 → 末站良品 `FG_IN`。
5. **線別工時**：上工掃描 = 線別 + 製令單 + 人數；同線再上工另一張工單時自動結束前一筆。
6. **用料預警**：以工單需求判斷，不依賴安全庫存設定。可用量（庫存 ＋ 在途半成品製令 − 其他已開立工單未扣料量）< 本工單未扣料需求 × (1 + `alert_pct`%，預設 3%) 時建立 `material_alerts` 並彈窗通知生管 → 「通知建單」產生 DRAFT 半成品工單（`source=SHORTAGE`、`parent_wo_id`）或「PASS」。
7. **權限**：權限碼 `模組:動作`；使用者依工廠指派角色，基本資料由各廠具權限者自行維護。
8. **統計**：`output_hourly` 在掃描交易內即時 upsert（看板直接讀取，無延遲）；明細查 `production_scans`；跨廠以 `work_date` 加總。
9. **需求來源**：客戶訂單（`sales_orders`）每筆明細轉一張製令；製令完工後回推訂單狀態。
10. **生產 8HR 看板**（首頁，`GET /api/dashboard/shift`）：以班別為時間窗，逐小時比較各站／各線良品與目標。
    目標 = 上工時間 × 線別時產能；人時效率 = Σ(良品 × 標準人秒) ÷ 投入人時；30 分鐘無掃描視為停滯。

## 本機開發

```bash
docker compose -f deploy/docker-compose.yml up -d   # Postgres :5433
cd backend && go run ./cmd/demo                      # （選用）清空並重建拖鞋示範資料，模擬今日早班
cd backend && go run ./cmd/api                       # API 127.0.0.1:8080
cd frontend && npm install && npm run dev            # 前端 http://127.0.0.1:5173
```

示範帳號：`admin@example.com / admin1234`（最高權限）、`pc@demo.local`（生管）、`wh@demo.local`（倉管）、`leader@demo.local`（現場組長），後三者密碼 `Demo1234!`。

API 慣例：`Authorization: Bearer <token>`；工廠範圍的 API 需帶 `X-Factory-ID`。

## 分期

- P1：權限、工廠/班別、站點/線別、產品/BOM/替代料/圖片、途程、不良代碼
- P2：製令單、流程卡 PDF、現場掃描、線別工時、不良記錄、產量報表
- P3：倉庫入出庫、流水帳、用料預警
- P4：生產排程
