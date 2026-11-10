package production

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ───── 發料（REQ-ISSUE）─────

// TC-07 發料超過未發量 → 整筆拒絕
func TestTC07_IssueOverRequired(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	box := e.materialID(t, wo, e.Box)
	_, err := e.svc.IssueMaterials(e.ctx, e.Factory, wo, e.User, map[uuid.UUID]float64{box: 101}, e.Now)
	wantErr(t, err, "超過未發量")
	wantEq(t, "庫存不變", e.Stock(t, e.WhRaw, e.Box), 1000.0)
}

// TC-08 發料超過庫存 → 整筆拒絕
func TestTC08_IssueOverStock(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 1200, Control{})
	box := e.materialID(t, wo, e.Box)
	_, err := e.svc.IssueMaterials(e.ctx, e.Factory, wo, e.User, map[uuid.UUID]float64{box: 1100}, e.Now)
	wantErr(t, err, "庫存只有")
}

// TC-09 發料 60 個：庫存 1000 → 940，製令已發 60
func TestTC09_IssueUpdatesStock(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	box := e.materialID(t, wo, e.Box)
	if _, err := e.svc.IssueMaterials(e.ctx, e.Factory, wo, e.User, map[uuid.UUID]float64{box: 60}, e.Now); err != nil {
		t.Fatal(err)
	}
	wantEq(t, "鞋盒庫存", e.Stock(t, e.WhRaw, e.Box), 940.0)
	wantEq(t, "製令已發", e.QueryFloat(t, `SELECT issued_qty::float8 FROM work_order_materials WHERE id = $1`, box), 60.0)
}

// TC-10 到站扣帳的料（大底）不會被手動發料扣掉（避免重複扣）
func TestTC10_OnArrivalNotIssued(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	sole := e.materialID(t, wo, e.Sole)
	res, err := e.svc.IssueMaterials(e.ctx, e.Factory, wo, e.User, map[uuid.UUID]float64{sole: 100}, e.Now)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "大底庫存不變", e.Stock(t, e.WhRaw, e.Sole), 1000.0)
	wantEq(t, "回傳列為到站扣帳", len(res.OnArrival), 1)
}

// TC-11 強制發料：整理站的鞋盒沒發完 → 整理站不能報工、不能上工；成型站不受影響；發完後整理站可以報
func TestTC11_RequireIssue(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{RequireIssue: true})
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 100) // 成型站用的大底是到站扣帳，不受管制
	_, err := e.scan(card, e.LineFinA, 100)
	wantErr(t, err, "強制發料")
	_, err = e.svc.StartLabor(e.ctx, e.Factory, e.LineFinA, wo, 6, e.User, e.Now)
	wantErr(t, err, "強制發料")
	if _, err := e.svc.IssueMaterials(e.ctx, e.Factory, wo, e.User, nil, e.Now); err != nil {
		t.Fatal(err)
	}
	e.mustScan(t, card, e.LineFinA, 100)
}

// ───── 上工／下工（REQ-LABOR）─────

// TC-12 同一條線上工第二張製令，第一張自動下工
func TestTC12_OneLineOneOrder(t *testing.T) {
	e := setup(t)
	a, b := e.newWO(t, 100, Control{}), e.newWO(t, 100, Control{})
	if _, err := e.svc.StartLabor(e.ctx, e.Factory, e.LineFormA, a, 10, e.User, e.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.StartLabor(e.ctx, e.Factory, e.LineFormA, b, 10, e.User, e.Now.Add(30*60e9)); err != nil {
		t.Fatal(err)
	}
	wantEq(t, "成型 A 線上工中筆數", e.openLabor(t, e.LineFormA), 1.0)
	wantEq(t, "上工中的是第二張", e.QueryString(t, `SELECT work_order_id::text FROM line_labor_sessions WHERE line_id = $1 AND ended_at IS NULL`, e.LineFormA), b.String())
}

// TC-13 強制上工：沒上工不能報；上工後可以報
func TestTC13_RequireLabor(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{RequireLabor: true})
	card := e.cards(t, wo)[0]
	_, err := e.scan(card, e.LineFormA, 10)
	wantErr(t, err, "上工")
	if _, err := e.svc.StartLabor(e.ctx, e.Factory, e.LineFormA, wo, 10, e.User, e.Now); err != nil {
		t.Fatal(err)
	}
	e.mustScan(t, card, e.LineFormA, 10)
}

// 上工時間設在報工前 1 小時：同一時刻上工又下工不算工時，系統會略過（見 TC-14～16 的測試修正紀錄）

// TC-14 成型站報完（100/100）→ 成型 A 線自動下工；整理 A 線還沒報完 → 繼續上工
func TestTC14_AutoEndWhenStationDone(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	for _, l := range []struct {
		line uuid.UUID
		hc   int
	}{{e.LineFormA, 10}, {e.LineFinA, 6}} {
		if _, err := e.svc.StartLabor(e.ctx, e.Factory, l.line, wo, l.hc, e.User, e.Now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	card := e.cards(t, wo)[0]
	r := e.mustScan(t, card, e.LineFormA, 99)
	wantEq(t, "報 99 雙還沒下工", r.LaborEnded, false)
	r = e.mustScan(t, card, e.LineFormA, 1)
	wantEq(t, "報滿 100 雙自動下工", r.LaborEnded, true)
	wantEq(t, "成型 A 線", e.openLabor(t, e.LineFormA), 0.0)
	wantEq(t, "整理 A 線仍上工", e.openLabor(t, e.LineFinA), 1.0)
}

// TC-15 製令完工時所有線別同步下工（成型 B 線上工中但沒報工，也要下工）
func TestTC15_EndAllOnComplete(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	for _, line := range []uuid.UUID{e.LineFormB, e.LineFinA} {
		if _, err := e.svc.StartLabor(e.ctx, e.Factory, line, wo, 6, e.User, e.Now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	e.finishAll(t, wo)
	wantEq(t, "製令狀態", e.woStatus(t, wo), "COMPLETED")
	wantEq(t, "成型 B 線", e.openLabor(t, e.LineFormB), 0.0)
	wantEq(t, "整理 A 線", e.openLabor(t, e.LineFinA), 0.0)
}

// TC-16 結案時所有線別同步下工
func TestTC16_EndAllOnClose(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 98, DefectInput{DefectCodeID: e.DefectForm, Qty: 2, Disposition: "SCRAP"})
	for _, line := range []uuid.UUID{e.LineFormA, e.LineFinA} {
		if _, err := e.svc.StartLabor(e.ctx, e.Factory, line, wo, 6, e.User, e.Now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	ended, err := e.svc.CloseWorkOrder(e.ctx, e.Factory, wo, e.User, "報廢短少", e.Now)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "結案下工線數", len(ended), 2)
	wantEq(t, "製令狀態", e.woStatus(t, wo), "CLOSED")
}
