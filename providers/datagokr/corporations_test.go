package datagokr

import (
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
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
	if got.FormerNames != nil {
		t.Errorf("former names: %+v", got.FormerNames)
	}
	if got.IsSME != nil || got.Industry != "" {
		t.Errorf("empty fields must be omitted: %+v", got)
	}
}

func TestGetCorporationByBusinessNumber(t *testing.T) {
	f, cs := setup(t, map[string]string{"getCorpOutline_V2": "corp_history_hanwha_ocean.xml"})

	var got CorporationProfile
	if e := call(t, cs, "datagokr_get_corporation", map[string]any{"business_number": "104-81-57667"}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("getCorpOutline_V2"); q.Get("bzno") != "1048157667" || q.Has("crno") {
		t.Errorf("query: %v", q)
	}
	// A name search for 대우조선해양 finds only its old records, with a
	// former CEO.
	if got.Name != "한화오션(주)" || got.Crno != "1101112095837" || got.CEO != "김희철" || got.AsOf != "2026-11-12" {
		t.Errorf("current record: %+v", got.Corporation)
	}
	if len(got.FormerNames) != 1 || got.FormerNames[0] != (FormerName{Name: "대우조선해양(주)", Until: "2023-06-12"}) {
		t.Errorf("former names: %+v", got.FormerNames)
	}
}

func TestFormerNames(t *testing.T) {
	r := func(name, from, to string) corpOutline {
		return corpOutline{Crno: "1", CorpNm: name, FstOpegDt: from, LastOpegDt: to}
	}
	// 다이나맥 (crno 1601110007032), unordered as returned.
	records := []corpOutline{
		r("(주)다이나맥", "20261109", "20261112"),
		r("KR모터스(주)", "20250921", "20260319"),
		r("효성기계공업(주)", "20251008", "20251014"), // a glitch: a name from decades earlier
		r("KR모터스(주)", "20200509", "20210321"),
		r("KR모터스(주)", "20260101", "20260101"),
		r("KR모터스 주식회사", "20261102", "20261108"),
		r("다이나맥 주식회사", "20200101", "20200101"), // not a former name: the same as the current one
	}
	got := formerNames(records, records[0])
	if len(got) != 1 || got[0] != (FormerName{Name: "KR모터스 주식회사", Until: "2026-11-08"}) {
		t.Errorf("got %+v", got)
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
	for _, args := range []map[string]any{{}, {"crno": "1301110006246", "business_number": "1248100998"}, {"business_number": "12481009"}} {
		if e := call(t, cs, "datagokr_get_corporation", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
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
