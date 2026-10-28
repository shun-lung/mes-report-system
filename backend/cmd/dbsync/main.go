// dbsync 在兩個 PostgreSQL 之間整份複製報工系統資料（例如本地 Docker ⇄ Supabase）。
//
//	go run ./cmd/dbsync -from .env -to .env.supabase -dry-run   列出兩邊各表筆數
//	go run ./cmd/dbsync -from .env -to .env.supabase -yes       本地 → 雲端
//	go run ./cmd/dbsync -from .env.supabase -to .env -yes       雲端 → 本地
//
// 流程：兩邊套用最新 migration → 備份目標為 CSV → 目標端單一交易內清空並依外鍵順序串流複製 → 核對筆數與內容雜湊。
// 複製期間來源端應停止寫入（先停 API）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mes/internal/config"
	"mes/internal/database"
)

type table struct {
	name    string
	columns []string // 不含計算欄位
	pk      []string
}

func main() {
	from := flag.String("from", ".env", "來源設定檔")
	to := flag.String("to", "", "目標設定檔（會被覆蓋）")
	yes := flag.Bool("yes", false, "確認覆蓋目標資料庫")
	dryRun := flag.Bool("dry-run", false, "只列出兩邊各表筆數，不寫入")
	backupDir := flag.String("backup-dir", "backups", "覆蓋前備份目標資料的目錄")
	flag.Parse()
	if *to == "" {
		log.Fatal("請指定 -to 目標設定檔，例如 -to .env.supabase")
	}

	srcURL, dstURL := databaseURL(*from), databaseURL(*to)
	if srcURL == dstURL {
		log.Fatal("來源與目標是同一個資料庫")
	}
	fmt.Printf("來源 %-16s %s\n目標 %-16s %s\n\n", *from, redact(srcURL), *to, redact(dstURL))

	ctx := context.Background()
	if *dryRun {
		dryRunReport(ctx, srcURL, dstURL)
		return
	}
	if !*yes {
		log.Fatal("這會清空並覆蓋目標資料庫；確認後請加上 -yes（可先用 -dry-run 查看）")
	}

	fmt.Println("▸ 套用 migration（兩邊結構一致）")
	for _, u := range []string{srcURL, dstURL} {
		if err := database.Migrate(u); err != nil {
			log.Fatalf("migrate %s: %v", redact(u), err)
		}
	}

	src := connect(ctx, srcURL)
	defer src.Close(ctx)
	dst := connect(ctx, dstURL)
	defer dst.Close(ctx)

	tables, err := loadTables(ctx, src)
	must(err)

	dir := filepath.Join(*backupDir, time.Now().Format("20060102-150405")+"-"+hostOf(dstURL))
	fmt.Printf("▸ 備份目標資料到 %s\n", dir)
	must(backup(ctx, dst, tables, dir))

	fmt.Println("▸ 複製資料（目標端單一交易，失敗會整筆還原）")
	started := time.Now()
	copied, err := copyAll(ctx, src, dst, tables)
	if err != nil {
		log.Fatalf("複製失敗，目標資料庫未變更：%v", err)
	}
	fmt.Printf("  完成，%d 張表，%s\n", len(tables), time.Since(started).Round(time.Millisecond))

	fmt.Println("▸ 核對")
	if !verify(ctx, src, dst, tables, copied) {
		os.Exit(1)
	}
}

// databaseURL 取設定檔中的 DATABASE_URL；未設定時為本機開發資料庫。
func databaseURL(file string) string {
	values, err := config.ReadEnvFile(file)
	if err != nil {
		log.Fatalf("讀取設定檔 %s：%v", file, err)
	}
	if u := values["DATABASE_URL"]; u != "" {
		return u
	}
	return config.DefaultDatabaseURL
}

func connect(ctx context.Context, u string) *pgx.Conn {
	conn, err := pgx.Connect(ctx, u)
	if err != nil {
		log.Fatalf("連線 %s：%v", redact(u), err)
	}
	// 核對雜湊依賴文字格式，兩邊固定相同的時區與日期格式
	if _, err := conn.Exec(ctx, `SET TIME ZONE 'UTC'; SET DateStyle = 'ISO, MDY'; SET IntervalStyle = 'postgres'`); err != nil {
		log.Fatal(err)
	}
	return conn
}

