-- +goose Up
-- 完工暫存倉：成品末站良品先進此倉，倉管勾選製令入庫後才轉進成品倉
ALTER TABLE warehouses DROP CONSTRAINT warehouses_type_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_type_check CHECK (type IN ('RAW','WIP','FG','STAGE'));

INSERT INTO warehouses (id, factory_id, code, name, type)
SELECT gen_random_uuid(), f.factory_id,
       CASE WHEN EXISTS (SELECT 1 FROM warehouses x WHERE x.factory_id = f.factory_id AND x.code = 'W04') THEN 'W04S' ELSE 'W04' END,
       '完工暫存倉', 'STAGE'
FROM (SELECT DISTINCT factory_id FROM warehouses WHERE type = 'FG') f
WHERE NOT EXISTS (SELECT 1 FROM warehouses s WHERE s.factory_id = f.factory_id AND s.type = 'STAGE');

-- +goose Down
DELETE FROM warehouses w WHERE type = 'STAGE'
  AND NOT EXISTS (SELECT 1 FROM inventory_transactions t WHERE t.warehouse_id = w.id);
ALTER TABLE warehouses DROP CONSTRAINT warehouses_type_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_type_check CHECK (type IN ('RAW','WIP','FG'));
