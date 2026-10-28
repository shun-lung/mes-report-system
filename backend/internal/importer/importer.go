// Package importer 以 Excel 匯入基本資料：下載範本 → 上傳預覽（逐列標示新增／更新／無變更／錯誤，不寫入）→ 確認後單一交易寫入。
// 依代碼比對：已存在則更新、不存在則新增；不刪除既有資料。空白的選填欄位在更新時沿用原值。
package importer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mes/internal/database"
)

type Column struct {
	Key      string `json:"key"`
	Header   string `json:"header"`
	Required bool   `json:"required"`
	Help     string `json:"help"`
}

// Sheet 定義一種可匯入的資料。
type Sheet struct {
	Entity   string                                                                                        `json:"entity"`
	Title    string                                                                                        `json:"title"`
	Note     string                                                                                        `json:"note"`
	Columns  []Column                                                                                      `json:"columns"`
	Examples [][]string                                                                                    `json:"-"`
	Plan     func(ctx context.Context, q database.Querier, factoryID uuid.UUID, rows []Row) (*Plan, error) `json:"-"`
}

// Row 為 Excel 的一列資料（Num 為 Excel 列號，方便對照）。
type Row struct {
	Num    int
	values map[string]string
}

func (r Row) get(key string) string { return strings.TrimSpace(r.values[key]) }

const (
	ActCreate = "create"
	ActUpdate = "update"
	ActSame   = "same"
	ActError  = "error"
)

type Result struct {
	Row     int    `json:"row"`
	Key     string `json:"key"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

type Plan struct {
	Results []Result
	Apply   func(ctx context.Context, tx pgx.Tx) error
}

func (p *Plan) add(r Row, key, action, msg string) {
	p.Results = append(p.Results, Result{Row: r.Num, Key: key, Action: action, Message: msg})
}

func (p *Plan) errors() int {
	n := 0
	for _, r := range p.Results {
		if r.Action == ActError {
			n++
		}
	}
	return n
}

// Summary 各動作的筆數。
func (p *Plan) Summary() map[string]int {
	s := map[string]int{ActCreate: 0, ActUpdate: 0, ActSame: 0, ActError: 0}
	for _, r := range p.Results {
		s[r.Action]++
	}
	return s
}

var Sheets = []*Sheet{productSheet, stationSheet, defectSheet, bomSheet, routingSheet}

// 處理函式會引用各自的 Sheet（取欄位名稱），在 init 才接上以避免初始化循環。
func init() {
	productSheet.Plan = planProducts
	stationSheet.Plan = planStations
	defectSheet.Plan = planDefects
	bomSheet.Plan = planBOM
	routingSheet.Plan = planRoutings
}

func Find(entity string) *Sheet {
	for _, s := range Sheets {
		if s.Entity == entity {
			return s
		}
	}
	return nil
}

// errs 收集一列的錯誤訊息。
type errs []string

func (e *errs) add(format string, a ...any) { *e = append(*e, fmt.Sprintf(format, a...)) }
func (e errs) msg() string                  { return strings.Join(e, "；") }

func require(r Row, e *errs, sheet *Sheet, keys ...string) {
	for _, k := range keys {
		if r.get(k) == "" {
			e.add("「%s」必填", headerOf(sheet, k))
		}
	}
}

func headerOf(s *Sheet, key string) string {
	for _, c := range s.Columns {
		if c.Key == key {
			return c.Header
		}
	}
	return key
}

// 數值：允許千分位逗號；空白回傳 nil。
func parseNum(r Row, key, header string, e *errs, min float64) *float64 {
	v := strings.ReplaceAll(r.get(key), ",", "")
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		e.add("「%s」不是數字：%s", header, v)
		return nil
	}
	if f < min {
		e.add("「%s」不可小於 %g", header, min)
		return nil
	}
	return &f
}

func parseBool(r Row, key, header string, e *errs) *bool {
	switch strings.ToUpper(r.get(key)) {
	case "":
		return nil
	case "是", "Y", "YES", "TRUE", "1", "使用中", "啟用", "V":
		t := true
		return &t
	case "否", "N", "NO", "FALSE", "0", "停用":
		f := false
		return &f
	default:
		e.add("「%s」請填 是 或 否", header)
		return nil
	}
}

// 代碼對應表：允許中文名稱或系統代碼。
func parseEnum(r Row, key, header string, e *errs, options map[string]string) *string {
	v := r.get(key)
	if v == "" {
		return nil
	}
	if code, ok := options[strings.ToUpper(v)]; ok {
		return &code
	}
	e.add("「%s」的值「%s」無法辨識", header, v)
	return nil
}

var categories = map[string]string{"成品": "FG", "半成品": "SFG", "基本材料": "RAW", "材料": "RAW", "輔助料件": "AUX", "輔料": "AUX",
	"FG": "FG", "SFG": "SFG", "RAW": "RAW", "AUX": "AUX"}
var timings = map[string]string{"到站扣帳": "ON_ARRIVAL", "到站": "ON_ARRIVAL", "出庫扣帳": "ON_ISSUE", "出庫": "ON_ISSUE",
	"ON_ARRIVAL": "ON_ARRIVAL", "ON_ISSUE": "ON_ISSUE"}
var materialTypes = map[string]string{"基本材料": "MAIN", "主料": "MAIN", "輔助料件": "AUX", "輔料": "AUX", "MAIN": "MAIN", "AUX": "AUX"}

func boolText(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

func trimNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
