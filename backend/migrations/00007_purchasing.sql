-- +goose Up
-- 採購：供應商、採購單；缺料可自動拋採購單
CREATE TABLE suppliers (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    code        varchar(20)  NOT NULL,
    name        varchar(100) NOT NULL,
    contact     varchar(50),
    phone       varchar(30),
    lead_days   int          NOT NULL DEFAULT 7 CHECK (lead_days >= 0),
    is_active   boolean      NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

ALTER TABLE products ADD COLUMN default_supplier_id uuid REFERENCES suppliers(id);

CREATE TABLE purchase_orders (
    id             uuid PRIMARY KEY,
    factory_id     uuid         NOT NULL REFERENCES factories(id),
    po_no          varchar(40)  NOT NULL UNIQUE,
    supplier_id    uuid         REFERENCES suppliers(id),         -- 草稿可暫缺，下單前必填
    status         varchar(15)  NOT NULL DEFAULT 'DRAFT'
                   CHECK (status IN ('DRAFT','ORDERED','PARTIAL','RECEIVED','CANCELLED')),
    source         varchar(15)  NOT NULL DEFAULT 'MANUAL' CHECK (source IN ('MANUAL','SHORTAGE')),
    expected_date  date,
    note           text,
    created_by     uuid         NOT NULL REFERENCES users(id),
    ordered_at     timestamptz,
    created_at     timestamptz  NOT NULL DEFAULT now(),
    updated_at     timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX ON purchase_orders (factory_id, status);

CREATE TABLE purchase_order_lines (
    id             uuid PRIMARY KEY,
    po_id          uuid          NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    line_no        int           NOT NULL,
    product_id     uuid          NOT NULL REFERENCES products(id),
    qty            numeric(18,4) NOT NULL CHECK (qty > 0),
    received_qty   numeric(18,4) NOT NULL DEFAULT 0 CHECK (received_qty >= 0),
    work_order_id  uuid          REFERENCES work_orders(id),     -- 觸發缺料的製令
    note           text,
    UNIQUE (po_id, line_no)
);
CREATE INDEX ON purchase_order_lines (product_id);

ALTER TABLE material_alerts ADD COLUMN created_po_id uuid REFERENCES purchase_orders(id);
ALTER TABLE material_alerts DROP CONSTRAINT material_alerts_status_check;
ALTER TABLE material_alerts ADD CONSTRAINT material_alerts_status_check
    CHECK (status IN ('PENDING','CREATED_WO','CREATED_PO','PASSED'));

ALTER TABLE suppliers ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_order_lines ENABLE ROW LEVEL SECURITY;

-- +goose Down
UPDATE material_alerts SET status = 'PASSED' WHERE status = 'CREATED_PO';
ALTER TABLE material_alerts DROP CONSTRAINT material_alerts_status_check;
ALTER TABLE material_alerts ADD CONSTRAINT material_alerts_status_check
    CHECK (status IN ('PENDING','CREATED_WO','PASSED'));
ALTER TABLE material_alerts DROP COLUMN created_po_id;
DROP TABLE purchase_order_lines, purchase_orders;
ALTER TABLE products DROP COLUMN default_supplier_id;
DROP TABLE suppliers;
