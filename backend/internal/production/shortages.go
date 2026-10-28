package production

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/database"
	"mes/internal/httpx"
	"mes/internal/purchasing"
)

// 即時缺料清單：彙總所有已開立製令的未扣料需求，對照庫存與在途（半成品製令、採購單），算出淨缺口。
// 與開立時的單次預警不同，這份清單隨時反映現況（含現場實際扣料後的變化）。

type Shortage struct {
	ProductID   uuid.UUID       `json:"productId"`
	ItemNo      string          `json:"itemNo"`
	ItemName    string          `json:"itemName"`
	Unit        string          `json:"unit"`
	Category    string          `json:"category"`
	AlertPct    float64         `json:"alertPct"`
	Need        float64         `json:"need"`        // 已開立製令未扣料需求
	OnHand      float64         `json:"onHand"`      // 全廠庫存
	InboundWO   float64         `json:"inboundWo"`   // 在途半成品製令
	InboundPO   float64         `json:"inboundPo"`   // 在途採購（含草稿）
	Purchasable bool            `json:"purchasable"` // 無途程＝外購
	CardQty     int             `json:"cardQty"`
	Supplier    *string         `json:"supplier"`
	WorkOrders  json.RawMessage `json:"workOrders"`
	Shortage    float64         `json:"shortage"` // 需求×(1+緩衝%) − 庫存 − 在途；>0 表示需要處理
	Status      string          `json:"status"`   // OK 庫存足夠 / COVERED 在途可補足 / SHORT 缺料
}

func loadShortages(ctx context.Context, q database.Querier, factoryID uuid.UUID, productIDs []uuid.UUID) ([]Shortage, error) {
	rows, err := q.Query(ctx, `
		WITH need AS (
			SELECT m.product_id, sum(m.required_qty - m.issued_qty) AS need,
			       json_agg(json_build_object('woNo', w.wo_no, 'status', w.status, 'qty', m.required_qty - m.issued_qty,
			                                  'dueDate', w.due_date) ORDER BY w.due_date NULLS LAST, w.wo_no) AS wos
			FROM work_order_materials m JOIN work_orders w ON w.id = m.work_order_id
			WHERE w.factory_id = $1 AND w.status IN ('RELEASED','SCHEDULED','IN_PROGRESS') AND m.required_qty > m.issued_qty
			  AND ($2::uuid[] IS NULL OR m.product_id = ANY ($2))
			GROUP BY m.product_id)
		SELECT p.id, p.item_no, p.name, p.unit, p.category, p.alert_pct, n.need,
		       COALESCE((SELECT sum(b.qty) FROM inventory_balances b JOIN warehouses wh ON wh.id = b.warehouse_id
		                 WHERE b.product_id = p.id AND wh.factory_id = $1), 0),
		       COALESCE((SELECT sum(w.planned_qty - COALESCE((
		                   SELECT sum(fp.good_qty) FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
		                   WHERE c.work_order_id = w.id
		                     AND fp.step_seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = w.routing_id)), 0))
		                 FROM work_orders w WHERE w.product_id = p.id AND w.factory_id = $1
		                   AND w.status IN ('DRAFT','RELEASED','SCHEDULED','IN_PROGRESS')), 0),
		       COALESCE((SELECT sum(l.qty - l.received_qty) FROM purchase_order_lines l JOIN purchase_orders po ON po.id = l.po_id
		                 WHERE l.product_id = p.id AND po.factory_id = $1 AND po.status IN ('DRAFT','ORDERED','PARTIAL')), 0),
		       NOT EXISTS (SELECT 1 FROM routings r WHERE r.product_id = p.id AND r.is_active),
		       p.card_qty, s.name, n.wos
		FROM need n JOIN products p ON p.id = n.product_id
		LEFT JOIN suppliers s ON s.id = p.default_supplier_id`, factoryID, productIDs)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Shortage, error) {
		var s Shortage
		err := r.Scan(&s.ProductID, &s.ItemNo, &s.ItemName, &s.Unit, &s.Category, &s.AlertPct, &s.Need, &s.OnHand,
			&s.InboundWO, &s.InboundPO, &s.Purchasable, &s.CardQty, &s.Supplier, &s.WorkOrders)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	for i := range list {
		s := &list[i]
		s.Shortage = round4(s.Need*(1+s.AlertPct/100) - s.OnHand - s.InboundWO - s.InboundPO)
		switch {
		case s.OnHand >= s.Need*(1+s.AlertPct/100):
			s.Status = "OK"
		case s.Shortage <= 0:
			s.Status = "COVERED"
		default:
			s.Status = "SHORT"
		}
	}
	rank := map[string]int{"SHORT": 0, "COVERED": 1, "OK": 2}
	sort.SliceStable(list, func(i, j int) bool {
		if rank[list[i].Status] != rank[list[j].Status] {
			return rank[list[i].Status] < rank[list[j].Status]
		}
		return list[i].ItemNo < list[j].ItemNo
	})
	return list, nil
}

