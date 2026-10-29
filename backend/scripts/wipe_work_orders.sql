-- 清除全部製令與其衍生資料，保留客戶訂單（訂單狀態回到「未生產」）。
-- 一併刪除：流程卡、報工（含不良）、上工、每小時產量、缺料預警與通知、製令用料、
--           製令相關庫存異動（發料、到站扣帳、完工入庫及其沖銷）、
--           缺料採購單（source=SHORTAGE、明細掛製令、或由預警產生）與其收料異動。
-- 庫存餘額與每筆異動的 balance_after 依剩餘異動重算。單一交易，失敗即全部還原。
-- 用法：psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f wipe_work_orders.sql
BEGIN;

ALTER TABLE inventory_transactions DISABLE TRIGGER inventory_transactions_append_only;
ALTER TABLE production_scans DISABLE TRIGGER production_scans_append_only;

CREATE TEMP TABLE del_po ON COMMIT DROP AS
SELECT id FROM purchase_orders WHERE source = 'SHORTAGE'
UNION SELECT po_id FROM purchase_order_lines WHERE work_order_id IS NOT NULL
UNION SELECT created_po_id FROM material_alerts WHERE created_po_id IS NOT NULL;

CREATE TEMP TABLE del_tx ON COMMIT DROP AS
WITH RECURSIVE t AS (
    SELECT id FROM inventory_transactions
    WHERE work_order_id IS NOT NULL
       OR (ref_type = 'po_line' AND ref_id IN (SELECT id FROM purchase_order_lines WHERE po_id IN (SELECT id FROM del_po)))
    UNION
    SELECT x.id FROM inventory_transactions x JOIN t ON x.reversal_of = t.id)
SELECT id FROM t;

SELECT (SELECT count(*) FROM work_orders) AS 製令,
       (SELECT count(*) FROM flow_cards) AS 流程卡,
       (SELECT count(*) FROM production_scans) AS 報工,
       (SELECT count(*) FROM del_po) AS 採購單,
       (SELECT count(*) FROM del_tx) AS 庫存異動;

DELETE FROM inventory_transactions WHERE id IN (SELECT id FROM del_tx);
DELETE FROM notifications WHERE ref_type = 'material_alert';
DELETE FROM material_alerts;
DELETE FROM purchase_order_lines WHERE po_id IN (SELECT id FROM del_po);
DELETE FROM purchase_orders WHERE id IN (SELECT id FROM del_po);
DELETE FROM defect_records;
DELETE FROM production_scans;
DELETE FROM output_hourly;
DELETE FROM line_labor_sessions;
DELETE FROM flow_card_progress;
DELETE FROM flow_cards;
DELETE FROM work_order_materials;
DELETE FROM work_orders;

-- 庫存：重算每筆異動的 balance_after 與餘額
UPDATE inventory_transactions t SET balance_after = r.bal
FROM (SELECT id, sum(qty) OVER (PARTITION BY warehouse_id, product_id ORDER BY occurred_at, id) AS bal
      FROM inventory_transactions) r
WHERE r.id = t.id AND t.balance_after <> r.bal;
UPDATE inventory_balances b SET qty = coalesce(s.qty, 0), updated_at = now()
FROM inventory_balances b2
LEFT JOIN (SELECT warehouse_id, product_id, sum(qty) AS qty FROM inventory_transactions GROUP BY 1, 2) s
       ON s.warehouse_id = b2.warehouse_id AND s.product_id = b2.product_id
WHERE b.warehouse_id = b2.warehouse_id AND b.product_id = b2.product_id AND b.qty <> coalesce(s.qty, 0);

UPDATE sales_orders SET status = 'OPEN', updated_at = now() WHERE status IN ('IN_PRODUCTION', 'COMPLETED');
-- 製令號從 0001 重新編；採購單全部刪光時採購單號也重編
DELETE FROM doc_sequences WHERE doc_type = 'WO';
DELETE FROM doc_sequences WHERE doc_type = 'PO' AND NOT EXISTS (SELECT 1 FROM purchase_orders);

ALTER TABLE inventory_transactions ENABLE TRIGGER inventory_transactions_append_only;
ALTER TABLE production_scans ENABLE TRIGGER production_scans_append_only;

-- 核對：餘額＝異動加總
SELECT count(*) AS 庫存不一致筆數 FROM inventory_balances b
LEFT JOIN (SELECT warehouse_id, product_id, sum(qty) AS qty FROM inventory_transactions GROUP BY 1, 2) s
       ON s.warehouse_id = b.warehouse_id AND s.product_id = b.product_id
WHERE b.qty <> coalesce(s.qty, 0);
SELECT status AS 訂單狀態, count(*) FROM sales_orders GROUP BY 1 ORDER BY 1;

COMMIT;
