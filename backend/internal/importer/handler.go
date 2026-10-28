package importer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"mes/internal/auth"
	"mes/internal/httpx"
)

const (
	maxFileSize = 5 << 20
	maxRows     = 2000
	dataSheet   = "資料"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) Register(scoped *gin.RouterGroup) {
	g := scoped.Group("/import", auth.Require("masterdata:import"))
	g.GET("/sheets", h.sheets)
	g.GET("/:entity/template", h.template)
	g.POST("/:entity/preview", h.preview)
	g.POST("/:entity/commit", h.commit)
}

func (h *Handler) sheets(c *gin.Context) {
	c.JSON(http.StatusOK, Sheets)
}

func sheetOf(c *gin.Context) (*Sheet, bool) {
	s := Find(c.Param("entity"))
	if s == nil {
		httpx.Abort(c, http.StatusNotFound, "不支援的匯入類型")
		return nil, false
	}
	return s, true
}

// template 產生範本：「資料」工作表（標題列＋範例）與「說明」工作表。
func (h *Handler) template(c *gin.Context) {
	s, ok := sheetOf(c)
	if !ok {
		return
	}
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName("Sheet1", dataSheet)
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"DCE8F7"}, Pattern: 1}})
	req, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "B42C2B"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"DCE8F7"}, Pattern: 1}})
	for i, col := range s.Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		name := col.Header
		style := head
		if col.Required {
			name += "*"
			style = req
		}
		_ = f.SetCellStr(dataSheet, cell, name)
		_ = f.SetCellStyle(dataSheet, cell, cell, style)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(dataSheet, colName, colName, float64(max(12, len([]rune(col.Header))*2+4)))
	}
	for r, ex := range s.Examples {
		for i, v := range ex {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellStr(dataSheet, cell, v) // 一律存成文字，避免品號、條碼被 Excel 轉成數字
		}
	}
	_ = f.SetPanes(dataSheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	_, _ = f.NewSheet("說明")
	_ = f.SetCellStr("說明", "A1", s.Title+" 匯入說明")
	_ = f.SetCellStr("說明", "A2", s.Note)
	_ = f.SetCellStr("說明", "A3", "標 * 為必填；範例資料請刪除或改成自己的資料後再上傳。只讀取「資料」工作表。")
	for i, h := range []string{"欄位", "必填", "說明"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 5)
		_ = f.SetCellStr("說明", cell, h)
		_ = f.SetCellStyle("說明", cell, cell, head)
	}
	for i, col := range s.Columns {
		row := i + 6
		_ = f.SetCellStr("說明", "A"+itoa(row), col.Header)
		if col.Required {
			_ = f.SetCellStr("說明", "B"+itoa(row), "是")
		}
		_ = f.SetCellStr("說明", "C"+itoa(row), col.Help)
	}
	_ = f.SetColWidth("說明", "A", "A", 22)
	_ = f.SetColWidth("說明", "C", "C", 70)
	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		httpx.Abort(c, http.StatusInternalServerError, "產生範本失敗")
		return
	}
	name := s.Title + "_匯入範本.xlsx"
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// readRows 讀取上傳的 Excel：找到標題列、對應欄位、轉成資料列。
func readRows(c *gin.Context, s *Sheet) ([]Row, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFileSize+64<<10)
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, httpx.Biz("請選擇檔案（5 MB 以內）")
	}
	if strings.ToLower(filepath.Ext(fh.Filename)) != ".xlsx" {
		return nil, httpx.Biz("只支援 .xlsx 檔（Excel 2007 以後格式）；若是 .xls 請另存新檔為 .xlsx")
	}
	src, err := fh.Open()
	if err != nil {
		return nil, httpx.Biz("讀取檔案失敗")
	}
	defer src.Close()
	f, err := excelize.OpenReader(src)
	if err != nil {
		return nil, httpx.Biz("無法開啟 Excel 檔，請確認檔案沒有損壞或加密")
	}
	defer f.Close()

	name := dataSheet
	if idx, err := f.GetSheetIndex(dataSheet); err != nil || idx < 0 {
		name = f.GetSheetList()[0]
	}
	all, err := f.GetRows(name)
	if err != nil {
		return nil, httpx.Biz("讀取工作表失敗")
	}

	headerAt := -1
	for i, r := range all {
		if strings.TrimSpace(strings.Join(r, "")) != "" {
			headerAt = i
			break
		}
	}
	if headerAt < 0 {
		return nil, httpx.Biz("檔案是空的")
	}
	colKey := map[int]string{}
	found := map[string]bool{}
	for i, h := range all[headerAt] {
		n := normalizeHeader(h)
		for _, col := range s.Columns {
			if n == normalizeHeader(col.Header) {
				colKey[i] = col.Key
				found[col.Key] = true
			}
		}
	}
	var missing []string
	for _, col := range s.Columns {
		if col.Required && !found[col.Key] {
			missing = append(missing, col.Header)
		}
	}
	if len(missing) > 0 {
		return nil, httpx.Biz("找不到必要欄位：%s（請使用「%s」的範本）", strings.Join(missing, "、"), s.Title)
	}

	var rows []Row
	for i := headerAt + 1; i < len(all); i++ {
		if strings.TrimSpace(strings.Join(all[i], "")) == "" {
			continue
		}
		r := Row{Num: i + 1, values: map[string]string{}}
		for j, v := range all[i] {
			if k, ok := colKey[j]; ok {
				r.values[k] = v
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil, httpx.Biz("檔案沒有資料列")
	}
	if len(rows) > maxRows {
		return nil, httpx.Biz("一次最多匯入 %d 列，請分批上傳", maxRows)
	}
	return rows, nil
}

func normalizeHeader(h string) string {
	h = strings.NewReplacer("*", "", "＊", "", "（必填）", "", "(必填)", "", " ", "", "　", "").Replace(h)
	return strings.ToLower(strings.TrimSpace(h))
}

func (h *Handler) preview(c *gin.Context) {
	s, ok := sheetOf(c)
	if !ok {
		return
	}
	rows, err := readRows(c, s)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	plan, err := s.Plan(c, h.pool, auth.FactoryID(c), rows)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": plan.Results, "summary": plan.Summary()})
}

// commit 在單一交易內重新檢查並寫入；仍有任何錯誤則不寫入。
func (h *Handler) commit(c *gin.Context) {
	s, ok := sheetOf(c)
	if !ok {
		return
	}
	rows, err := readRows(c, s)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	factoryID := auth.FactoryID(c)
	var summary map[string]int
	err = pgx.BeginFunc(c, h.pool, func(tx pgx.Tx) error {
		// 同工廠同類型的匯入依序進行
		if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtext($1))`, "import:"+factoryID.String()+":"+s.Entity); err != nil {
			return err
		}
		plan, err := s.Plan(c, tx, factoryID, rows)
		if err != nil {
			return err
		}
		if n := plan.errors(); n > 0 {
			return httpx.Biz("仍有 %d 列錯誤，請修正後重新上傳", n)
		}
		if err := plan.Apply(c, tx); err != nil {
			return err
		}
		summary = plan.Summary()
		after, _ := json.Marshal(gin.H{"sheet": s.Title, "rows": len(rows), "summary": summary})
		_, err = tx.Exec(c, `
			INSERT INTO audit_logs (factory_id, user_id, action, entity, after) VALUES ($1, $2, 'IMPORT', $3, $4)`,
			factoryID, auth.UserID(c), s.Entity, after)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"summary": summary})
}
