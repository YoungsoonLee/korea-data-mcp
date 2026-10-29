package datagokr

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

const stockOp = "getStockPriceInfo_V2"

func stockFixtures(extra map[string]string) map[string]string {
	m := map[string]string{
		stockOp:                        "stock_head.xml", // no filters: the latest day first
		stockOp + "?basDt=20261026":    "stock_listed_20261026.xml",
		stockOp + "?likeSrtnCd=005930": "stock_samsung_202609.xml",
	}
	maps.Copy(m, extra)
	return m
}

func TestGetStockPrices(t *testing.T) {
	f, cs := setup(t, stockFixtures(nil))
	for i, stock := range []string{"005930", "삼성전자", "삼성 전자", "KR7005930003"} {
		var got StockPrices
		if e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": stock, "from": "2026-09-01", "to": "2026-09-30"}, &got); e != nil {
			t.Fatalf("%s: %v", stock, e)
		}
		q := f.query(stockOp)
		// Later calls are served from the cache.
		if i == 0 && (q.Get("likeSrtnCd") != "005930" || q.Get("beginBasDt") != "20260901" || q.Get("endBasDt") != "20261001" || q.Has("srtnCd")) {
			t.Errorf("%s: query %v", stock, q)
		}
		if got.Stock != (Stock{Code: "005930", ISIN: "KR7005930003", Name: "삼성전자", Market: "KOSPI"}) {
			t.Errorf("%s: stock %+v", stock, got.Stock)
		}
		if got.PageInfo.TotalCount != 20 || len(got.Items) != 20 || got.Items[0].Date != "2026-09-30" || got.Items[19].Date != "2026-09-01" {
			t.Errorf("%s: %+v, first %+v", stock, got.PageInfo, got.Items[0])
		}
		for _, it := range got.Items {
			if it.Name != "" || it.NoTrades || it.Open == nil || it.Close == nil || it.ChangeRate == nil || it.MarketCap == nil {
				t.Errorf("%s: item %+v", stock, it)
			}
		}
	}
}

const emptyStockPage = "<response><header><resultCode>00</resultCode></header><body><items/><totalCount>0</totalCount></body></response>"

// Names are matched here against the latest day's listing, ignoring case and
// spaces; the prices are then queried by code.
func TestGetStockPricesByName(t *testing.T) {
	f, cs := setup(t, stockFixtures(map[string]string{stockOp + "?endBasDt=20261026": emptyStockPage}))
	for i, tc := range []struct {
		in   string
		want Stock
	}{
		{"naver", Stock{Code: "035420", ISIN: "KR7035420009", Name: "NAVER", Market: "KOSPI"}}, // likeItmsNm is case-sensitive upstream
		{"sk하이닉스", Stock{Code: "000660", ISIN: "KR7000660001", Name: "SK하이닉스", Market: "KOSPI"}},
		{"현대차", Stock{Code: "005380", ISIN: "KR7005380001", Name: "현대차", Market: "KOSPI"}}, // not 현대차우 or 현대차증권
		{"삼성전자우", Stock{Code: "005935", ISIN: "KR7005931001", Name: "삼성전자우", Market: "KOSPI"}},
		{"KR7005931001", Stock{Code: "005935", ISIN: "KR7005931001", Name: "삼성전자우", Market: "KOSPI"}}, // ISIN; not code 005931
		{"홀딩스", Stock{Code: "005490", ISIN: "KR7005490008", Name: "POSCO홀딩스", Market: "KOSPI"}},       // the only partial match
		{"0001a0", Stock{Code: "0001A0"}}, // a code with a letter
	} {
		var got StockPrices
		// A different from date per case, so no price query is served from the cache.
		from := fmt.Sprintf("2026-10-%02d", i+1)
		if e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": tc.in, "from": from}, &got); e != nil {
			t.Errorf("%s: %v", tc.in, e)
			continue
		}
		if q := f.query(stockOp); got.Stock != tc.want || q.Get("likeSrtnCd") != tc.want.Code {
			t.Errorf("%s: got %+v, query %v", tc.in, got.Stock, q)
		}
	}

	for _, tc := range []struct {
		name string
		code hub.Code
		msg  string
	}{
		{"삼성", hub.CodeInvalidArgument, "삼성전자 (005930)"},
		{"현대자동차", hub.CodeNotFound, "no listed stock"},
	} {
		e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": tc.name}, nil)
		if e == nil || e.Code != tc.code || !strings.Contains(e.Message, tc.msg) {
			t.Errorf("%s: got %+v", tc.name, e)
		}
	}
}

func TestGetStockPricesNoTrades(t *testing.T) {
	_, cs := setup(t, stockFixtures(map[string]string{stockOp + "?likeSrtnCd=000300": "stock_halted.xml"}))
	var got StockPrices
	if e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": "000300", "from": "2026-10-19"}, &got); e != nil {
		t.Fatal(e)
	}
	it := got.Items[0]
	if !it.NoTrades || it.Open != nil || it.High != nil || it.Low != nil || *it.Close != 4200 || *it.Volume != 0 {
		t.Errorf("halted day: %+v", it)
	}
}

// History follows the code across a rename; each day keeps its own name.
func TestGetStockPricesRename(t *testing.T) {
	_, cs := setup(t, stockFixtures(map[string]string{stockOp + "?likeSrtnCd=005490": "stock_posco_rename.xml"}))
	var got StockPrices
	if e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": "005490", "from": "2022-03-10", "to": "2022-03-21"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Stock.Name != "POSCO홀딩스" || len(got.Items) != 8 || got.Items[2].Name != "" || got.Items[3].Name != "POSCO" || got.Items[3].Date != "2022-03-16" {
		t.Errorf("got %+v, items %+v", got.Stock, got.Items)
	}
}

func TestGetStockPricesValidation(t *testing.T) {
	f, cs := setup(t, stockFixtures(map[string]string{stockOp + "?likeSrtnCd=999999": emptyStockPage}))
	for _, args := range []map[string]any{
		{"stock": ""},
		{"stock": "005930", "from": "20260901"}, // upstream: "success, 0 results"
		{"stock": "005930", "from": "2019-12-31"},
		{"stock": "005930", "to": "2026-10-26"}, // future (now is 2026-10-25 in tests)
		{"stock": "005930", "from": "2026-10-10", "to": "2026-10-01"},
		{"stock": "005930", "page_size": 1001},
	} {
		if e := call(t, cs, "datagokr_get_stock_prices", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	if e := call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": "999999"}, nil); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown code: got %+v", e)
	}

	// Defaults: the 30 days up to today, end exclusive.
	call(t, cs, "datagokr_get_stock_prices", map[string]any{"stock": "005930"}, &StockPrices{})
	if q := f.query(stockOp); q.Get("beginBasDt") != "20260925" || q.Get("endBasDt") != "20261026" {
		t.Errorf("default range: %v", q)
	}
}
