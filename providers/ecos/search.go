package ecos

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerSearch(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "ecos_search_series",
		Description: "Find Bank of Korea (ECOS) statistics for ecos_get_series. With a query, searches table names in Korean or English " +
			"(e.g. 실업률, unemployment, 수출, housing) and returns stat_codes; named indicators that match come first. " +
			"With a stat_code, lists the table's items with their codes, dimension, unit, and the frequencies and date range of each; " +
			"the query then filters the first dimension, and items of the other dimensions are always listed.",
	}, c.searchSeries)
}

type searchSeriesInput struct {
	Query    string `json:"query,omitempty" jsonschema:"words to look for in table or item names, Korean or English"`
	StatCode string `json:"stat_code,omitempty" jsonschema:"list this table's items instead of searching tables"`
	hub.PageParams
}

type SearchResult struct {
	Tables   []Table      `json:"tables,omitempty"`
	StatCode string       `json:"stat_code,omitempty"`
	StatName string       `json:"stat_name,omitempty"`
	Items    []TableItem  `json:"items,omitempty"`
	PageInfo hub.PageInfo `json:"page_info"`
}

type Table struct {
	Indicator   string   `json:"indicator,omitempty" jsonschema:"a named indicator for ecos_get_series; stat_code is then not needed"`
	StatCode    string   `json:"stat_code,omitempty"`
	Name        string   `json:"name"`
	NameEn      string   `json:"name_en,omitempty"`
	Category    string   `json:"category,omitempty"`
	Frequencies []string `json:"frequencies,omitempty"`
}

type TableItem struct {
	Dimension     int         `json:"dimension" jsonschema:"position in item_codes: 1 to 4"`
	DimensionName string      `json:"dimension_name,omitempty"`
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Parent        string      `json:"parent,omitempty"`
	Unit          string      `json:"unit,omitempty"`
	Available     []Available `json:"available"`
}

