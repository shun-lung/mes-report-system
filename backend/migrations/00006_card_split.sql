-- +goose Up
-- 拆卡／合併的原因與紀錄
ALTER TABLE flow_cards ADD COLUMN note text;
CREATE INDEX ON flow_cards (parent_card_id);

-- +goose Down
DROP INDEX IF EXISTS flow_cards_parent_card_id_idx;
ALTER TABLE flow_cards DROP COLUMN note;
