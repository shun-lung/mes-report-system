package production

import "testing"

// ───── 報工（REQ-SCAN）─────

// TC-17 等價分割「無效：0 雙」→ 拒絕
func TestTC17_ScanZero(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	_, err := e.scan(e.cards(t, wo)[0], e.LineFormA, 0)
	if err == nil {
		t.Fatal("報 0 雙竟然成功")
	}
}

// TC-18 邊界值：可報 100 雙，報 101 → 拒絕；報 100 → 成功
func TestTC18_ScanBoundary(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	if _, err := e.scan(card, e.LineFormA, 101); err == nil {
		t.Fatal("報 101 雙（超過可報 100）竟然成功")
	}
	r := e.mustScan(t, card, e.LineFormA, 100)
	wantEq(t, "剩餘可報", r.Remaining, 0.0)
}

// TC-19 不能跳站：成型還沒報，整理就報 → 拒絕
func TestTC19_NoSkipStation(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	if _, err := e.scan(e.cards(t, wo)[0], e.LineFinA, 10); err == nil {
		t.Fatal("成型未報，整理竟然可以報")
	}
}

// TC-20 成型報良品 60 → 整理可報 60，報 61 被擋
func TestTC20_FlowToNextStation(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 60)
	if _, err := e.scan(card, e.LineFinA, 61); err == nil {
		t.Fatal("整理報 61 雙（上一站只流入 60）竟然成功")
	}
	e.mustScan(t, card, e.LineFinA, 60)
}

// TC-21 重工：良品 95＋重工 5 → 本站還能報 5 雙
func TestTC21_ReworkReturns(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	r := e.mustScan(t, e.cards(t, wo)[0], e.LineFormA, 95, DefectInput{DefectCodeID: e.DefectForm, Qty: 5, Disposition: "REWORK"})
	wantEq(t, "本站剩餘可報", r.Remaining, 5.0)
}

// TC-22 到站扣帳：成型良品 90＋報廢 10 → 大底扣 100（實際加工量）
func TestTC22_ConsumeOnArrival(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	e.mustScan(t, e.cards(t, wo)[0], e.LineFormA, 90, DefectInput{DefectCodeID: e.DefectForm, Qty: 10, Disposition: "SCRAP"})
	wantEq(t, "大底庫存", e.Stock(t, e.WhRaw, e.Sole), 900.0)
}

// TC-23 邊界值：末站報 99 → 還是生產中；報滿 100 → 完工
func TestTC23_CompleteAtLastUnit(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 100)
	r := e.mustScan(t, card, e.LineFinA, 99)
	wantEq(t, "報 99 雙完工？", r.WOCompleted, false)
	wantEq(t, "狀態", e.woStatus(t, wo), "IN_PROGRESS")
	r = e.mustScan(t, card, e.LineFinA, 1)
	wantEq(t, "報滿完工？", r.WOCompleted, true)
	wantEq(t, "狀態", e.woStatus(t, wo), "COMPLETED")
}

// TC-24 結案後不能再報工
func TestTC24_NoScanAfterClose(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 50)
	if _, err := e.svc.CloseWorkOrder(e.ctx, e.Factory, wo, e.User, "測試", e.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.scan(card, e.LineFormA, 10); err == nil {
		t.Fatal("結案後竟然還能報工")
	}
}

// ───── 撤銷報工（REQ-UNDO）─────

// TC-25 撤銷成型報工 60 雙：成型可報回到 100、大底退回 1000、整理沒有可報量
func TestTC25_UndoRestores(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	r := e.mustScan(t, card, e.LineFormA, 60)
	if _, err := e.svc.ReverseScan(e.ctx, e.Factory, r.ScanID, e.User, "報錯", true, e.Now); err != nil {
		t.Fatal(err)
	}
	wantEq(t, "大底庫存", e.Stock(t, e.WhRaw, e.Sole), 1000.0)
	wantEq(t, "成型剩餘可報", e.QueryFloat(t, `
		SELECT remaining_qty::float8 FROM flow_card_progress p JOIN flow_cards c ON c.id = p.card_id
		WHERE c.card_no = $1 AND p.step_seq = 10`, card), 100.0)
	if _, err := e.scan(card, e.LineFinA, 1); err == nil {
		t.Fatal("撤銷後整理站竟然還能報")
	}
}

// TC-26 整理已報這些量時，不能撤銷成型（要從後往前撤）
func TestTC26_UndoOrder(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	card := e.cards(t, wo)[0]
	r := e.mustScan(t, card, e.LineFormA, 60)
	e.mustScan(t, card, e.LineFinA, 60)
	if _, err := e.svc.ReverseScan(e.ctx, e.Factory, r.ScanID, e.User, "報錯", true, e.Now); err == nil {
		t.Fatal("下一站已報，竟然可以撤銷上一站")
	}
}

// TC-27 同一筆不能撤銷兩次
func TestTC27_UndoTwice(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	r := e.mustScan(t, e.cards(t, wo)[0], e.LineFormA, 60)
	if _, err := e.svc.ReverseScan(e.ctx, e.Factory, r.ScanID, e.User, "報錯", true, e.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ReverseScan(e.ctx, e.Factory, r.ScanID, e.User, "再撤", true, e.Now); err == nil {
		t.Fatal("同一筆竟然可以撤銷兩次")
	}
}
