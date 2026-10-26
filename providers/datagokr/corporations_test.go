package datagokr

import (
	"testing"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

func TestGetCorporationUsesLatestRecord(t *testing.T) {
	f, cs := setup(t, map[string]string{"getCorpOutline_V2": "corp_history_samsung.xml"})

	var got CorporationProfile
	if e := call(t, cs, "datagokr_get_corporation", map[string]any{"crno": "130111-0006246"}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("getCorpOutline_V2"); q.Get("crno") != "1301110006246" {
		t.Errorf("hyphens must be removed: %v", q)
	}
	// The fixture has 19 records in no particular order; older ones name a
	// former CEO (한종희) or lack the market.
	if got.CEO != "전영현, 노태문" || got.AsOf != "2026-10-25" || got.Market != "KOSPI" {
		t.Errorf("not the latest record: %+v", got.Corporation)
	}
	if got.Name != "삼성전자(주)" || got.NameEn != "SAMSUNG ELECTRONICS CO,.LTD" || got.BizRegNo != "1248100998" || got.DartCorpCode != "00126380" {
		t.Errorf("identity: %+v", got.Corporation)
	}
	if got.EstablishedDate != "1969-01-13" || got.ListedDate != "1975-06-11" || got.DelistedDate != "" || *got.FiscalYearEndMonth != 12 {
		t.Errorf("dates: %+v", got)
	}
	if got.Address != "경기도 수원시 영통구 삼성로 129 (매탄동)" {
		t.Errorf("address whitespace: %q", got.Address)
	}
	if *got.Employees != 128881 || *got.AvgSalary != 158000000 || *got.AvgTenureYears != 13.7 || got.Auditor != "삼정회계법인" || got.AuditOpinion != "적정의견" {
		t.Errorf("figures: %+v", got)
	}
	if got.IsSME != nil || got.Industry != "" {
		t.Errorf("empty fields must be omitted: %+v", got)
	}
}

func TestGetCorporationErrors(t *testing.T) {
	_, cs := setup(t, map[string]string{"getCorpOutline_V2": "holidays_none.xml"})
	if e := call(t, cs, "datagokr_get_corporation", map[string]any{"crno": "1301110006246"}, nil); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("no records: got %+v", e)
	}
	if e := call(t, cs, "datagokr_get_corporation", map[string]any{"crno": "124-81-00998"}, nil); e == nil || e.Code != hub.CodeInvalidArgument || e.Hint == "" {
		t.Errorf("a business registration number is not a crno: got %+v", e)
	}
}

func TestSearchCorporations(t *testing.T) {
	f, cs := setup(t, map[string]string{"getCorpOutline_V2": "corp_search_samsung_electronics.xml"})

	var got hub.List[Corporation]
	if e := call(t, cs, "datagokr_search_corporations", map[string]any{"query": "삼성 전자(주)"}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("getCorpOutline_V2")
	if q.Get("corpNm") != "삼성전자" || q.Get("numOfRows") != "1000" {
		t.Errorf("query: %v", q)
	}
	// 108 records for 37 companies. Upstream lists 삼성전자잠실판매 first.
	if got.PageInfo.TotalCount != 37 || len(got.Items) != 10 {
		t.Fatalf("page: %+v", got.PageInfo)
	}
	first := got.Items[0]
	if first.Crno != "1301110006246" || first.CEO != "전영현, 노태문" || first.Market != "KOSPI" || first.DartCorpCode != "00126380" {
		t.Errorf("exact match first, latest record: %+v", first)
	}
	seen := map[string]bool{}
	for _, c := range got.Items {
		if seen[c.Crno] {
			t.Errorf("duplicate %s", c.Crno)
		}
		seen[c.Crno] = true
	}

	got = hub.List[Corporation]{}
	if e := call(t, cs, "datagokr_search_corporations", map[string]any{"query": "삼성전자", "listed_only": true}, &got); e != nil {
		t.Fatal(e)
	}
	if len(got.Items) != 1 || got.Items[0].Crno != "1301110006246" {
		t.Errorf("listed_only: %+v", got.Items)
	}
}

func TestSearchCorporationsTooBroad(t *testing.T) {
	_, cs := setup(t, map[string]string{"getCorpOutline_V2": `<response><header><resultCode>00</resultCode></header><body><totalCount>3158</totalCount></body></response>`})
	e := call(t, cs, "datagokr_search_corporations", map[string]any{"query": "삼성"}, nil)
	if e == nil || e.Code != hub.CodeInvalidArgument || e.Hint == "" {
		t.Errorf("got %+v", e)
	}
	if e := call(t, cs, "datagokr_search_corporations", map[string]any{"query": " (주) "}, nil); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Errorf("empty query: %+v", e)
	}
}
