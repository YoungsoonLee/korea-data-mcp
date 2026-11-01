package ecos

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testKey = "TESTKEY"

// fakeECOS serves testdata files. A route is "<service>/<lang>/<args…>": the
// path after the key and row range, matched as a prefix; the longest match
// wins.
type fakeECOS struct {
	t      *testing.T
	routes map[string]string
	mu     sync.Mutex
	last   map[string]string // service -> path after the key
}

func (f *fakeECOS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /<service>/<key>/json/<lang>/<first>/<last>/<args…>
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 6 || parts[1] != testKey || parts[2] != "json" || parts[4] != "1" {
		f.t.Errorf("bad path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	got := strings.Join(append([]string{parts[0], parts[3]}, parts[6:]...), "/")
	f.mu.Lock()
	f.last[parts[0]] = got
	f.mu.Unlock()
	best := ""
	for route := range f.routes {
		if strings.HasPrefix(got, route) && len(route) > len(best) {
			best = route
		}
	}
	if best == "" {
		f.t.Errorf("no route for %s", got)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	b, err := os.ReadFile("testdata/" + f.routes[best])
	if err != nil {
		f.t.Fatal(err)
	}
	w.Write(b)
}

func (f *fakeECOS) path(service string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[service]
}

var tableRoutes = map[string]string{
	"StatisticTableList/kr": "tables_kr.json",
	"StatisticTableList/en": "tables_en.json",
}

func setup(t *testing.T, routes map[string]string) (*fakeECOS, *mcp.ClientSession) {
	t.Helper()
	all := map[string]string{}
	for k, v := range tableRoutes {
		all[k] = v
	}
	for k, v := range routes {
		all[k] = v
	}
	f := &fakeECOS{t: t, routes: all, last: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	p := &Provider{BaseURL: srv.URL, now: func() time.Time { return time.Date(2026, 10, 31, 10, 0, 0, 0, kst) }}
	h := hub.New(&mcp.Implementation{Name: "test", Version: "0"}, slog.New(slog.DiscardHandler))
	h.Add(p, func(k string) string {
		if k == envKey {
			return testKey
		}
		return ""
	})
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := h.Server().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return f, cs
}

// call returns the tool error, if any, and decodes a successful result into out.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *hub.Error {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		var e hub.Error
		text := res.Content[0].(*mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			t.Fatalf("%s: error is not a hub.Error: %s", name, text)
		}
		if strings.Contains(text, testKey) {
			t.Errorf("%s: key leaked: %s", name, text)
		}
		return &e
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	return nil
}

func mustOK(t *testing.T, e *hub.Error) {
	t.Helper()
	if e != nil {
		t.Fatalf("unexpected error: %+v", e)
	}
}

func values(s Series) []float64 {
	var out []float64
	for _, o := range s.Observations {
		out = append(out, *o.Value)
	}
	return out
}

// The base rate was cut from 3.5% to 3.25% on 2024-10-11; the daily table
// repeats the rate every calendar day.
func TestBaseRateChangesOnly(t *testing.T) {
	f, cs := setup(t, map[string]string{"StatisticSearch/kr/722Y001/D": "base_rate_daily.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"indicator": "base_rate", "from": "2024-10-08", "to": "2024-10-15"}, &got))
	if p := f.path("StatisticSearch"); p != "StatisticSearch/kr/722Y001/D/20241008/20241015/0101000" {
		t.Errorf("path %s", p)
	}
	s := got.Series[0]
	if len(got.Series) != 1 || len(s.Observations) != 2 || s.Observations[1].Period != "2024-10-11" || *s.Observations[1].Value != 3.25 ||
		s.Unit != "% per year" || got.Note == "" || got.StatName == "" {
		t.Errorf("got %+v", got)
	}
}

// Asking 731Y004 for KRW/USD without the second dimension returns the
// monthly average and the month-end rate interleaved under the same months.
func TestMixedItemsAreSeparateSeries(t *testing.T) {
	_, cs := setup(t, map[string]string{"StatisticSearch/kr/731Y004/M": "usd_krw_monthly_mixed.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"stat_code": "731y004", "item_codes": []string{"0000001"}, "frequency": "monthly", "from": "2025-01", "to": "2025-04"}, &got))
	if len(got.Series) != 2 || got.Note == "" {
		t.Fatalf("got %+v", got)
	}
	avg, end := got.Series[0], got.Series[1]
	if avg.Items[1].Name != "평균자료" || end.Items[1].Name != "말일자료" || len(avg.Observations) != 4 || *avg.Observations[0].Value != 1455.79 || *end.Observations[0].Value != 1433.3 {
		t.Errorf("got %+v / %+v", avg, end)
	}
	if avg.Unit != "KRW" || avg.Observations[3].Period != "2025-04" {
		t.Errorf("unit %s period %s", avg.Unit, avg.Observations[3].Period)
	}
}

// The usd_krw indicator picks the monthly average explicitly.
func TestIndicatorMonthlyAverage(t *testing.T) {
	f, cs := setup(t, map[string]string{"StatisticSearch/kr/731Y004/M": "usd_krw_monthly_mixed.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"indicator": "usd_krw", "frequency": "monthly", "from": "2025-01", "to": "2025-04"}, &got))
	if p := f.path("StatisticSearch"); p != "StatisticSearch/kr/731Y004/M/202501/202504/0000001/0000100" {
		t.Errorf("path %s", p)
	}
	if got.Series[0].Aggregation != average {
		t.Errorf("got %+v", got.Series[0])
	}
}

// CPI is an index: a year more is fetched for the year-on-year change.
func TestCPIYearOnYear(t *testing.T) {
	f, cs := setup(t, map[string]string{"StatisticSearch/kr/901Y009/A": "cpi_annual.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"indicator": "cpi", "frequency": "annual", "from": "2017", "to": "2025"}, &got))
	if p := f.path("StatisticSearch"); p != "StatisticSearch/kr/901Y009/A/2016/2025/0" {
		t.Errorf("path %s", p)
	}
	obs := got.Series[0].Observations
	if len(obs) != 9 || obs[0].Period != "2017" || *obs[5].YoYPercent != 5.09 || *obs[8].YoYPercent != 2.13 || got.Series[0].Unit != "index (2020=100)" {
		t.Errorf("got %+v", obs)
	}
}

// GDP is reported in 십억원 and returned in won.
func TestUnitConversion(t *testing.T) {
	_, cs := setup(t, map[string]string{"StatisticSearch/kr/200Y101/A": "gdp_annual.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"indicator": "gdp", "from": "2016", "to": "2025"}, &got))
	s := got.Series[0]
	if s.Unit != "KRW" || s.SourceUnit != "십억원" || values(s)[9] != 2_676_674_800_000_000 || values(s)[6] != 2_328_169_000_000_000 {
		t.Errorf("got %s %s %v", s.Unit, s.SourceUnit, values(s))
	}
}

// Unemployment is a rate: the yearly change is in percentage points. The
// seasonally adjusted combination has only nulls at annual frequency.
func TestRateYearOnYear(t *testing.T) {
	_, cs := setup(t, map[string]string{"StatisticSearch/kr/901Y027/A": "unemployment_annual.json"})
	var got SeriesResult
	mustOK(t, call(t, cs, "ecos_get_series", map[string]any{"indicator": "unemployment_rate", "frequency": "annual", "from": "2020", "to": "2025", "yoy": true}, &got))
	if len(got.Series) != 1 || got.Note != "" || got.Series[0].Items[1].Name != "원계열" {
		t.Fatalf("got %+v", got)
	}
	obs := got.Series[0].Observations
	if len(obs) != 6 || *obs[0].YoYPoints != 0.2 || *obs[2].YoYPoints != -0.8 || obs[0].YoYPercent != nil {
		t.Errorf("got %+v", obs)
	}
}

func TestSeriesErrors(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"StatisticSearch/kr/722Y001": "error_no_data.json",
		"StatisticSearch/kr/NOPE":    "error_no_data.json",
	})
	for _, args := range []map[string]any{
		{},
		{"indicator": "inflation"},
		{"indicator": "cpi", "stat_code": "901Y009"},
		{"indicator": "kospi", "frequency": "monthly"},
		{"indicator": "usd_krw", "yoy": true},
		{"stat_code": "722Y001"},
		{"indicator": "cpi", "from": "2025-13"},
		{"indicator": "cpi", "from": "2025-06", "to": "2025-01"},
	} {
		if e := call(t, cs, "ecos_get_series", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	e := call(t, cs, "ecos_get_series", map[string]any{"indicator": "base_rate", "frequency": "monthly", "from": "2030-01", "to": "2030-02"}, nil)
	if e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("no data: %+v", e)
	}
	e = call(t, cs, "ecos_get_series", map[string]any{"stat_code": "NOPE", "frequency": "monthly"}, nil)
	if e == nil || e.Code != hub.CodeNotFound || !strings.Contains(e.Message, "unknown stat_code") {
		t.Errorf("unknown table: %+v", e)
	}
}

func TestBadKey(t *testing.T) {
	_, cs := setup(t, map[string]string{"StatisticSearch": "error_bad_key.json"})
	e := call(t, cs, "ecos_get_series", map[string]any{"indicator": "cpi"}, nil)
	if e == nil || e.Code != hub.CodeUnauthenticated || e.Hint == "" {
		t.Errorf("got %+v", e)
	}
}

func TestSearchTables(t *testing.T) {
	_, cs := setup(t, nil)
	var got SearchResult
	mustOK(t, call(t, cs, "ecos_search_series", map[string]any{"query": "M2 평잔"}, &got))
	if len(got.Tables) == 0 || got.Tables[0].StatCode != "161Y005" || got.Tables[0].NameEn == "" || got.Tables[0].Frequencies[0] != "monthly" {
		t.Fatalf("got %+v", got)
	}
	for _, tb := range got.Tables {
		if strings.HasPrefix(tb.Name, "1.") {
			t.Errorf("numbering kept: %s", tb.Name)
		}
	}
	// English, and a named indicator first.
	mustOK(t, call(t, cs, "ecos_search_series", map[string]any{"query": "base rate"}, &got))
	if got.Tables[0].Indicator != "base_rate" {
		t.Errorf("got %+v", got.Tables)
	}
}

func TestListItems(t *testing.T) {
	f, cs := setup(t, map[string]string{"StatisticItemList/kr/731Y004": "items_731Y004.json"})
	var got SearchResult
	mustOK(t, call(t, cs, "ecos_search_series", map[string]any{"stat_code": "731Y004", "query": "달러"}, &got))
	if p := f.path("StatisticItemList"); p != "StatisticItemList/kr/731Y004" {
		t.Errorf("path %s", p)
	}
	if len(got.Items) != 1 || got.Items[0].Code != "0000001" || got.Items[0].Dimension != 1 || len(got.Items[0].Available) != 4 || got.Items[0].Unit != "KRW" {
		t.Fatalf("got %+v", got)
	}
	if a := got.Items[0].Available[0]; a.Frequency != "annual" || a.From != "1964" {
		t.Errorf("available %+v", a)
	}
}
