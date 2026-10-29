-- +goose Up
-- 生管管制：製令可要求「人員授權」（只有被授權該線別的人能回報）與「強制上工」（須先上工本製令才能回報），預設皆不管制
ALTER TABLE work_orders
    ADD COLUMN require_auth  boolean NOT NULL DEFAULT false,
    ADD COLUMN require_labor boolean NOT NULL DEFAULT false;

-- 人員可回報的線別
CREATE TABLE user_line_assignments (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    factory_id uuid NOT NULL REFERENCES factories(id),
    line_id    uuid NOT NULL REFERENCES production_lines(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, line_id)
);
CREATE INDEX ON user_line_assignments (factory_id, user_id);
ALTER TABLE user_line_assignments ENABLE ROW LEVEL SECURITY;

-- +goose Down
DROP TABLE user_line_assignments;
ALTER TABLE work_orders DROP COLUMN require_labor, DROP COLUMN require_auth;