// loadTables 取得 public schema 的業務表，依外鍵關係排序（被參照的表在前）。
func loadTables(ctx context.Context, conn *pgx.Conn) ([]table, error) {
	rows, err := conn.Query(ctx, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}

	rows, err = conn.Query(ctx, `
		SELECT cl.relname, pr.relname
		FROM pg_constraint co
		JOIN pg_class cl ON cl.oid = co.conrelid
		JOIN pg_class pr ON pr.oid = co.confrelid
		JOIN pg_namespace n ON n.oid = cl.relnamespace
		WHERE co.contype = 'f' AND n.nspname = 'public' AND cl.oid <> pr.oid`)
	if err != nil {
		return nil, err
	}
	parents := map[string]map[string]bool{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		if parents[child] == nil {
			parents[child] = map[string]bool{}
		}
		parents[child][parent] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 拓撲排序；同層依名稱排序，結果固定
	var ordered []string
	done := map[string]bool{}
	for len(ordered) < len(names) {
		var ready []string
		for _, n := range names {
			if done[n] {
				continue
			}
			ok := true
			for p := range parents[n] {
				if !done[p] {
					ok = false
					break
				}
			}
			if ok {
				ready = append(ready, n)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("外鍵關係有循環，無法決定複製順序")
		}
		sort.Strings(ready)
		for _, n := range ready {
			done[n] = true
		}
		ordered = append(ordered, ready...)
	}

	tables := make([]table, 0, len(ordered))
	for _, name := range ordered {
		t := table{name: name}
		rows, err := conn.Query(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND is_generated = 'NEVER'
			ORDER BY ordinal_position`, name)
		if err != nil {
			return nil, err
		}
		if t.columns, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, err
		}
		rows, err = conn.Query(ctx, `
			SELECT a.attname FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
			WHERE i.indrelid = ('public.' || quote_ident($1))::regclass AND i.indisprimary
			ORDER BY array_position(i.indkey, a.attnum)`, name)
		if err != nil {
			return nil, err
		}
		if t.pk, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, err
		}
		if len(t.pk) == 0 {
			t.pk = t.columns
		}
		tables = append(tables, t)
	}
	return tables, nil
}

func backup(ctx context.Context, conn *pgx.Conn, tables []table, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, t := range tables {
		f, err := os.OpenFile(filepath.Join(dir, t.name+".csv"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, err = conn.PgConn().CopyTo(ctx, f, fmt.Sprintf(
			"COPY (SELECT * FROM %s ORDER BY %s) TO STDOUT (FORMAT csv, HEADER)", ident(t.name), idents(t.pk)))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("備份 %s：%w", t.name, err)
		}
	}
	return nil
}

// copyAll 在目標端單一交易內清空所有業務表，再依序串流複製；回傳各表筆數。
func copyAll(ctx context.Context, src, dst *pgx.Conn, tables []table) (map[string]int64, error) {
	copied := map[string]int64{}
	err := pgx.BeginFunc(ctx, dst, func(tx pgx.Tx) error {
		names := make([]string, len(tables))
		for i, t := range tables {
			names[i] = ident(t.name)
		}
		if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(names, ", ")+" RESTART IDENTITY"); err != nil {
			return err
		}
		for _, t := range tables {
			n, err := copyTable(ctx, src, tx, t)
			if err != nil {
				return fmt.Errorf("%s：%w", t.name, err)
			}
			copied[t.name] = n
			fmt.Printf("  %-24s %8d 筆\n", t.name, n)
		}
		return resetSequences(ctx, tx)
	})
	return copied, err
}

// copyTable 以 binary COPY 從來源串流到目標；依主鍵排序（UUIDv7 依時間遞增，自我參照的父列先寫入）。
func copyTable(ctx context.Context, src *pgx.Conn, tx pgx.Tx, t table) (int64, error) {
	cols := idents(t.columns)
	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		_, err := src.PgConn().CopyTo(ctx, pw, fmt.Sprintf(
			"COPY (SELECT %s FROM %s ORDER BY %s) TO STDOUT (FORMAT binary)", cols, ident(t.name), idents(t.pk)))
		pw.CloseWithError(err)
		errc <- err
	}()
	tag, err := tx.Conn().PgConn().CopyFrom(ctx, pr, fmt.Sprintf(
		"COPY %s (%s) FROM STDIN (FORMAT binary)", ident(t.name), cols))
	if err != nil {
		pr.CloseWithError(err)
		<-errc
		return 0, err
	}
	if err := <-errc; err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func resetSequences(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_default LIKE 'nextval(%'`)
	if err != nil {
		return err
	}
	type serial struct{ Table, Column string }
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[serial])
	if err != nil {
		return err
	}
	for _, s := range list {
		_, err := tx.Exec(ctx, fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence($1, $2), COALESCE((SELECT max(%s) FROM %s), 0) + 1, false)`,
			ident(s.Column), ident(s.Table)), "public."+s.Table, s.Column)
		if err != nil {
			return err
		}
	}
	return nil
}

func verify(ctx context.Context, src, dst *pgx.Conn, tables []table, copied map[string]int64) bool {
	ok := true
	fmt.Printf("  %-24s %8s %8s  %s\n", "資料表", "來源", "目標", "內容")
	for _, t := range tables {
		sc, sh := fingerprint(ctx, src, t)
		dc, dh := fingerprint(ctx, dst, t)
		status := "相符"
		if sc != dc || sh != dh || copied[t.name] != sc {
			status, ok = "✕ 不符", false
		}
		fmt.Printf("  %-24s %8d %8d  %s\n", t.name, sc, dc, status)
	}
	var bad int
	must(dst.QueryRow(ctx, `
		SELECT count(*) FROM inventory_balances b
		WHERE b.qty <> COALESCE((SELECT sum(qty) FROM inventory_transactions t
		                         WHERE t.warehouse_id = b.warehouse_id AND t.product_id = b.product_id), 0)`).Scan(&bad))
	if bad > 0 {
		ok = false
	}
	fmt.Printf("  庫存餘額 = 流水帳加總：%s\n", map[bool]string{true: "相符", false: fmt.Sprintf("✕ %d 筆不符", bad)}[bad == 0])
	if ok {
		fmt.Println("\n✓ 全部相符")
	} else {
		fmt.Println("\n✕ 有不符項目，請檢查（覆蓋前的備份在 backups/）")
	}
	return ok
}

func fingerprint(ctx context.Context, conn *pgx.Conn, t table) (int64, string) {
	order := make([]string, len(t.pk))
	for i, c := range t.pk {
		order[i] = "t." + ident(c)
	}
	var n int64
	var hash string
	must(conn.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*), md5(COALESCE(string_agg(t::text, E'\n' ORDER BY %s), '')) FROM %s t`,
		strings.Join(order, ", "), ident(t.name))).Scan(&n, &hash))
	return n, hash
}

