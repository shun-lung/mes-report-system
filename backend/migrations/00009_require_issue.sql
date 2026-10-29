-- +goose Up
-- 生管管制「強制發料」：本站使用的倉庫發料料件須發完，才能在本站上工與回報（預設不管制）
ALTER TABLE work_orders ADD COLUMN require_issue boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE work_orders DROP COLUMN require_issue;
