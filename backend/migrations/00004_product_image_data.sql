-- +goose Up
-- 產品圖片改存資料庫：雲端主機（Render 免費版）重新部署會清空硬碟，且 dbsync 切換資料庫時可一併搬移
ALTER TABLE product_images ADD COLUMN data bytea;
ALTER TABLE product_images ALTER COLUMN file_path DROP NOT NULL;

-- +goose Down
DELETE FROM product_images WHERE file_path IS NULL;
ALTER TABLE product_images ALTER COLUMN file_path SET NOT NULL;
ALTER TABLE product_images DROP COLUMN data;