func dryRunReport(ctx context.Context, srcURL, dstURL string) {
	src := connect(ctx, srcURL)
	defer src.Close(ctx)
	tables, err := loadTables(ctx, src)
	must(err)
	dst, err := pgx.Connect(ctx, dstURL)
	if err != nil {
		log.Fatalf("連線目標：%v", err)
	}
	defer dst.Close(ctx)
	fmt.Printf("  %-24s %8s %8s\n", "資料表", "來源", "目標")
	for _, t := range tables {
		var sc int64
		must(src.QueryRow(ctx, "SELECT count(*) FROM "+ident(t.name)).Scan(&sc))
		dc := "—"
		var n int64
		if err := dst.QueryRow(ctx, "SELECT count(*) FROM "+ident(t.name)).Scan(&n); err == nil {
			dc = fmt.Sprint(n)
		}
		fmt.Printf("  %-24s %8d %8s\n", t.name, sc, dc)
	}
	fmt.Println("\n（dry-run：未寫入任何資料；目標「—」表示尚無此表）")
}

func ident(s string) string { return pgx.Identifier{s}.Sanitize() }

func idents(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = ident(c)
	}
	return strings.Join(out, ", ")
}

func redact(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return "（無法解析的連線字串）"
	}
	return p.Redacted()
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return "unknown"
	}
	return strings.NewReplacer(":", "_", ".", "_").Replace(p.Host) + "_" + strings.TrimPrefix(p.Path, "/")
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
