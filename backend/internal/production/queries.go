package production

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/auth"
	"mes/internal/httpx"
)

type StepProgress struct {
	Seq     int     `json:"seq"`
	Station string  `json:"station"`
	Input   float64 `json:"input"`
	Good    float64 `json:"good"`
	Defect  float64 `json:"defect"`
}

type WorkOrderRow struct {
	ID          uuid.UUID      `json:"id"`
	WONo        string         `json:"woNo"`
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	ItemNo      string         `json:"itemNo"`
	ItemName    string         `json:"itemName"`
	Category    string         `json:"category"`
	PlannedQty  float64        `json:"plannedQty"`
	FinishedQty float64        `json:"finishedQty"`
	Cards       int            `json:"cards"`
	DueDate     *time.Time     `json:"dueDate"`
	SONo        *string        `json:"soNo"`
	Customer    *string        `json:"customer"`
	ParentWONo  *string        `json:"parentWoNo"`
	ReleasedAt  *time.Time     `json:"releasedAt"`
	CompletedAt *time.Time     `json:"completedAt"`
	Steps       []StepProgress `json:"steps"`
}

// WorkOrderSelect 查詢製令與各站進度，供製令列表與看板共用。
const WorkOrderSelect = `
	SELECT w.id, w.wo_no, w.status, w.source, p.item_no, p.name, p.category, w.planned_qty,
	       COALESCE((SELECT sum(fp.good_qty) FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
	                 WHERE c.work_order_id = w.id
	                   AND fp.step_seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = w.routing_id)), 0),
	       (SELECT count(*) FROM flow_cards c WHERE c.work_order_id = w.id)::int,
	       w.due_date, so.so_no, cu.name, pw.wo_no, w.released_at, w.completed_at,
	       COALESCE((SELECT json_agg(json_build_object('seq', rs.seq, 'station', st.name,
	                   'input', COALESCE(agg.input, 0), 'good', COALESCE(agg.good, 0), 'defect', COALESCE(agg.defect, 0))
	                   ORDER BY rs.seq)
	                 FROM routing_steps rs
	                 JOIN work_stations st ON st.id = rs.station_id
	                 LEFT JOIN (SELECT fp.step_seq, sum(fp.input_qty) input, sum(fp.good_qty) good, sum(fp.defect_qty) defect
	                            FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
	                            WHERE c.work_order_id = w.id GROUP BY fp.step_seq) agg ON agg.step_seq = rs.seq
	                 WHERE rs.routing_id = w.routing_id), '[]')
	FROM work_orders w
	JOIN products p ON p.id = w.product_id
	LEFT JOIN sales_order_lines sl ON sl.id = w.sales_order_line_id
	LEFT JOIN sales_orders so ON so.id = sl.sales_order_id
	LEFT JOIN customers cu ON cu.id = so.customer_id
	LEFT JOIN work_orders pw ON pw.id = w.parent_wo_id`

