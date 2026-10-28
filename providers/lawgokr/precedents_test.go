package lawgokr

import (
	"strings"
	"testing"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

// By default only court records, which all have text, are searched.
func TestSearchPrecedents(t *testing.T) {
	f, cs := setup(t, map[string]string{"lawSearch?target=prec": "prec_search_unfair_dismissal.xml"})
	var got hub.List[PrecedentSummary]
	if e := call(t, cs, "lawgokr_search_precedents", map[string]any{"query": "부당해고", "page_size": 3}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("lawSearch")
	if q.Get("datSrcNm") != "대법원" || q.Get("query") != "부당해고" || q.Has("search") || q.Get("display") != "3" {
		t.Errorf("query: %v", q)
	}
	want := PrecedentSummary{ID: "616245", CaseName: "부당해고구제재심판정취소", CaseNumber: "2023두54914", Court: "대법원", Decided: "2026-01-29",
		CaseType: "일반행정", JudgmentType: "판결", Source: "court", TextAvailable: true, URL: "https://www.law.go.kr/LSW/precInfoP.do?precSeq=616245"}
	if got.PageInfo.TotalCount != 399 || len(got.Items) != 3 || got.Items[0] != want {
		t.Errorf("got %+v", got)
	}
}

func TestSearchPrecedentsFilters(t *testing.T) {
	f, cs := setup(t, map[string]string{"lawSearch?target=prec": "prec_search_unfair_dismissal.xml"})
	for _, tc := range []struct {
		args map[string]any
		want map[string]string
	}{
		{map[string]any{"query": "2020 다 296604"}, map[string]string{"nb": "2020다296604", "query": ""}}, // spaces give no results upstream
		{map[string]any{"query": "부당해고", "in_text": true}, map[string]string{"search": "2"}},
		{map[string]any{"query": "a", "court": "supreme"}, map[string]string{"org": "400201"}},
		{map[string]any{"query": "b", "court": "lower"}, map[string]string{"org": "400202"}},
		{map[string]any{"query": "c", "court": "서울고등법원"}, map[string]string{"curt": "서울고등법원", "org": ""}},
		{map[string]any{"from": "2025-01-01", "to": "2025-12-31"}, map[string]string{"prncYd": "20250101~20251231"}},
		{map[string]any{"from": "2026-01-01"}, map[string]string{"prncYd": "20260101~20261028"}},
		{map[string]any{"query": "d", "source": "all"}, map[string]string{"datSrcNm": ""}},
		{map[string]any{"query": "e", "source": "local_tax"}, map[string]string{"datSrcNm": "지방세법령정보시스템"}},
	} {
		if e := call(t, cs, "lawgokr_search_precedents", tc.args, &hub.List[PrecedentSummary]{}); e != nil {
			t.Fatalf("%v: %v", tc.args, e)
		}
		q := f.query("lawSearch")
		for k, v := range tc.want {
			if q.Get(k) != v {
				t.Errorf("%v: %s = %q, want %q", tc.args, k, q.Get(k), v)
			}
		}
	}
}

// National Tax Service records have no court name and no text in the API.
func TestSearchPrecedentsNationalTax(t *testing.T) {
	_, cs := setup(t, map[string]string{"lawSearch?target=prec": "prec_search_national_tax.xml"})
	var got hub.List[PrecedentSummary]
	if e := call(t, cs, "lawgokr_search_precedents", map[string]any{"query": "부당해고", "source": "national_tax"}, &got); e != nil {
		t.Fatal(e)
	}
	p := got.Items[0]
	if p.Court != "서울행정법원" || p.CaseNumber != "2025구합53785" || p.Source != "national_tax" || p.TextAvailable || p.JudgmentType != "" {
		t.Errorf("got %+v", p)
	}
}

func TestGetPrecedent(t *testing.T) {
	f, cs := setup(t, map[string]string{
		"lawSearch?nb=2020다296604": "prec_search_case_number.xml",
		"lawService?ID=606191":     "prec_606191.xml",
	})
	var got Precedent
	if e := call(t, cs, "lawgokr_get_precedent", map[string]any{"case_number": "2020다296604"}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("lawSearch"); q.Has("datSrcNm") { // a case number is looked up in every source
		t.Errorf("query: %v", q)
	}
	if got.ID != "606191" || got.Court != "대법원" || got.Decided != "2025-05-15" || got.CaseName != "손해배상(기)[한국소비자원의 행정상 공표의 위법성이 문제된 사건]" {
		t.Errorf("summary: %+v", got.PrecedentSummary)
	}
	if !strings.HasPrefix(got.Issues, "[1] 이른바 ‘행정상 공표’") || !strings.Contains(got.Issues, "\n[2] 명예훼손으로") ||
		!strings.HasPrefix(got.CitedStatutes, "[1] 민법 제750조") || strings.Contains(got.Holding, "<br") {
		t.Errorf("sections: issues %q, statutes %q", got.Issues, got.CitedStatutes)
	}
	if got.FullText != "" || got.FullTextLength != 7609 {
		t.Errorf("full text without include_full_text: %d chars, length %d", len(got.FullText), got.FullTextLength)
	}
}

// Local-tax records have "null" fields and <br/> followed by newlines.
func TestGetPrecedentFullText(t *testing.T) {
	_, cs := setup(t, map[string]string{"lawService?ID=622879": "prec_622879_local_tax.xml"})
	var got Precedent
	if e := call(t, cs, "lawgokr_get_precedent", map[string]any{"id": "622879", "include_full_text": true}, &got); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(got.FullText, "【심급】\n3심\n【세목】\n취득세\n【주문】\n상고를 기각한다.\n\n상고비용은") ||
		got.FullTextLength != 4102 || strings.Contains(got.FullText, "<br") || strings.Contains(got.FullText, "\n\n\n") {
		t.Errorf("full text: %q", got.FullText[:200])
	}
	if got.Issues != "" || got.JudgmentType != "기각" || !got.TextAvailable {
		t.Errorf("got %+v", got.PrecedentSummary)
	}
}

func TestGetPrecedentErrors(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"lawService?ID=617999": "prec_not_found.xml",
		"lawSearch?nb=2099다1":  "search_none.xml",
	})
	e := call(t, cs, "lawgokr_get_precedent", map[string]any{"id": "617999"}, nil)
	if e == nil || e.Code != hub.CodeNotFound || !strings.Contains(e.Hint, "precSeq=617999") {
		t.Errorf("national tax record: got %+v", e)
	}
	if e := call(t, cs, "lawgokr_get_precedent", map[string]any{"case_number": "2099다1"}, nil); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown case number: got %+v", e)
	}
	for _, args := range []map[string]any{
		{},
		{"id": "2020다296604"},
		{"case_number": "다296604"},
	} {
		if e := call(t, cs, "lawgokr_get_precedent", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	for _, args := range []map[string]any{
		{},
		{"query": "a", "source": "supreme_court"},
		{"query": "a", "from": "2026/01/01"},
		{"query": "a", "from": "2026-02-01", "to": "2026-01-01"},
		{"case_number": "296604"},
		{"query": "a", "page_size": 101},
	} {
		if e := call(t, cs, "lawgokr_search_precedents", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
}
