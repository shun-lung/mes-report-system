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

11. **基本資料**（`internal/masterdata`）：
    - 途程有版本，建立後不可修改；要調整就建新版本，可從現行版本複製。製令鎖定開立當時的版本。同一站點在一個途程中只能出現一次（掃描以線別所屬站點找站序）。
    - BOM 可直接修改，只影響之後新開立的製令（製令開立時複製用料）；加入料件前檢查不可形成循環。
    - 產品圖片存在資料庫（`product_images.data`，每張 ≤ 2 MB，依內容判斷格式），`GET /api/images/:id` 免登入供 `<img>` 載入；雲端主機重新部署不會遺失，dbsync 也會一併搬移。
    - 看板的完工站 = 生效中成品途程的最後一站，不依站點排序。
12. **現場掃描畫面**（`/station`，需 `scan:report`）：每台裝置設定一次所屬線別（存在瀏覽器）；掃描槍輸入流程卡條碼 + Enter 帶出本站可報量，預設整卡良品；點不良代碼會從良品移一雙到不良（可選重工／報廢）；空白 Enter 即確認回報。線別未上工或掃到不同製令時提示上工／換製令（自動結束前一筆工時）。API：`GET /station/state`、`GET /cards/:cardNo`。
13. **流程卡列印**（`/print/cards/:woId`）：瀏覽器列印 A4 直式、每頁 8 張、Code128 條碼（內容 = 流程卡號）；列印視窗選「另存為 PDF」即得 PDF。由瀏覽器排版可直接使用中文字型，不需在後端嵌入字型。確認已列印後記錄 `print_count`，補印會標示「補印」。

14. **倉儲作業**（`/warehouse`，`internal/inventory/ops.go`）：收料入庫、製令發料（同步累加製令用料已發數量）、一般發料（需填用途）、倉庫移轉、盤點調整（輸入實盤，依差異產生調整，無差異不產生紀錄）、倉庫設定。可用掃描槍掃料件條碼加入明細。
    - 手動出庫與移轉**不可超過現有庫存**；現場掃描的自動扣帳仍允許負庫存（產線不因帳務停線）。
    - **沖銷**（「庫存與歷程」頁，需 `inventory:adjust`）：只限收料、發料、移轉、盤點調整；產生反向 `REVERSAL` 異動並以 `reversal_of` 指回原異動，不可重複沖銷；移轉的出入兩筆一起沖；製令發料沖銷時扣回已發數量；沖銷入庫時若庫存已被用掉則拒絕。生產自動產生的扣帳與完工入庫不可在此沖銷（待「掃描撤銷」處理）。
    - 製令發料只針對「出庫扣帳」料件；「到站扣帳」料件由現場掃描自動扣帳，手動發料會被拒絕（避免重複扣）。製令頁「發料」遇庫存不足時依現有量發料並列出仍缺的數量，不會發成負數。
    - 同一次作業的多筆異動共用 `ref_id`，整筆在單一交易內完成。

15. **訂單與製令**（「訂單與製令」頁三分頁）：客戶維護；客戶訂單（明細只能是成品）→ 轉製令（草稿）→ 可立即開立（觸發缺料預警時頂端自動彈窗）；手動新增製令（成品或半成品，須有生效途程）；草稿可改數量與交期，用料需求等比例重算。
    - 取消製令：限草稿或已開立、且無掃描／上工／庫存異動；流程卡作廢、待處理預警結案、訂單狀態重算，該訂單明細可重新轉製令。
    - 修改訂單：無未取消的製令時才可改（舊明細上已取消製令的關聯會解除）；取消訂單需先取消所有製令。
    - 訂單狀態：無有效製令＝未生產；有製令開始生產＝生產中；有效製令全部完工＝已完成。

16. **掃描撤銷**（`ReverseScan`）：單一交易內還原一次回報的所有影響——本站良品／不良與重工回流、下一站流入、到站扣帳（反向沖銷並扣回製令已發）、末站入庫、小時統計（在原時段沖回）、不良統計，並重算卡片／製令／訂單狀態（淨回報為 0 時卡片退回未開始、製令退回已開立）。
    - 撤銷紀錄為負數的 `production_scans`，`reversal_of` 指回原紀錄，沿用原 `scanned_at`／生產日／班別；實際撤銷時間在 `created_at`、原因（必填）在 `note`。
    - 下一站已回報這些量時拒絕，須由後往前逐站撤銷；同一筆不可重複撤銷，撤銷紀錄不可再撤銷。
    - 權限：現場組長（`scan:report`）只能撤銷本班；生管（`wo:edit`）可撤銷任何時段。介面：掃描畫面「最近回報」、「回報紀錄」頁（`GET /scans` 依生產日／班別／線別／卡號查詢）。