func (h *Handler) listWorkOrders(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, WorkOrderSelect+`
		WHERE w.factory_id = $1 ORDER BY w.created_at DESC LIMIT 300`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[WorkOrderRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type salesOrderRow struct {
	ID         uuid.UUID `json:"id"`
	SONo       string    `json:"soNo"`
	Status     string    `json:"status"`
	Customer   string    `json:"customer"`
	CustomerPO *string   `json:"customerPo"`
	OrderDate  time.Time `json:"orderDate"`
	Lines      []struct {
		LineNo      int        `json:"lineNo"`
		ItemNo      string     `json:"itemNo"`
		ItemName    string     `json:"itemName"`
		Qty         float64    `json:"qty"`
		DueDate     string     `json:"dueDate"`
		WONo        *string    `json:"woNo"`
		WOStatus    *string    `json:"woStatus"`
		FinishedQty float64    `json:"finishedQty"`
		WOID        *uuid.UUID `json:"woId"`
	} `json:"lines"`
}

func (h *Handler) listSalesOrders(c *gin.Context) {
	rows, err := h.svc.pool.Query(c, `
		SELECT so.id, so.so_no, so.status, cu.name, so.customer_po, so.order_date,
		       (SELECT json_agg(json_build_object(
		            'lineNo', l.line_no, 'itemNo', p.item_no, 'itemName', p.name, 'qty', l.qty, 'dueDate', l.due_date,
		            'woId', w.id, 'woNo', w.wo_no, 'woStatus', w.status,
		            'finishedQty', COALESCE((SELECT sum(fp.good_qty) FROM flow_card_progress fp JOIN flow_cards c ON c.id = fp.card_id
		                                     WHERE c.work_order_id = w.id
		                                       AND fp.step_seq = (SELECT max(seq) FROM routing_steps WHERE routing_id = w.routing_id)), 0))
		          ORDER BY l.line_no)
		        FROM sales_order_lines l
		        JOIN products p ON p.id = l.product_id
		        LEFT JOIN work_orders w ON w.sales_order_line_id = l.id AND w.status <> 'CANCELLED'
		        WHERE l.sales_order_id = so.id)
		FROM sales_orders so JOIN customers cu ON cu.id = so.customer_id
		WHERE so.factory_id = $1 ORDER BY so.created_at DESC LIMIT 200`, auth.FactoryID(c))
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[salesOrderRow])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

type printCard struct {
	CardNo   string     `json:"cardNo"`
	Qty      float64    `json:"qty"`
	Status   string     `json:"status"`
	Prints   int        `json:"printCount"`
	Printed  *time.Time `json:"lastPrintedAt"`
	ParentNo *string    `json:"parentCardNo"`
}

// cardsForPrint 製令與其流程卡（列印用）。
func (h *Handler) cardsForPrint(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var wo struct {
		WONo       string     `json:"woNo"`
		Status     string     `json:"status"`
		PlannedQty float64    `json:"plannedQty"`
		DueDate    *time.Time `json:"dueDate"`
		ItemNo     string     `json:"itemNo"`
		ItemName   string     `json:"itemName"`
		Spec       *string    `json:"spec"`
		Unit       string     `json:"unit"`
		ImageID    *uuid.UUID `json:"imageId"`
		SONo       *string    `json:"soNo"`
		Customer   *string    `json:"customer"`
		Factory    string     `json:"factory"`
		Routing    []string   `json:"routing"`
	}
	err := h.svc.pool.QueryRow(c, `
		SELECT w.wo_no, w.status, w.planned_qty, w.due_date, p.item_no, p.name, p.spec, p.unit,
		       (SELECT i.id FROM product_images i WHERE i.product_id = p.id ORDER BY i.is_primary DESC, i.sort_order LIMIT 1),
		       so.so_no, cu.name, f.name,
		       ARRAY(SELECT s.name FROM routing_steps rs JOIN work_stations s ON s.id = rs.station_id
		             WHERE rs.routing_id = w.routing_id ORDER BY rs.seq)
		FROM work_orders w
		JOIN products p ON p.id = w.product_id
		JOIN factories f ON f.id = w.factory_id
		LEFT JOIN sales_order_lines sl ON sl.id = w.sales_order_line_id
		LEFT JOIN sales_orders so ON so.id = sl.sales_order_id
		LEFT JOIN customers cu ON cu.id = so.customer_id
		WHERE w.id = $1 AND w.factory_id = $2`, id, auth.FactoryID(c)).
		Scan(&wo.WONo, &wo.Status, &wo.PlannedQty, &wo.DueDate, &wo.ItemNo, &wo.ItemName, &wo.Spec, &wo.Unit,
			&wo.ImageID, &wo.SONo, &wo.Customer, &wo.Factory, &wo.Routing)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	rows, err := h.svc.pool.Query(c, `
		SELECT fc.card_no, fc.qty, fc.status, fc.print_count, fc.last_printed_at, pc.card_no
		FROM flow_cards fc LEFT JOIN flow_cards pc ON pc.id = fc.parent_card_id
		WHERE fc.work_order_id = $1 AND fc.status NOT IN ('VOID','SPLIT')
		ORDER BY fc.card_no`, id)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	cards, err := pgx.CollectRows(rows, pgx.RowToStructByPos[printCard])
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"workOrder": wo, "cards": cards})
}

type markPrintedRequest struct {
	CardNos []string `json:"cardNos" binding:"required,min=1"`
}

// markPrinted 記錄流程卡已列印（次數與時間），補印時可辨識。
func (h *Handler) markPrinted(c *gin.Context) {
	id, ok := httpx.ParamID(c)
	if !ok {
		return
	}
	var req markPrintedRequest
	if !httpx.Bind(c, &req) {
		return
	}
	tag, err := h.svc.pool.Exec(c, `
		UPDATE flow_cards SET print_count = print_count + 1, last_printed_at = now()
		WHERE work_order_id = $1 AND factory_id = $2 AND card_no = ANY ($3)`, id, auth.FactoryID(c), req.CardNos)
	if err != nil {
		httpx.DBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": tag.RowsAffected()})
}