func (h *Handler) listShortages(c *gin.Context) {
	list, err := loadShortages(c, h.svc.pool, auth.FactoryID(c), nil)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type purchaseShortagesRequest struct {
	ProductIDs []uuid.UUID `json:"productIds" binding:"required,min=1"`
}

type purchaseResult struct {
	ItemNo string  `json:"itemNo"`
	Qty    float64 `json:"qty"`
	PONo   string  `json:"poNo,omitempty"`
	Skip   string  `json:"skip,omitempty"`
}

// purchaseShortages 將勾選的外購料件依目前缺口加入採購單草稿（同供應商合併）。
func (h *Handler) purchaseShortages(c *gin.Context) {
	var req purchaseShortagesRequest
	if !httpx.Bind(c, &req) {
		return
	}
	factoryID, userID, at := auth.FactoryID(c), auth.UserID(c), time.Now()
	var results []purchaseResult
	err := pgx.BeginFunc(c, h.svc.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtext($1))`, "shortage:"+factoryID.String()); err != nil {
			return err
		}
		list, err := loadShortages(c, tx, factoryID, req.ProductIDs)
		if err != nil {
			return err
		}
		for _, s := range list {
			r := purchaseResult{ItemNo: s.ItemNo}
			switch {
			case !s.Purchasable:
				r.Skip = "自製件，請開製令"
			case s.Shortage <= 0:
				r.Skip = "目前不缺（含在途）"
			default:
				r.Qty = math.Ceil(s.Shortage)
				if _, r.PONo, err = purchasing.AddShortage(c, tx, factoryID, s.ProductID, r.Qty, nil, userID, at); err != nil {
					return err
				}
			}
			results = append(results, r)
		}
		return nil
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, results)
}

type shortageWORequest struct {
	ProductID uuid.UUID `json:"productId" binding:"required"`
}

// workOrderForShortage 依自製件目前缺口建立草稿製令（數量取整到每卡數量）。
func (h *Handler) workOrderForShortage(c *gin.Context) {
	var req shortageWORequest
	if !httpx.Bind(c, &req) {
		return
	}
	factoryID := auth.FactoryID(c)
	var id uuid.UUID
	var no string
	var qty float64
	err := pgx.BeginFunc(c, h.svc.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtext($1))`, "shortage:"+factoryID.String()); err != nil {
			return err
		}
		list, err := loadShortages(c, tx, factoryID, []uuid.UUID{req.ProductID})
		if err != nil {
			return err
		}
		if len(list) == 0 || list[0].Shortage <= 0 {
			return httpx.Biz("目前不缺料（含在途），不需開製令")
		}
		s := list[0]
		if s.Purchasable {
			return httpx.Biz("%s 沒有途程，請改用採購", s.ItemNo)
		}
		qty = math.Ceil(s.Shortage/float64(s.CardQty)) * float64(s.CardQty)
		id, no, err = createWorkOrder(c, tx, NewWorkOrder{
			FactoryID: factoryID, ProductID: s.ProductID, Qty: qty, Source: "SHORTAGE", UserID: auth.UserID(c), At: time.Now(),
		})
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "woNo": no, "qty": qty})
}
