package opendart

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testKey = "TESTKEY"

// fakeDART serves testdata fixtures and records requests per endpoint.
type fakeDART struct {
	t        *testing.T
	mu       sync.Mutex
	hits     map[string]int
	last     map[string]url.Values
	override map[string]string // endpoint -> response body
}

func (f *fakeDART) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ep := strings.TrimPrefix(r.URL.Path, "/")
	q := r.URL.Query()
	f.mu.Lock()
	f.hits[ep]++
	f.last[ep] = q
	body, overridden := f.override[ep]
	f.mu.Unlock()

	switch {
	case q.Get("crtfc_key") != testKey && ep == "corpCode.xml":
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><result><status>010</status><message>등록되지 않은 키입니다.</message></result>`))
	case q.Get("crtfc_key") != testKey:
		w.Write([]byte(`{"status":"010","message":"등록되지 않은 키입니다."}`))
	case overridden:
		w.Write([]byte(body))
	case ep == "corpCode.xml":
		w.Write(zipFixture(f.t, "CORPCODE.xml"))
	default:
		b, err := os.ReadFile("testdata/" + ep)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}
}

func (f *fakeDART) set(ep, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.override[ep] = body
}

func (f *fakeDART) count(ep string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[ep]
}

func (f *fakeDART) query(ep string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[ep]
}

func zipFixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write(b)
	zw.Close()
	return buf.Bytes()
}

func setup(t *testing.T, key string) (*fakeDART, *mcp.ClientSession) {
	t.Helper()
	f := &fakeDART{t: t, hits: map[string]int{}, last: map[string]url.Values{}, override: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	p := &Provider{BaseURL: srv.URL, now: func() time.Time { return time.Date(2025, 11, 20, 10, 0, 0, 0, kst) }}
	h := hub.New(&mcp.Implementation{Name: "test", Version: "0"}, slog.New(slog.DiscardHandler))
	h.Add(p, func(k string) string {
		if k == envKey {
			return key
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

// call invokes a tool and decodes either its structured output into out or
// its error into a *hub.Error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *hub.Error {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var e hub.Error
		text := res.Content[0].(*mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			t.Fatalf("%s: error is not a hub.Error: %s", name, text)
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

func TestSearchCompanies(t *testing.T) {
	f, cs := setup(t, testKey)

	var got hub.List[Company]
	mustOK(t, call(t, cs, "opendart_search_companies", map[string]any{"query": "삼성 전자(주)"}, &got))
	names := []string{}
	for _, c := range got.Items {
		names = append(names, c.CorpName)
	}
	// exact match, then prefix (listed first), then substring
	if strings.Join(names, ",") != "삼성전자,삼성전자판매" {
		t.Fatalf("order: %v", names)
	}
	if got.Items[0].CorpCode != "00126380" || !got.Items[0].Listed || got.Items[0].ModifiedDate != "2025-12-01" {
		t.Fatalf("item: %+v", got.Items[0])
	}
	if got.Items[1].StockCode != "" || got.Items[1].Listed {
		t.Fatalf("blank stock_code must be trimmed: %+v", got.Items[1])
	}

	mustOK(t, call(t, cs, "opendart_search_companies", map[string]any{"query": "삼성", "listed_only": true}, &got))
	if len(got.Items) != 2 || got.PageInfo.TotalCount != 2 {
		t.Fatalf("listed_only: %+v", got)
	}

	for _, q := range []string{"005930", "00126380", "samsung electronics"} {
		mustOK(t, call(t, cs, "opendart_search_companies", map[string]any{"query": q}, &got))
		if len(got.Items) == 0 || got.Items[0].CorpCode != "00126380" {
			t.Fatalf("%q: %+v", q, got.Items)
		}
	}

	if n := f.count("corpCode.xml"); n != 1 {
		t.Fatalf("company list must be downloaded once, got %d", n)
	}
}

func TestBadKeyIsUnauthenticated(t *testing.T) {
	_, cs := setup(t, "WRONG")
	e := call(t, cs, "opendart_search_companies", map[string]any{"query": "삼성"}, nil)
	if e == nil || e.Code != hub.CodeUnauthenticated || e.UpstreamCode != "010" || e.Provider != "opendart" {
		t.Fatalf("got %+v", e)
	}
}

func TestGetCompanyByStockCode(t *testing.T) {
	f, cs := setup(t, testKey)
	var got CompanyProfile
	mustOK(t, call(t, cs, "opendart_get_company", map[string]any{"corp_code": "005930"}, &got))
	if f.query("company.json").Get("corp_code") != "00126380" {
		t.Fatalf("stock code not resolved: %v", f.query("company.json"))
	}
	if got.Market != "KOSPI" || got.EstablishedDate != "1969-01-13" || got.FiscalYearEndMonth != 12 || got.IRPage != "" {
		t.Fatalf("got %+v", got)
	}

	e := call(t, cs, "opendart_get_company", map[string]any{"corp_code": "삼성"}, nil)
	if e == nil || e.Code != hub.CodeInvalidArgument || e.Hint == "" {
		t.Fatalf("got %+v", e)
	}
}

func TestSearchDisclosures(t *testing.T) {
	f, cs := setup(t, testKey)
	var got hub.List[Disclosure]
	mustOK(t, call(t, cs, "opendart_search_disclosures", map[string]any{
		"corp_code": "00126380", "type": "periodic", "final_only": true, "page_size": 20,
	}, &got))

	q := f.query("list.json")
	want := map[string]string{
		"corp_code": "00126380", "bgn_de": "20251021", "end_de": "20251120",
		"pblntf_ty": "A", "last_reprt_at": "Y", "page_no": "1", "page_count": "20",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	d := got.Items[0]
	if d.ReceivedDate != "2026-10-23" || d.Market != "KOSPI" || d.URL != viewerURL+"20261023000484" || got.PageInfo.TotalCount != 24 {
		t.Fatalf("got %+v", got)
	}

	f.set("list.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	mustOK(t, call(t, cs, "opendart_search_disclosures", map[string]any{"corp_code": "00126380"}, &got))
	if got.Items == nil || len(got.Items) != 0 || got.PageInfo.TotalCount != 0 {
		t.Fatalf("no data must be an empty list, got %+v", got)
	}
}

func TestSearchDisclosuresValidation(t *testing.T) {
	_, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"start_date": "2025-01-01", "end_date": "2025-06-30"}, // > 3 months without corp_code
		{"start_date": "2025/01/01"},
		{"start_date": "2025-11-10", "end_date": "2025-11-01"},
		{"type": "annual"},
		{"market": "NASDAQ"},
	} {
		if e := call(t, cs, "opendart_search_disclosures", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
}

func TestGetFinancials(t *testing.T) {
	f, cs := setup(t, testKey)
	var got Financials
	mustOK(t, call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2024}, &got))

	if q := f.query("fnlttSinglAcnt.json"); q.Get("reprt_code") != "11011" || q.Get("bsns_year") != "2024" {
		t.Fatalf("query: %v", q)
	}
	// 15 consolidated rows, one of them a duplicate 당기순이익(손실).
	if got.Statement != "consolidated" || got.Currency != "KRW" || len(got.Accounts) != 14 || got.URL != viewerURL+"20250311001085" {
		t.Fatalf("got %+v", got)
	}
	assets, ni := account(t, got, "자산총계"), account(t, got, "당기순이익(손실)")
	if assets.StatementType != "balance_sheet" || assets.CurrentPeriod != "2024-12-31" || *assets.CurrentAmount != 514531948000000 {
		t.Fatalf("assets: %+v", assets)
	}
	if ni.StatementType != "income_statement" || ni.CurrentPeriod != "2024-01-01 ~ 2024-12-31" || *ni.PriorAmount != 15487100000000 || *ni.BeforePriorAmount != 55654077000000 || ni.CurrentQuarterAmount != nil {
		t.Fatalf("net income: %+v", ni)
	}

	mustOK(t, call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2024, "statement": "separate"}, &got))
	if len(got.Accounts) != 14 || *account(t, got, "자산총계").CurrentAmount != 324966127000000 {
		t.Fatalf("separate: %+v", got)
	}
}

func TestGetFinancialsInterim(t *testing.T) {
	f, cs := setup(t, testKey)
	b, err := os.ReadFile("testdata/fnlttSinglAcnt_half.json")
	if err != nil {
		t.Fatal(err)
	}
	f.set("fnlttSinglAcnt.json", string(b))
	var got Financials
	mustOK(t, call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "005930", "year": 2025, "report": "half"}, &got))

	assets := account(t, got, "자산총계")
	if assets.CurrentPeriod != "2025-06-30" || *assets.CurrentAmount != 504875185000000 || assets.CurrentQuarterAmount != nil {
		t.Fatalf("assets: %+v", assets)
	}
	// The period label is year-to-date, so current_amount must be too.
	rev := account(t, got, "매출액")
	if rev.CurrentPeriod != "2025-01-01 ~ 2025-06-30" || *rev.CurrentAmount != 153706820000000 || *rev.CurrentQuarterAmount != 74566317000000 ||
		*rev.PriorAmount != 145983903000000 || *rev.PriorQuarterAmount != 74068302000000 {
		t.Fatalf("revenue: %+v", rev)
	}
}

func account(t *testing.T, f Financials, name string) Account {
	t.Helper()
	for _, a := range f.Accounts {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no account %q in %+v", name, f.Accounts)
	return Account{}
}

func TestGetFinancialsErrors(t *testing.T) {
	f, cs := setup(t, testKey)

	// Only separate statements filed: suggest switching.
	f.set("fnlttSinglAcnt.json", `{"status":"000","message":"정상","list":[{"account_nm":"자산총계","fs_div":"OFS","sj_div":"BS","thstrm_amount":"1"}]}`)
	e := call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2023}, nil)
	if e == nil || e.Code != hub.CodeNotFound || !strings.Contains(e.Hint, "statement=separate") {
		t.Fatalf("got %+v", e)
	}

	// Rate limit inside HTTP 200 maps to RATE_LIMITED and is not cached.
	f.set("fnlttSinglAcnt.json", `{"status":"020","message":"요청 제한을 초과하였습니다."}`)
	for range 2 {
		e = call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2022}, nil)
		if e == nil || e.Code != hub.CodeRateLimited || e.UpstreamCode != "020" || !e.Retryable {
			t.Fatalf("got %+v", e)
		}
	}
	if n := f.count("fnlttSinglAcnt.json"); n != 3 {
		t.Fatalf("error responses must not be cached: %d upstream calls", n)
	}

	if e := call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2014}, nil); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Fatalf("year 2014: %+v", e)
	}
	if e := call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "00126380", "year": 2024, "report": "q2"}, nil); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Fatalf("report q2: %+v", e)
	}
}

func TestAPIKeyNeverInErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("company.json", `not json`)
	e := call(t, cs, "opendart_get_company", map[string]any{"corp_code": "00126380"}, nil)
	if e == nil || strings.Contains(e.Message, testKey) {
		t.Fatalf("got %+v", e)
	}
}
