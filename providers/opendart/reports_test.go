package opendart

import (
	"os"
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Samsung Electronics' 2025 half-year report: the current column is
// January to June, the others are all of 2024 and 2023.
func TestGetDividendsInterim(t *testing.T) {
	f, cs := setup(t, testKey)
	var got Dividends
	mustOK(t, call(t, cs, "opendart_get_dividends", map[string]any{"corp_code": "005930", "year": 2025, "report": "half"}, &got))
	if q := f.query("alotMatter.json"); q.Get("corp_code") != "00126380" || q.Get("bsns_year") != "2025" || q.Get("reprt_code") != "11012" {
		t.Errorf("query: %v", q)
	}
	if got.ReceiptNo != "20250814003156" || got.URL == "" || len(got.Periods) != 3 {
		t.Fatalf("got %+v", got)
	}
	cur, prior := got.Periods[0], got.Periods[1]
	if cur.FiscalYear != 2025 || cur.FullYear || cur.Through != "2025-06-30" || *cur.Common.CashPerShare != 732 ||
		*cur.CashDividendTotal != 4_901_077_000_000 || *cur.NetIncomeConsolidated != 12_962_441_000_000 || *cur.EPS != 1929 ||
		*cur.PayoutRatio != 37.8 || *cur.Common.CashYield != 1.2 || *cur.Preferred.CashYield != 1.5 || *cur.ParValue != 100 {
		t.Errorf("current: %+v common %+v", cur, cur.Common)
	}
	if prior.FiscalYear != 2024 || !prior.FullYear || prior.Through != "" || *prior.Common.CashPerShare != 1446 || *prior.Preferred.CashPerShare != 1447 {
		t.Errorf("prior: %+v", prior)
	}
	if cur.StockDividendTotal != nil || cur.Common.StockPerShare != nil || got.Periods[2].FiscalYear != 2023 {
		t.Errorf("'-' should be omitted: %+v", cur)
	}
}

// 2015: before the 50:1 split (par value ₩5,000), and EPS labeled
// "주당순이익(원)" without "(연결)".
func TestGetDividendsBeforeSplit(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("alotMatter.json", fixture(t, "alotMatter_2015.json"))
	var got Dividends
	mustOK(t, call(t, cs, "opendart_get_dividends", map[string]any{"corp_code": "00126380", "year": 2015}, &got))
	p := got.Periods[0]
	if !p.FullYear || *p.ParValue != 5000 || *p.Common.CashPerShare != 21000 || *p.EPS != 126305 || p.NetIncomeSeparate != nil {
		t.Errorf("got %+v", p)
	}
}

func TestGetDividendsErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"corp_code": "005930", "year": 2014},
		{"corp_code": "005930", "year": 2026},
		{"corp_code": "005930", "year": 2025, "report": "q2"},
	} {
		if e := call(t, cs, "opendart_get_dividends", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	f.set("alotMatter.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	if e := call(t, cs, "opendart_get_dividends", map[string]any{"corp_code": "005930", "year": 2025}, nil); e == nil || e.Code != hub.CodeNotFound || e.Hint == "" {
		t.Errorf("not filed: %+v", e)
	}
}

func TestGetMajorShareholders(t *testing.T) {
	f, cs := setup(t, testKey)
	var got MajorShareholders
	mustOK(t, call(t, cs, "opendart_get_major_shareholders", map[string]any{"corp_code": "005930", "year": 2024}, &got))
	if q := f.query("hyslrSttus.json"); q.Get("reprt_code") != "11011" || q.Get("bsns_year") != "2024" {
		t.Errorf("query: %v", q)
	}
	if got.LargestShareholder != "삼성생명보험㈜" || got.AsOf != "2024-12-31" || len(got.Holders) != 18 {
		t.Fatalf("got %s %s, %d holders", got.LargestShareholder, got.AsOf, len(got.Holders))
	}
	if c := got.Total.Common; *c.Shares != 1_198_033_154 || *c.Percent != 20.07 || *c.SharesAtStart != 1_235_835_433 || *got.Total.Preferred.Shares != 993_638 {
		t.Errorf("total: %+v %+v", *got.Total.Common, *got.Total.Preferred)
	}
	var order []string
	for _, h := range got.Holders[:4] {
		order = append(order, h.Name)
	}
	if want := "삼성생명보험㈜ 삼성물산㈜ 홍라희 이재용"; strings.Join(order, " ") != want {
		t.Errorf("order: %v", order)
	}
	h := got.Holders[2] // 홍라희: sold common shares, kept preferred
	if !(*h.Common.Shares == 97_978_700 && *h.Common.SharesAtStart == 117_302_806 && *h.Preferred.Shares == 206_633 && h.Note == "시간외 매매" && !h.Largest) {
		t.Errorf("holder: %+v", h)
	}
	for _, h := range got.Holders {
		if h.Name == "삼성생명보험㈜ (특별계정)" && !h.Largest {
			t.Errorf("special account: %+v", h)
		}
	}
}

// TYM: "김   식" with layout spaces is the largest shareholder; a "합계" row
// repeats the total.
func TestGetMajorShareholdersNames(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("hyslrSttus.json", fixture(t, "hyslrSttus_tym.json"))
	var got MajorShareholders
	mustOK(t, call(t, cs, "opendart_get_major_shareholders", map[string]any{"corp_code": "00126380", "year": 2024}, &got))
	if got.LargestShareholder != "김식" || len(got.Holders) != 11 || got.Holders[0].Name != "김식" || *got.Total.Common.Shares != 14_456_627 || got.Total.Preferred != nil {
		t.Errorf("got %s, %d holders, first %+v, total %+v", got.LargestShareholder, len(got.Holders), got.Holders[0], got.Total)
	}
}

// 삼환기업 2017: preferred rows say `"` for the name and relation.
func TestGetMajorShareholdersDitto(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("hyslrSttus.json", fixture(t, "hyslrSttus_ditto.json"))
	var got MajorShareholders
	mustOK(t, call(t, cs, "opendart_get_major_shareholders", map[string]any{"corp_code": "00126380", "year": 2017}, &got))
	if got.LargestShareholder != "최용권" || len(got.Holders) != 8 || *got.Total.Preferred.Shares != 67_470 {
		t.Fatalf("got %s, %d holders, total %+v", got.LargestShareholder, len(got.Holders), got.Total)
	}
	var preferred int64
	for _, h := range got.Holders {
		if h.Name == `"` || h.Relation == `"` {
			t.Errorf("ditto kept: %+v", h)
		}
		preferred += shares(h.Preferred)
	}
	if preferred != 67_470 || *got.Holders[0].Preferred.Shares != 0 {
		t.Errorf("preferred: sum %d, first %+v", preferred, got.Holders[0])
	}
}
