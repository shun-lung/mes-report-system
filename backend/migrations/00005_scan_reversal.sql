-- +goose Up
-- 掃描撤銷：撤銷紀錄沿用原回報的 scanned_at / work_date / shift（統計在原時段沖回），
-- 實際撤銷時間記在 created_at，原因記在 note。
ALTER TABLE production_scans
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN note text;
CREATE INDEX ON production_scans (reversal_of);

-- +goose Down
DROP INDEX IF EXISTS production_scans_reversal_of_idx;
ALTER TABLE production_scans DROP COLUMN note, DROP COLUMN created_at;
