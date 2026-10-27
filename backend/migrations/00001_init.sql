-- +goose Up
-- 報工系統 初始資料結構（P1~P3 + 排程預留）
-- 慣例：
--   * 主鍵 uuid 由 Go 端產生 UUIDv7（時間有序，跨廠彙整不衝突）
--   * 所有時間 timestamptz（存 UTC），另存 work_date（依工廠時區 + 班別算出的生產日）
--   * 每張業務表帶 factory_id，各廠獨立部署、總部彙整
--   * 流水帳類表（inventory_transactions / production_scans）只新增不修改，錯誤以沖銷處理

CREATE EXTENSION IF NOT EXISTS citext;

-- =========================================================
-- 組織：工廠、班別
-- =========================================================
CREATE TABLE factories (
    id          uuid PRIMARY KEY,
    code        varchar(10)  NOT NULL UNIQUE,          -- 單號前綴，如 TW1、VN1
    name        varchar(100) NOT NULL,
    timezone    varchar(64)  NOT NULL,                 -- IANA，如 Asia/Taipei
    is_active   boolean      NOT NULL DEFAULT true,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now()
);

-- 2 班或 3 班，由各廠自行設定；end_time < start_time 表示跨午夜
CREATE TABLE shifts (
    id          uuid PRIMARY KEY,
    factory_id  uuid        NOT NULL REFERENCES factories(id),
    code        varchar(10) NOT NULL,
    name        varchar(50) NOT NULL,
    start_time  time        NOT NULL,                  -- 工廠當地時間
    end_time    time        NOT NULL,
    work_date_offset int    NOT NULL DEFAULT 0 CHECK (work_date_offset IN (0, -1)),  -- -1：如 00:00 開始的大夜班算前一天
    sort_order  int         NOT NULL DEFAULT 0,
    is_active   boolean     NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

-- =========================================================
-- 權限：使用者 / 角色 / 權限（依工廠指派）
-- =========================================================
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    email           citext       NOT NULL UNIQUE,
    name            varchar(100) NOT NULL,
    password_hash   varchar(255) NOT NULL,
    employee_no     varchar(30),
    is_super_admin  boolean      NOT NULL DEFAULT false,  -- 總部管理員，可跨廠
    is_active       boolean      NOT NULL DEFAULT true,
    last_login_at   timestamptz,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id          uuid PRIMARY KEY,
    factory_id  uuid REFERENCES factories(id),        -- NULL = 共用角色範本
    code        varchar(50)  NOT NULL,
    name        varchar(100) NOT NULL,
    is_system   boolean      NOT NULL DEFAULT false,
    UNIQUE NULLS NOT DISTINCT (factory_id, code)
);

-- 權限代碼由程式定義後 seed，例如 product:view、product:edit、wo:release、inventory:adjust
CREATE TABLE permissions (
    code        varchar(80)  PRIMARY KEY,
    module      varchar(50)  NOT NULL,                 -- 對應前端頁面群組
    name        varchar(100) NOT NULL,
    sort_order  int          NOT NULL DEFAULT 0
);

CREATE TABLE role_permissions (
    role_id          uuid        NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_code  varchar(80) NOT NULL REFERENCES permissions(code),
    PRIMARY KEY (role_id, permission_code)
);

-- 使用者在「哪個工廠」擁有「哪個角色」
CREATE TABLE user_factory_roles (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    factory_id  uuid NOT NULL REFERENCES factories(id),
    role_id     uuid NOT NULL REFERENCES roles(id),
    PRIMARY KEY (user_id, factory_id, role_id)
);

