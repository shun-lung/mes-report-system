package production

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/testutil"
)

// 整合測試共用小工具。測試案例編號（TC-xx）對照 docs/testing/03-測試案例.md。

type env struct {
	*testutil.Fixture
	svc *Service
	ctx context.Context
}

func setup(t *testing.T) *env {
	f := testutil.Setup(t)
	return &env{Fixture: f, svc: NewService(f.Pool), ctx: context.Background()}
}

// newWO 建立並開立製令，回傳製令 ID。
func (e *env) newWO(t *testing.T, qty float64, ctl Control) uuid.UUID {
	t.Helper()
	id, _, err := e.svc.CreateWorkOrder(e.ctx, e.Factory, e.Product, qty, nil, ctl, e.User, e.Now)
	if err != nil {
		t.Fatalf("建立製令：%v", err)
	}
	if _, err := e.svc.Release(e.ctx, e.Factory, id, e.User, e.Now); err != nil {
		t.Fatalf("開立製令：%v", err)
	}
	return id
}

// cards 製令的流程卡號（依卡號排序）。
func (e *env) cards(t *testing.T, woID uuid.UUID) []string {
	t.Helper()
	rows, err := e.Pool.Query(e.ctx, `SELECT card_no FROM flow_cards WHERE work_order_id = $1 AND status <> 'VOID' ORDER BY card_no`, woID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (e *env) scan(card string, line uuid.UUID, good float64, defects ...DefectInput) (ScanResult, error) {
	return e.svc.Scan(e.ctx, ScanInput{FactoryID: e.Factory, CardNo: card, LineID: line, GoodQty: good, Defects: defects, UserID: e.User, At: e.Now})
}

// mustScan 回報必須成功。
func (e *env) mustScan(t *testing.T, card string, line uuid.UUID, good float64, defects ...DefectInput) ScanResult {
	t.Helper()
	r, err := e.scan(card, line, good, defects...)
	if err != nil {
		t.Fatalf("回報 %s %g 雙失敗：%v", card, good, err)
	}
	return r
}

// finishAll 每張卡依序在成型、整理全數以良品報完。
func (e *env) finishAll(t *testing.T, woID uuid.UUID) {
	t.Helper()
	for _, c := range e.cards(t, woID) {
		qty := e.QueryFloat(t, `SELECT qty::float8 FROM flow_cards WHERE card_no = $1`, c)
		e.mustScan(t, c, e.LineFormA, qty)
		e.mustScan(t, c, e.LineFinA, qty)
	}
}

func (e *env) woStatus(t *testing.T, woID uuid.UUID) string {
	t.Helper()
	return e.QueryString(t, `SELECT status FROM work_orders WHERE id = $1`, woID)
}

// openLabor 某線目前上工中的製令數。
func (e *env) openLabor(t *testing.T, line uuid.UUID) float64 {
	t.Helper()
	return e.QueryFloat(t, `SELECT count(*)::float8 FROM line_labor_sessions WHERE line_id = $1 AND ended_at IS NULL`, line)
}

func (e *env) materialID(t *testing.T, woID, productID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.Pool.QueryRow(e.ctx, `SELECT id FROM work_order_materials WHERE work_order_id = $1 AND product_id = $2`, woID, productID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// wantErr 必須失敗，且錯誤訊息含指定文字。
func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("預期失敗（%s），實際卻成功", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("錯誤訊息不符：要含「%s」，實際「%v」", contains, err)
	}
}

func wantEq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s：預期 %v，實際 %v", what, want, got)
	}
}
