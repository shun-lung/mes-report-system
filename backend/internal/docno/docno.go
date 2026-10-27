package docno

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"mes/internal/database"
)

// Next 產生單號：{工廠代碼}-{類別}-{yyyyMMdd}-{流水}，流水號依工廠、類別、日期遞增。
func Next(ctx context.Context, q database.Querier, factoryID uuid.UUID, docType string, date time.Time) (string, error) {
	var code string
	var n int
	err := q.QueryRow(ctx, `
		WITH seq AS (
			INSERT INTO doc_sequences (factory_id, doc_type, seq_date, last_no) VALUES ($1, $2, $3, 1)
			ON CONFLICT (factory_id, doc_type, seq_date) DO UPDATE SET last_no = doc_sequences.last_no + 1
			RETURNING last_no)
		SELECT f.code, seq.last_no FROM seq, factories f WHERE f.id = $1`,
		factoryID, docType, date).Scan(&code, &n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s-%04d", code, docType, date.Format("20060102"), n), nil
}