type Available struct {
	Frequency string `json:"frequency"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func (c *client) searchSeries(ctx context.Context, in searchSeriesInput) (SearchResult, error) {
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return SearchResult{}, err
	}
	tokens := tokenize(in.Query)
	if in.StatCode != "" {
		return c.listItems(ctx, strings.ToUpper(strings.TrimSpace(in.StatCode)), tokens, page, size)
	}
	if len(tokens) == 0 {
		return SearchResult{}, hub.Errorf(hub.CodeInvalidArgument, "query or stat_code is required")
	}
	var found []Table
	for _, ind := range indicators {
		if matches(tokens, ind.name, ind.label, ind.labelKo) {
			found = append(found, Table{Indicator: ind.name, Name: ind.labelKo, NameEn: ind.label, Frequencies: ind.freqs()})
		}
	}
	tables, err := c.tables.get(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	var byName, byCategory []Table
	for _, t := range tables {
		if !t.searchable {
			continue
		}
		out := Table{StatCode: t.code, Name: t.name, NameEn: t.nameEn, Category: t.category}
		if f, ok := freqNames[freq(t.cycle)]; ok {
			out.Frequencies = []string{f}
		}
		switch {
		case matches(tokens, t.name, t.nameEn):
			byName = append(byName, out)
		case matches(tokens, t.name, t.nameEn, t.category, t.categoryEn):
			byCategory = append(byCategory, out)
		}
	}
	found = append(append(found, byName...), byCategory...)
	return SearchResult{Tables: pageOf(found, page, size), PageInfo: hub.NewPageInfo(page, size, len(found))}, nil
}

type itemRow struct {
	StatName  string  `json:"STAT_NAME"`
	GrpCode   string  `json:"GRP_CODE"`
	GrpName   string  `json:"GRP_NAME"`
	ItemCode  string  `json:"ITEM_CODE"`
	ItemName  string  `json:"ITEM_NAME"`
	PItemName *string `json:"P_ITEM_NAME"`
	Cycle     string  `json:"CYCLE"`
	Start     string  `json:"START_TIME"`
	End       string  `json:"END_TIME"`
	Unit      *string `json:"UNIT_NAME"`
}

func (c *client) listItems(ctx context.Context, stat string, tokens []string, page, size int) (SearchResult, error) {
	var rows []itemRow
	if _, err := c.get(ctx, "StatisticItemList", "kr", maxRows, []string{stat}, &rows); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Message, he.Hint = "unknown stat_code "+stat, "find table codes with ecos_search_series and a query"
		}
		return SearchResult{}, err
	}
	out := SearchResult{StatCode: stat}
	var items []TableItem
	index := map[string]int{}
	for _, r := range rows {
		out.StatName = numbering.ReplaceAllString(strings.TrimSpace(r.StatName), "")
		// The query picks items of the first dimension only; the others
		// (평균자료/말일자료, 원계열/계절조정) are always listed, since
		// leaving them out of item_codes mixes them into one result.
		if dimension(r.GrpCode) == 1 && !matches(tokens, r.ItemName, deref(r.PItemName)) {
			continue
		}
		key := r.GrpCode + "/" + r.ItemCode
		i, ok := index[key]
		if !ok {
			i = len(items)
			index[key] = i
			unit, _ := convertUnit(deref(r.Unit))
			items = append(items, TableItem{
				Dimension: dimension(r.GrpCode), DimensionName: r.GrpName, Code: r.ItemCode,
				Name: strings.TrimSpace(r.ItemName), Parent: strings.TrimSpace(deref(r.PItemName)), Unit: unit,
			})
		}
		f := freq(r.Cycle)
		if _, ok := freqNames[f]; ok {
			items[i].Available = append(items[i].Available, Available{Frequency: f.String(), From: outPeriod(r.Start, f), To: outPeriod(r.End, f)})
		}
	}
	out.Items = pageOf(items, page, size)
	out.PageInfo = hub.NewPageInfo(page, size, len(items))
	return out, nil
}

// dimension turns "Group1" into 1.
func dimension(grp string) int {
	if n := len(grp); n > 0 && grp[n-1] >= '1' && grp[n-1] <= '4' {
		return int(grp[n-1] - '0')
	}
	return 1
}

func pageOf[T any](s []T, page, size int) []T {
	start := (page - 1) * size
	if start >= len(s) {
		return []T{}
	}
	return s[start:min(start+size, len(s))]
}

func tokenize(q string) []string {
	return strings.Fields(strings.ToLower(q))
}

// matches reports whether every token appears in one of the texts, ignoring
// case and spaces.
func matches(tokens []string, texts ...string) bool {
	joined := strings.ToLower(strings.ReplaceAll(strings.Join(texts, "|"), " ", ""))
	for _, t := range tokens {
		if !strings.Contains(joined, t) {
			return false
		}
	}
	return true
}

// ---- table index ----

// table is one entry of ECOS's table list (StatisticTableList), in Korean and
// English. Category entries (searchable false) group the tables below them.
type table struct {
	code, parent         string
	name, nameEn         string
	category, categoryEn string
	cycle                string
	searchable           bool
}

type tableRow struct {
	Parent string  `json:"P_STAT_CODE"`
	Code   string  `json:"STAT_CODE"`
	Name   string  `json:"STAT_NAME"`
	Cycle  *string `json:"CYCLE"`
	Srch   string  `json:"SRCH_YN"`
}

// numbering is the outline number ECOS puts before names: "1.3.1. ".
var numbering = regexp.MustCompile(`^[\d.]+\s*`)

func (c *client) downloadTables(ctx context.Context) ([]table, error) {
	var ko, en []tableRow
	if _, err := c.get(ctx, "StatisticTableList", "kr", maxRows, nil, &ko); err != nil {
		return nil, err
	}
	if _, err := c.get(ctx, "StatisticTableList", "en", maxRows, nil, &en); err != nil {
		return nil, err
	}
	enName := map[string]string{}
	for _, r := range en {
		enName[r.Code] = numbering.ReplaceAllString(strings.TrimSpace(r.Name), "")
	}
	tables := make([]table, 0, len(ko))
	byCode := map[string]int{}
	for _, r := range ko {
		byCode[r.Code] = len(tables)
		tables = append(tables, table{
			code: r.Code, parent: r.Parent, cycle: deref(r.Cycle), searchable: r.Srch == "Y",
			name: numbering.ReplaceAllString(strings.TrimSpace(r.Name), ""), nameEn: enName[r.Code],
		})
	}
	// Category is the nearest category above the table, e.g. 시장금리.
	for i := range tables {
		if p, ok := byCode[tables[i].parent]; ok && !tables[p].searchable {
			tables[i].category, tables[i].categoryEn = tables[p].name, tables[p].nameEn
		}
	}
	return tables, nil
}

// tableIndex caches the table list, which changes rarely.
type tableIndex struct {
	mu       sync.Mutex
	tables   []table
	loadedAt time.Time
	ttl      time.Duration
	load     func(context.Context) ([]table, error)
}

func newTableIndex(load func(context.Context) ([]table, error), ttl time.Duration) *tableIndex {
	return &tableIndex{load: load, ttl: ttl}
}

func (ix *tableIndex) get(ctx context.Context) ([]table, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.tables != nil && time.Since(ix.loadedAt) < ix.ttl {
		return ix.tables, nil
	}
	tables, err := ix.load(ctx)
	if err != nil {
		if ix.tables != nil {
			return ix.tables, nil // serve stale data rather than fail
		}
		return nil, err
	}
	ix.tables, ix.loadedAt = tables, time.Now()
	return tables, nil
}

func (ix *tableIndex) exists(ctx context.Context, code string) (bool, error) {
	tables, err := ix.get(ctx)
	if err != nil {
		return true, err // unknown; do not claim the code is wrong
	}
	return slices.ContainsFunc(tables, func(t table) bool { return t.code == code && t.searchable }), nil
}
