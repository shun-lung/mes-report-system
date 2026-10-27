-- +goose Up
-- 客戶訂單（製造需求來源）→ 製令單

CREATE TABLE customers (
    id          uuid PRIMARY KEY,
    factory_id  uuid         NOT NULL REFERENCES factories(id),
    code        varchar(20)  NOT NULL,
    name        varchar(100) NOT NULL,
    is_active   boolean      NOT NULL DEFAULT true,
    UNIQUE (factory_id, code)
);

CREATE TABLE sales_orders (
    id           uuid PRIMARY KEY,
    factory_id   uuid         NOT NULL REFERENCES factories(id),
    so_no        varchar(40)  NOT NULL UNIQUE,
    customer_id  uuid         NOT NULL REFERENCES customers(id),
    customer_po  varchar(50),
    order_date   date         NOT NULL,
    status       varchar(15)  NOT NULL DEFAULT 'OPEN'
                 CHECK (status IN ('OPEN','IN_PRODUCTION','COMPLETED','CLOSED','CANCELLED')),
    note         text,
    created_by   uuid         NOT NULL REFERENCES users(id),
    created_at   timestamptz  NOT NULL DEFAULT now(),
    updated_at   timestamptz  NOT NULL DEFAULT now()
);

CREATE TABLE sales_order_lines (
    id              uuid PRIMARY KEY,
    sales_order_id  uuid          NOT NULL REFERENCES sales_orders(id) ON DELETE CASCADE,
    line_no         int           NOT NULL,
    product_id      uuid          NOT NULL REFERENCES products(id),
    qty             numeric(18,4) NOT NULL CHECK (qty > 0),
    due_date        date          NOT NULL,
    UNIQUE (sales_order_id, line_no)
);

ALTER TABLE work_orders
    ADD COLUMN sales_order_line_id uuid REFERENCES sales_order_lines(id),
    ADD COLUMN due_date date;

-- 倉管發料（出庫扣帳）
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_txn_type_check;
ALTER TABLE inventory_transactions ADD CONSTRAINT inventory_transactions_txn_type_check CHECK (txn_type IN (
    'RECEIPT','ISSUE','STATION_IN','STATION_OUT','CONSUME','FG_IN','SCRAP','ADJUST','TRANSFER','REVERSAL'));

-- +goose Down
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_txn_type_check;
ALTER TABLE inventory_transactions ADD CONSTRAINT inventory_transactions_txn_type_check CHECK (txn_type IN (
    'RECEIPT','STATION_IN','STATION_OUT','CONSUME','FG_IN','SCRAP','ADJUST','TRANSFER','REVERSAL'));
ALTER TABLE work_orders DROP COLUMN due_date, DROP COLUMN sales_order_line_id;
DROP TABLE sales_order_lines, sales_orders, customers;
