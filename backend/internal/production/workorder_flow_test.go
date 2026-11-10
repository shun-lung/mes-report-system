package production

import (
	"testing"
	"time"
)

// ───── 製令與訂單（REQ-WO）─────

// TC-01 開立 250 雙產生 3 張流程卡：100、100、50（邊界：尾數卡＝餘數）
func TestTC01_ReleaseCards(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 250, Control{})
	cards := e.cards(t, wo)
	wantEq(t, "流程卡張數", len(cards), 3)
	wantEq(t, "最後一張卡數量", e.QueryFloat(t, `SELECT qty::float8 FROM flow_cards WHERE card_no = $1`, cards[2]), 50.0)
	wantEq(t, "製令狀態", e.woStatus(t, wo), "RELEASED")
}

// TC-02 用料需求＝單位用量 × 計畫數
func TestTC02_MaterialRequirement(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 250, Control{})
	wantEq(t, "大底需求", e.QueryFloat(t, `SELECT required_qty::float8 FROM work_order_materials WHERE id = $1`, e.materialID(t, wo, e.Sole)), 250.0)
	wantEq(t, "鞋盒需求", e.QueryFloat(t, `SELECT required_qty::float8 FROM work_order_materials WHERE id = $1`, e.materialID(t, wo, e.Box)), 250.0)
}

// TC-03 草稿改數量 100 → 200，用料需求等比例加倍
func TestTC03_UpdateDraft(t *testing.T) {
	e := setup(t)
	wo, _, err := e.svc.CreateWorkOrder(e.ctx, e.Factory, e.Product, 100, nil, Control{}, e.User, e.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UpdateDraft(e.ctx, e.Factory, wo, 200, nil, Control{}, e.Now); err != nil {
		t.Fatal(err)
	}
	wantEq(t, "大底需求", e.QueryFloat(t, `SELECT required_qty::float8 FROM work_order_materials WHERE id = $1`, e.materialID(t, wo, e.Sole)), 200.0)
}

// TC-04 已開立的製令不能改數量
func TestTC04_UpdateReleasedRejected(t *testing.T) {
	e := setup(t)
	wo := e.newWO(t, 100, Control{})
	wantErr(t, e.svc.UpdateDraft(e.ctx, e.Factory, wo, 200, nil, Control{}, e.Now), "只有草稿")
}

// TC-05 已有報工的製令不能取消；沒有報工的可以取消
func TestTC05_CancelRules(t *testing.T) {
	e := setup(t)
	a := e.newWO(t, 100, Control{})
	e.mustScan(t, e.cards(t, a)[0], e.LineFormA, 10)
	if err := e.svc.CancelWorkOrder(e.ctx, e.Factory, a, e.User, "測試", e.Now); err == nil {
		t.Fatal("有報工的製令竟然可以取消")
	}
	b := e.newWO(t, 100, Control{})
	if err := e.svc.CancelWorkOrder(e.ctx, e.Factory, b, e.User, "測試", e.Now); err != nil {
		t.Fatalf("沒有報工的製令應可取消：%v", err)
	}
	wantEq(t, "取消後狀態", e.woStatus(t, b), "CANCELLED")
}

// TC-06 訂單狀態：未生產 → 生產中 → 已完成（狀態轉換）
func TestTC06_SalesOrderStatus(t *testing.T) {
	e := setup(t)
	so, _, err := e.svc.CreateSalesOrder(e.ctx, e.Factory, SalesOrderInput{
		CustomerID: e.Customer,
		Lines:      []SalesOrderLineInput{{ProductID: e.Product, Qty: 100, DueDate: e.Now.AddDate(0, 0, 14).Format(time.DateOnly)}},
	}, e.User, e.Now)
	if err != nil {
		t.Fatal(err)
	}
	status := func() string { return e.QueryString(t, `SELECT status FROM sales_orders WHERE id = $1`, so) }
	wantEq(t, "建立後", status(), "OPEN")
	list, err := e.svc.ConvertSalesOrder(e.ctx, e.Factory, so, Control{}, e.User, e.Now)
	if err != nil {
		t.Fatal(err)
	}
	wo := list[0].ID
	if _, err := e.svc.Release(e.ctx, e.Factory, wo, e.User, e.Now); err != nil {
		t.Fatal(err)
	}
	e.mustScan(t, e.cards(t, wo)[0], e.LineFormA, 10)
	wantEq(t, "開始報工後", status(), "IN_PRODUCTION")
	card := e.cards(t, wo)[0]
	e.mustScan(t, card, e.LineFormA, 90)
	e.mustScan(t, card, e.LineFinA, 100)
	wantEq(t, "製令完工後", status(), "COMPLETED")
}
