-- +goose Up
-- 對 public schema 所有資料表啟用 RLS 且不建 policy：
-- Supabase 的 Data API（PostgREST）會對外開放 public schema，啟用後匿名金鑰讀不到任何資料。
-- Go 以資料表擁有者身分連線，不受 RLS 影響；本地與雲端結構保持一致。

-- +goose StatementBegin
DO $$
DECLARE t record;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t.tablename);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE t record;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I DISABLE ROW LEVEL SECURITY', t.tablename);
    END LOOP;
END $$;
-- +goose StatementEnd