17. **拆卡／合併**（`SplitCard`／`MergeCard`）：從流程卡某站的待做量分出子卡（卡號加 `-S1`、`-S2`…），子卡沿用原卡途程、從該站開始；原卡數量相應減少，進度紀錄保留。子卡尚無回報時可合併回原卡（子卡作廢）。原因必填，記在 `flow_cards.note`。待做量只是換卡承載，製令總量、庫存、統計不變。
    - 介面：掃描畫面「拆卡」（在本站拆，拆完開啟列印頁只印原卡與新卡）；製令單「流程卡」視窗（各卡各站進度、拆卡、合併、勾選列印）。列印頁支援 `?cards=a,b` 只印指定卡，子卡標示「拆自」。
    - 權限：現場組長或生管（`scan:report` 或 `wo:edit`）。撤銷上一站回報時若下一站待做量已被拆走，需先合併子卡。

18. **Excel 匯入基本資料**（`internal/importer`，權限 `masterdata:import`）：產品、站點與線別、不良代碼、BOM（含替代料）、途程。流程：下載範本（含範例與「說明」工作表）→ 上傳預覽（逐列標示新增／更新／無變更／錯誤，不寫入）→ 確認匯入（整份檔案單一交易寫入，仍有錯誤則拒絕；同工廠同類型以 advisory lock 排隊；寫入 `audit_logs`）。
    - 依代碼比對，已存在更新、不存在新增，不刪除既有資料；更新時空白選填欄位沿用原值。只接受 .xlsx，最多 2,000 列、5 MB。
    - 套用與畫面相同的檢查：條碼唯一、已有 BOM／途程者不可改為材料、BOM 不可循環、途程同站不可重複。途程與生效版本不同時建立新版本並生效。
    - 角色「會員」：原有業務功能＋Excel 匯入，不含使用者與角色維護（`user:edit`、`role:edit`）。示範帳號 `member@demo.local`。

19. **採購與缺料自動請購**（`internal/purchasing`，權限 `purchase:view`／`purchase:edit`）：供應商、採購單（草稿 → 下單 → 部分到貨／全部到貨，或取消）、採購收料（`inventory:edit`，入庫並累計已到；沖銷收料會扣回已到並重算狀態）。產品可設定預設供應商（Excel 匯入亦可）。
    - 開立製令時，**外購料件**（無生效途程）缺料會自動加入該料件預設供應商的「缺料自動」採購單草稿（同供應商合併、同料件累加），預警記為「已請購」；**自製件**維持待處理預警由生管決定開製令或 Pass。待處理的材料預警可選「建立採購單」。
    - 可用量計入在途採購（含草稿）避免重複請購；採購單取消時，相關預警回到待處理。
    - 預警緩衝套用在全部未扣料需求上，開立檢查與即時缺料清單的缺口一致。
    - **即時缺料清單**（「缺料預警」頁 → 目前缺料，`GET /shortages`）：彙總已開立製令未扣需求，對照庫存與在途，分「缺料／在途可補足／庫存足夠」；可勾選材料「產生採購單」或對半成品「開半成品製令」。
20. **生管管制與掃描對話框**（migration `00008`）：
    - 每張製令有兩個管制旗標，預設都不管制：**人員授權**（`require_auth`，只有被授權該線別的人能回報；最高權限不受限）、**強制上工**（`require_labor`，該線須先上工此製令才能回報，掃描時不提供「先不上工」）。檢查在後端 `checkControl`，前端只是提示。
    - 設定時機：「訂單與製令 → 客戶訂單」勾選多張訂單 →「轉製令（N）」對話框選管制並可勾「立即開立」（`POST /sales-orders/convert`，整批單一交易）；手動新增製令與草稿編輯也可設定；缺料開的半成品製令沿用上層製令的旗標。
    - 授權線別：「權限管理」使用者列「授權線別」→ 依站點勾選線別（`GET/PUT /users/:id/lines`，`user:edit`）。有設定授權的人，掃描畫面只列出授權線別；沒設定者列出全部（但遇人員授權製令會被擋）。
    - 現場掃描改為頂部按鈕開啟的對話框（可切換全螢幕，平板會進入瀏覽器全螢幕）；`/station` 頁仍保留給專用掃描機。