CREATE TABLE audit_logs (
    id          bigserial PRIMARY KEY,
    factory_id  uuid,
    user_id     uuid,
    action      varchar(30)  NOT NULL,                 -- CREATE / UPDATE / DELETE / LOGIN ...
    entity      varchar(50)  NOT NULL,
    entity_id   varchar(64),
    before      jsonb,
    after       jsonb,
    created_at  timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX ON audit_logs (entity, entity_id);

-- 單號流水：{工廠}-{類別}-{yyyyMMdd}-{0001}
CREATE TABLE doc_sequences (
    factory_id  uuid        NOT NULL REFERENCES factories(id),
    doc_type    varchar(10) NOT NULL,                  -- WO / TX / CARD ...
    seq_date    date        NOT NULL,
    last_no     int         NOT NULL DEFAULT 0,
    PRIMARY KEY (factory_id, doc_type, seq_date)
);

-- =========================================================
-- 基本資料：站點 / 線別 / 產品 / BOM / 途程 / 不良代碼
-- =========================================================
CREATE TABLE work_stations (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    code        varchar(20)  NOT NULL,
    name        varchar(100) NOT NULL,                 -- 前期準備 / 針車 / 成型 / 整理
    sort_order  int          NOT NULL DEFAULT 0,
    is_active   boolean      NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

CREATE TABLE production_lines (
    id                 uuid PRIMARY KEY,
    factory_id         uuid          NOT NULL REFERENCES factories(id),
    station_id         uuid          NOT NULL REFERENCES work_stations(id),
    code               varchar(20)   NOT NULL,
    name               varchar(100)  NOT NULL,
    capacity_per_hour  numeric(10,2) NOT NULL DEFAULT 0,   -- 排程用：時產能（雙）
    std_headcount      int           NOT NULL DEFAULT 0,   -- 排程用：標準人力
    is_active          boolean       NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

CREATE TABLE products (
    id                     uuid PRIMARY KEY,
    factory_id             uuid          NOT NULL REFERENCES factories(id),
    item_no                varchar(50)   NOT NULL,     -- 品號
    name                   varchar(200)  NOT NULL,     -- 品名
    barcode                varchar(100),               -- 條碼
    spec                   text,                       -- 規格
    unit                   varchar(10)   NOT NULL DEFAULT '雙',
    category               varchar(10)   NOT NULL CHECK (category IN ('FG','SFG','RAW','AUX')), -- 成品/半成品/基本材料/輔助料件
    default_deduct_timing  varchar(10)   NOT NULL DEFAULT 'ON_ISSUE'
                           CHECK (default_deduct_timing IN ('ON_ARRIVAL','ON_ISSUE')),  -- 到站扣帳 / 出庫扣帳
    safety_stock           numeric(18,4) NOT NULL DEFAULT 0,      -- 參考用，不影響缺料預警
    alert_pct              numeric(5,2)  NOT NULL DEFAULT 3,      -- 缺料預警緩衝 %，見 material_alerts
    card_qty               int           NOT NULL DEFAULT 100,    -- 流程卡預設每張數量
    is_active              boolean       NOT NULL DEFAULT true,
    created_at             timestamptz   NOT NULL DEFAULT now(),
    updated_at             timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (factory_id, item_no),
    UNIQUE (factory_id, barcode)
);

CREATE TABLE product_images (
    id          uuid PRIMARY KEY,
    product_id  uuid         NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    file_path   varchar(500) NOT NULL,                 -- 檔案系統 / MinIO 物件路徑
    file_name   varchar(255) NOT NULL,
    mime_type   varchar(100) NOT NULL,
    file_size   bigint       NOT NULL,
    sort_order  int          NOT NULL DEFAULT 0,
    is_primary  boolean      NOT NULL DEFAULT false,
    created_at  timestamptz  NOT NULL DEFAULT now()
);

-- 多階 BOM：成品 → 半成品 → 基本材料 / 輔助料件
CREATE TABLE product_bom (
    id                  uuid PRIMARY KEY,
    factory_id          uuid          NOT NULL REFERENCES factories(id),
    parent_product_id   uuid          NOT NULL REFERENCES products(id),
    child_product_id    uuid          NOT NULL REFERENCES products(id),
    material_type       varchar(10)   NOT NULL CHECK (material_type IN ('MAIN','AUX')),
    qty_per             numeric(18,6) NOT NULL,        -- 每單位用量
    scrap_rate          numeric(5,2)  NOT NULL DEFAULT 0,
    deduct_timing       varchar(10)   CHECK (deduct_timing IN ('ON_ARRIVAL','ON_ISSUE')), -- NULL = 用料件預設
    consume_station_id  uuid          REFERENCES work_stations(id),  -- 到站扣帳時在哪站扣
    sort_order          int           NOT NULL DEFAULT 0,
    UNIQUE (parent_product_id, child_product_id),
    CHECK (parent_product_id <> child_product_id)
);

CREATE TABLE bom_substitutes (
    id                     uuid PRIMARY KEY,
    bom_id                 uuid          NOT NULL REFERENCES product_bom(id) ON DELETE CASCADE,
    substitute_product_id  uuid          NOT NULL REFERENCES products(id),
    ratio                  numeric(18,6) NOT NULL DEFAULT 1,   -- 替代換算比
    priority               int           NOT NULL DEFAULT 0,
    UNIQUE (bom_id, substitute_product_id)
);

-- 途程有版本；工單鎖定開立當下的版本
CREATE TABLE routings (
    id          uuid PRIMARY KEY,
    factory_id  uuid        NOT NULL REFERENCES factories(id),
    product_id  uuid        NOT NULL REFERENCES products(id),
    version     int         NOT NULL,
    is_active   boolean     NOT NULL DEFAULT false,
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, version)
);
CREATE UNIQUE INDEX routings_one_active ON routings (product_id) WHERE is_active;

CREATE TABLE routing_steps (
    id                    uuid PRIMARY KEY,
    routing_id            uuid          NOT NULL REFERENCES routings(id) ON DELETE CASCADE,
    seq                   int           NOT NULL,      -- 10, 20, 30 ...
    station_id            uuid          NOT NULL REFERENCES work_stations(id),
    std_seconds_per_unit  numeric(10,2) NOT NULL DEFAULT 0,  -- 標準工時（效率 / 排程用）
    is_report_point       boolean       NOT NULL DEFAULT true,
    UNIQUE (routing_id, seq)
);

CREATE TABLE defect_codes (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    station_id  uuid         REFERENCES work_stations(id),   -- NULL = 全站通用
    code        varchar(20)  NOT NULL,
    name        varchar(100) NOT NULL,
    category    varchar(50),
    is_active   boolean      NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

-- =========================================================
-- 倉庫與庫存流水帳
-- =========================================================
CREATE TABLE warehouses (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    code        varchar(20)  NOT NULL,
    name        varchar(100) NOT NULL,
    type        varchar(10)  NOT NULL CHECK (type IN ('RAW','WIP','FG')),  -- 材料倉 / 暫存倉 / 成品倉
    station_id  uuid         REFERENCES work_stations(id),  -- 暫存倉對應站點
    is_active   boolean      NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

CREATE TABLE inventory_balances (
    warehouse_id  uuid          NOT NULL REFERENCES warehouses(id),
    product_id    uuid          NOT NULL REFERENCES products(id),
    qty           numeric(18,4) NOT NULL DEFAULT 0,
    updated_at    timestamptz   NOT NULL DEFAULT now(),
    PRIMARY KEY (warehouse_id, product_id)
);

-- 異動歷程：與 balances 同一交易寫入；不可 UPDATE / DELETE
CREATE TABLE inventory_transactions (
    id             uuid PRIMARY KEY,
    factory_id     uuid          NOT NULL REFERENCES factories(id),
    txn_no         varchar(40)   NOT NULL UNIQUE,
    txn_type       varchar(20)   NOT NULL CHECK (txn_type IN (
                       'RECEIPT',      -- 採購/外部入庫
                       'STATION_IN',   -- 到站入庫（暫存倉）
                       'STATION_OUT',  -- 出庫（暫存倉）
                       'CONSUME',      -- 依 BOM 扣料
                       'FG_IN',        -- 成品完工入庫
                       'SCRAP',        -- 報廢
                       'ADJUST',       -- 盤點調整
                       'TRANSFER',     -- 移轉
                       'REVERSAL')),   -- 沖銷
    warehouse_id   uuid          NOT NULL REFERENCES warehouses(id),
    product_id     uuid          NOT NULL REFERENCES products(id),
    qty            numeric(18,4) NOT NULL,             -- 正入負出
    balance_after  numeric(18,4) NOT NULL,
    work_order_id  uuid,
    ref_type       varchar(30),                        -- scan / flow_card / manual ...
    ref_id         uuid,
    reversal_of    uuid REFERENCES inventory_transactions(id),
    occurred_at    timestamptz   NOT NULL DEFAULT now(),
    work_date      date          NOT NULL,
    user_id        uuid          NOT NULL REFERENCES users(id),
    note           text
);
CREATE INDEX ON inventory_transactions (factory_id, work_date);
CREATE INDEX ON inventory_transactions (product_id, occurred_at);
CREATE INDEX ON inventory_transactions (work_order_id);

-- +goose StatementBegin
CREATE FUNCTION forbid_modify() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only; use a reversal record', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER inventory_transactions_append_only
    BEFORE UPDATE OR DELETE ON inventory_transactions
    FOR EACH ROW EXECUTE FUNCTION forbid_modify();

-- =========================================================
-- 生產：製令單 / 用料 / 流程卡 / 掃描 / 不良 / 線別工時
-- =========================================================
CREATE TABLE work_orders (
    id             uuid PRIMARY KEY,
    factory_id     uuid          NOT NULL REFERENCES factories(id),
    wo_no          varchar(40)   NOT NULL UNIQUE,
    product_id     uuid          NOT NULL REFERENCES products(id),
    routing_id     uuid          NOT NULL REFERENCES routings(id),  -- 鎖定途程版本
    planned_qty    numeric(18,4) NOT NULL,
    card_qty       int           NOT NULL,             -- 帶入 products.card_qty，可改
    status         varchar(15)   NOT NULL DEFAULT 'DRAFT' CHECK (status IN (
                       'DRAFT','RELEASED','SCHEDULED','IN_PROGRESS','COMPLETED','CLOSED','CANCELLED')),
    source         varchar(15)   NOT NULL DEFAULT 'MANUAL' CHECK (source IN ('MANUAL','SHORTAGE')),
    parent_wo_id   uuid          REFERENCES work_orders(id),       -- 缺料衍生的半成品工單
    planned_start  timestamptz,                        -- 排程預留
    planned_end    timestamptz,
    released_at    timestamptz,
    completed_at   timestamptz,
    created_by     uuid          NOT NULL REFERENCES users(id),
    created_at     timestamptz   NOT NULL DEFAULT now(),
    updated_at     timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX ON work_orders (factory_id, status);

-- 開立時從 BOM 複製；換料記錄於此
CREATE TABLE work_order_materials (
    id                     uuid PRIMARY KEY,
    work_order_id          uuid          NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
    bom_id                 uuid          REFERENCES product_bom(id),
    product_id             uuid          NOT NULL REFERENCES products(id),
    material_type          varchar(10)   NOT NULL,
    qty_per                numeric(18,6) NOT NULL,
    required_qty           numeric(18,4) NOT NULL,
    issued_qty             numeric(18,4) NOT NULL DEFAULT 0,
    deduct_timing          varchar(10)   NOT NULL,
    consume_station_id     uuid          REFERENCES work_stations(id),
    substituted_from_id    uuid          REFERENCES products(id),  -- 換料前料號
    substituted_by         uuid          REFERENCES users(id),
    substituted_at         timestamptz
);

-- 流程卡：開立時預先產生（預設 100 雙/張），拆卡時動態產生子卡
CREATE TABLE flow_cards (
    id              uuid PRIMARY KEY,
    factory_id      uuid          NOT NULL REFERENCES factories(id),
    work_order_id   uuid          NOT NULL REFERENCES work_orders(id),
    card_no         varchar(50)   NOT NULL UNIQUE,     -- 條碼內容
    parent_card_id  uuid          REFERENCES flow_cards(id),
    qty             numeric(18,4) NOT NULL,
    status          varchar(15)   NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','IN_PROGRESS','DONE','SPLIT','VOID')),
    print_count     int           NOT NULL DEFAULT 0,
    last_printed_at timestamptz,
    created_at      timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX ON flow_cards (work_order_id);

-- 每張卡在每站的進度；可回報量 = 上站良品 - 本站已回報
CREATE TABLE flow_card_progress (
    card_id        uuid          NOT NULL REFERENCES flow_cards(id),
    step_seq       int           NOT NULL,
    station_id     uuid          NOT NULL REFERENCES work_stations(id),
    input_qty      numeric(18,4) NOT NULL DEFAULT 0,   -- 上站良品流入
    good_qty       numeric(18,4) NOT NULL DEFAULT 0,
    defect_qty     numeric(18,4) NOT NULL DEFAULT 0,
    remaining_qty  numeric(18,4) GENERATED ALWAYS AS (input_qty - good_qty - defect_qty) STORED,
    first_scan_at  timestamptz,
    last_scan_at   timestamptz,
    PRIMARY KEY (card_id, step_seq),
    CHECK (good_qty + defect_qty <= input_qty)
);

-- 掃描回報明細：只新增，錯誤以 is_reversal 沖銷
CREATE TABLE production_scans (
    id             uuid PRIMARY KEY,
    factory_id     uuid          NOT NULL REFERENCES factories(id),
    work_order_id  uuid          NOT NULL REFERENCES work_orders(id),
    card_id        uuid          NOT NULL REFERENCES flow_cards(id),
    step_seq       int           NOT NULL,
    station_id     uuid          NOT NULL REFERENCES work_stations(id),
    line_id        uuid          NOT NULL REFERENCES production_lines(id),
    product_id     uuid          NOT NULL REFERENCES products(id),
    good_qty       numeric(18,4) NOT NULL DEFAULT 0,
    defect_qty     numeric(18,4) NOT NULL DEFAULT 0,
    is_rework      boolean       NOT NULL DEFAULT false,
    reversal_of    uuid          REFERENCES production_scans(id),
    scanned_at     timestamptz   NOT NULL DEFAULT now(),
    work_date      date          NOT NULL,
    shift_id       uuid          NOT NULL REFERENCES shifts(id),
    user_id        uuid          NOT NULL REFERENCES users(id),
    device_id      varchar(50)
);
CREATE INDEX ON production_scans (factory_id, work_date, line_id);
CREATE INDEX ON production_scans (work_order_id);
CREATE INDEX ON production_scans (card_id);

CREATE TRIGGER production_scans_append_only
    BEFORE UPDATE OR DELETE ON production_scans
    FOR EACH ROW EXECUTE FUNCTION forbid_modify();

CREATE TABLE defect_records (
    id                 uuid PRIMARY KEY,
    scan_id            uuid          NOT NULL REFERENCES production_scans(id),
    defect_code_id     uuid          NOT NULL REFERENCES defect_codes(id),
    qty                numeric(18,4) NOT NULL,
    disposition        varchar(10)   NOT NULL CHECK (disposition IN ('REWORK','SCRAP','ACCEPT')),
    rework_station_id  uuid          REFERENCES work_stations(id),
    note               text
);

-- 線別工時：上工 / 下工掃描，記人數 × 時間
CREATE TABLE line_labor_sessions (
    id             uuid PRIMARY KEY,
    factory_id     uuid        NOT NULL REFERENCES factories(id),
    line_id        uuid        NOT NULL REFERENCES production_lines(id),
    work_order_id  uuid        NOT NULL REFERENCES work_orders(id),
    headcount      int         NOT NULL CHECK (headcount > 0),
    started_at     timestamptz NOT NULL,
    ended_at       timestamptz,
    work_date      date        NOT NULL,
    shift_id       uuid        NOT NULL REFERENCES shifts(id),
    started_by     uuid        NOT NULL REFERENCES users(id),
    ended_by       uuid        REFERENCES users(id),
    man_minutes    numeric(12,2) GENERATED ALWAYS AS
                   (headcount * EXTRACT(EPOCH FROM (ended_at - started_at)) / 60) STORED,
    CHECK (ended_at IS NULL OR ended_at > started_at)
);
-- 同一條線同時只能有一筆進行中的上工
CREATE UNIQUE INDEX line_labor_one_open ON line_labor_sessions (line_id) WHERE ended_at IS NULL;
CREATE INDEX ON line_labor_sessions (factory_id, work_date);

-- =========================================================
-- 預警與通知
-- =========================================================
-- 以工單需求判斷，不依賴安全庫存設定：
--   需求量 = 本工單未扣料量 (required_qty - issued_qty)
--   可用量 = 庫存 - 其他已開立工單未扣料量
--   觸發：可用量 < 需求量 × (1 + alert_pct / 100)   例：庫存 10、需求 13 → 通知
-- 檢查時機：工單開立、換料、扣帳後
-- 生管於彈窗選擇「通知建單」(產生 DRAFT 半成品工單) 或「PASS」
CREATE TABLE material_alerts (
    id                uuid PRIMARY KEY,
    factory_id        uuid          NOT NULL REFERENCES factories(id),
    work_order_id     uuid          REFERENCES work_orders(id),
    wo_material_id    uuid          REFERENCES work_order_materials(id),
    product_id        uuid          NOT NULL REFERENCES products(id),
    required_qty      numeric(18,4) NOT NULL,
    available_qty     numeric(18,4) NOT NULL,
    threshold_qty     numeric(18,4) NOT NULL,
    shortage_qty      numeric(18,4) GENERATED ALWAYS AS (GREATEST(required_qty - available_qty, 0)) STORED,
    status            varchar(15)   NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','CREATED_WO','PASSED')),
    created_wo_id     uuid          REFERENCES work_orders(id),
    decided_by        uuid          REFERENCES users(id),
    decided_at        timestamptz,
    note              text,
    created_at        timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX ON material_alerts (factory_id, status);

CREATE TABLE notifications (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    user_id     uuid         NOT NULL REFERENCES users(id),
    type        varchar(30)  NOT NULL,
    title       varchar(200) NOT NULL,
    body        text,
    ref_type    varchar(30),
    ref_id      uuid,
    read_at     timestamptz,
    created_at  timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX ON notifications (user_id) WHERE read_at IS NULL;

-- =========================================================
-- 報表匯總（混合統計：當小時看明細，歷史看匯總）
-- =========================================================
CREATE TABLE output_hourly (
    factory_id     uuid          NOT NULL,
    work_date      date          NOT NULL,
    shift_id       uuid          NOT NULL,
    hour_start     timestamptz   NOT NULL,
    line_id        uuid          NOT NULL,
    station_id     uuid          NOT NULL,
    work_order_id  uuid          NOT NULL,
    product_id     uuid          NOT NULL,
    good_qty       numeric(18,4) NOT NULL DEFAULT 0,
    defect_qty     numeric(18,4) NOT NULL DEFAULT 0,
    PRIMARY KEY (factory_id, hour_start, line_id, work_order_id)
);
CREATE INDEX ON output_hourly (factory_id, work_date);

-- =========================================================
-- 生產排程（P4 預留，暫不建立）
--   schedule_plans  (id, factory_id, name, period_start, period_end, status)
--   schedule_items  (id, plan_id, work_order_id, line_id, step_seq, planned_start, planned_end, planned_qty)
--   依賴：production_lines.capacity_per_hour、routing_steps.std_seconds_per_unit、work_orders.planned_*
-- =========================================================

-- +goose Down
DROP TABLE IF EXISTS output_hourly, notifications, material_alerts, line_labor_sessions,
    defect_records, production_scans, flow_card_progress, flow_cards, work_order_materials,
    work_orders, inventory_transactions, inventory_balances, warehouses, defect_codes,
    routing_steps, routings, bom_substitutes, product_bom, product_images, products,
    production_lines, work_stations, doc_sequences, audit_logs, user_factory_roles,
    role_permissions, permissions, roles, users, shifts, factories CASCADE;
DROP FUNCTION IF EXISTS forbid_modify();