21. **角色與使用者維護**（「權限管理」頁，`internal/auth/manage.go`）：
    - 角色：新增／編輯／複製／刪除（`POST/PUT/DELETE /roles`，`role:edit`），依模組勾選權限；刪除前須沒有使用者。最高權限（`is_system`）固定具備全部權限，不能修改；`factory_id` 為 NULL 的共用範本也不能在工廠內修改。
    - 使用者：新增帳號（Email、姓名、工號、密碼至少 8 碼、角色）、編輯姓名／工號／角色、重設密碼、停用（`POST/PUT /users`，`user:edit`）。總部管理員帳號不能在此修改。
    - 防越權：非總部管理員只能授予自己具備的權限、只能指派權限不超過自己的角色；不能改自己的角色、不能停用自己、不能移除自己唯一的 `role:edit`。
    - 權限每次請求從資料庫載入，修改後立即生效（前端選單在重新載入後更新）。所有變更寫入 `audit_logs`。

## 本機開發

```bash
docker compose -f deploy/docker-compose.yml up -d   # Postgres :5433
cd backend && go run ./cmd/demo                      # （選用）清空並重建拖鞋示範資料，模擬今日早班
cd backend && go run ./cmd/api                       # API 127.0.0.1:8080
cd frontend && npm install && npm run dev            # 前端 http://127.0.0.1:5173
```

對外測試（Cloudflare 快速通道，免費、免網域，網址每次啟動會變）：

```bash
./scripts/start-public.sh            # 啟動資料庫 + API（含網頁）+ 通道，印出 https://xxxx.trycloudflare.com
./scripts/start-public.sh --build    # 改過前端後，先重新建置
```

機密放在 `backend/.env`（JWT_SECRET、ADMIN_PASSWORD、DEMO_PASSWORD、STATIC_DIR），已列入 .gitignore。

示範帳號：`admin@example.com`（最高權限）、`pc@demo.local`（生管）、`wh@demo.local`（倉管）、`leader@demo.local`（現場組長）、`member@demo.local`（會員）。未設 `.env` 時密碼為 `admin1234` / `Demo1234!`（僅限本機開發）；已設 `.env` 時見 `帳號密碼.local.txt`，可用 `go run ./cmd/setpass` 重設。

## 雲端 Demo 與切換

Supabase 就是雲端代管的 PostgreSQL，沿用同一份 migration 與同一支 Go 程式；資料庫沒有被換掉，只是換了放的位置。

```
本地（正式上線演練）              雲端 Demo（Mac 可關機）
Mac: Docker PostgreSQL           Supabase: PostgreSQL（Session pooler）
     Go API（含網頁）       ⇄      Render: Go API（含網頁，Dockerfile）
              └──── cmd/dbsync：整份結構＋資料雙向複製、核對 ────┘
```

**切換程式連哪個資料庫**：`ENV_FILE` 指定設定檔（預設 `.env`）。

```bash
cd backend
go run ./cmd/api                               # 本地資料庫（.env）
ENV_FILE=.env.supabase go run ./cmd/api        # 雲端資料庫
ENV_FILE=.env.supabase go run ./cmd/demo       # 在雲端重建示範資料（刷新「今天」）
```

**搬移資料**：`cmd/dbsync`，一次一個方向整份複製，不做持續同步。

```bash
go run ./cmd/dbsync -from .env -to .env.supabase -dry-run   # 只列兩邊各表筆數
go run ./cmd/dbsync -from .env -to .env.supabase -yes       # 本地 → 雲端
go run ./cmd/dbsync -from .env.supabase -to .env -yes       # 雲端 → 本地
```

- 兩邊先套用最新 migration；覆蓋前把目標備份成 CSV（`backend/backups/`）。
- 目標端在單一交易內清空再依外鍵順序複製，中途失敗整筆還原，目標不變。
- 完成後逐表比對筆數與內容雜湊，並檢查庫存餘額 = 流水帳加總。
- 複製期間請先停止來源端的 API，避免資料寫到一半。

**部署（Render）**：repo 根目錄的 `Dockerfile` 會建置前端、編譯 Go，產生單一映像。環境變數：`DATABASE_URL`（Supabase Session pooler）、`JWT_SECRET`、`ADMIN_PASSWORD`、`DEMO_PASSWORD`；`PORT` 由 Render 提供。

**Supabase 安全**：migration `00003` 對所有資料表啟用 RLS 且不建 policy，Supabase Data API 用匿名金鑰讀不到資料；Go 以資料表擁有者連線，不受影響。

**免費版限制**：Render 閒置 15 分鐘會休眠，第一個人打開要等約 30–60 秒；Supabase 7 天沒有使用會暫停，需到後台恢復。

API 慣例：`Authorization: Bearer <token>`；工廠範圍的 API 需帶 `X-Factory-ID`。

## 分期

- P1：權限、工廠/班別、站點/線別、產品/BOM/替代料/圖片、途程、不良代碼
- P2：製令單、流程卡 PDF、現場掃描、線別工時、不良記錄、產量報表
- P3：倉庫入出庫、流水帳、用料預警
- P4：生產排程
