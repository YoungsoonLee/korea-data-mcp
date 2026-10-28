package lawgokr

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

// Upstream sorts by name, so 민법 comes after 난민법 and its decree; exact
// matches and acts come first here.
func TestSearchLaws(t *testing.T) {
	f, cs := setup(t, map[string]string{"lawSearch?query=민법": "search_minbeop.xml"})
	var got hub.List[LawSummary]
	if e := call(t, cs, "lawgokr_search_laws", map[string]any{"query": "민법"}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("lawSearch")
	if q.Get("OC") != testOC || q.Get("target") != "eflaw" || q.Get("nw") != "3" || q.Get("type") != "XML" || q.Get("display") != "100" {
		t.Errorf("query: %v", q)
	}
	var names []string
	for _, it := range got.Items {
		names = append(names, it.Name)
	}
	want := []string{"민법", "민법제312조의2단서의시행에관한규정", "민법법인 및 특수법인 등기규칙", "난민법", "난민법 시행령", "난민법 시행규칙"}
	if !slices.Equal(names, want) || got.PageInfo.TotalCount != 6 {
		t.Errorf("got %v", names)
	}
	m := got.Items[0]
	if m.LawID != "001706" || m.Kind != "법률" || m.EffectiveDate != "2026-03-17" || m.URL != "https://www.law.go.kr/법령/민법" {
		t.Errorf("민법: %+v", m)
	}
}

// Upstream does not search abbreviations; a two-character search finds them.
func TestSearchLawsAbbreviation(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"lawSearch?query=중대재해 처벌법": "search_none.xml",
		"lawSearch?query=중대":       "search_prefix.xml",
	})
	var got hub.List[LawSummary]
	if e := call(t, cs, "lawgokr_search_laws", map[string]any{"query": "중대재해 처벌법"}, &got); e != nil {
		t.Fatal(e)
	}
	if len(got.Items) != 2 || got.Items[0].Name != "중대재해 처벌 등에 관한 법률" || got.Items[0].Abbreviation != "중대재해처벌법" || got.Items[1].Kind != "대통령령" {
		t.Errorf("got %+v", got.Items)
	}
}

func TestSearchLawsInText(t *testing.T) {
	f, cs := setup(t, map[string]string{"lawSearch?search=2": "search_text.xml"})
	var got hub.List[LawSummary]
	if e := call(t, cs, "lawgokr_search_laws", map[string]any{"query": "최저임금", "in_text": true, "page": 1, "page_size": 2}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("lawSearch"); q.Get("display") != "2" || q.Get("page") != "1" {
		t.Errorf("query: %v", q)
	}
	if got.PageInfo.TotalCount != 81 || len(got.Items) != 2 || !got.PageInfo.HasNext {
		t.Errorf("got %+v", got)
	}
}

func laborRoutes(extra map[string]string) map[string]string {
	m := map[string]string{"lawSearch?LID=001872": "versions_labor.xml"}
	maps.Copy(m, extra)
	return m
}

// The text comes from the version in force today, by effective date.
func TestGetLawArticle(t *testing.T) {
	f, cs := setup(t, laborRoutes(map[string]string{"lawService?JO=005000": "law_labor_50.xml"}))
	var got Law
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "001872", "article": "제50조"}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("lawService")
	if q.Get("target") != "eflaw" || q.Get("MST") != "285279" || q.Get("efYd") != "20261008" || q.Get("JO") != "005000" {
		t.Errorf("query: %v", q)
	}
	if got.Name != "근로기준법" || got.EffectiveDate != "2026-10-08" || got.AsOf != "2026-10-28" || len(got.Articles) != 1 {
		t.Fatalf("got %+v", got)
	}
	a := got.Articles[0]
	if a.Number != "제50조" || a.Title != "근로시간" || a.Chapter != "제4장 근로시간과 휴식" || a.URL != "https://www.law.go.kr/법령/근로기준법/제50조" ||
		!strings.HasPrefix(a.Text, "제50조(근로시간)\n① 1주 간의 근로시간은") || strings.Count(a.Text, "\n") != 3 {
		t.Errorf("article: %+v", a)
	}
	var up []string
	for _, u := range got.Upcoming {
		up = append(up, u.EffectiveDate+" "+u.PromulgationDate)
	}
	want := []string{"2026-12-08 2026-04-07", "2026-12-10 2026-06-09", "2027-01-01 2026-03-17", "2027-01-01 2026-04-07", "2027-06-10 2026-06-09"}
	if !slices.Equal(up, want) {
		t.Errorf("upcoming: %v", up)
	}
}

// 제44조의4 is in the "current law" text already, but takes effect only on
// 2027-01-01.
func TestGetLawNotYetInForce(t *testing.T) {
	f, cs := setup(t, laborRoutes(map[string]string{
		"lawService?JO=004404&efYd=20261008": "law_labor_44_4_missing.xml",
		"lawService?JO=004404&efYd=20270101": "law_labor_44_4_2027.xml",
	}))
	e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "001872", "article": "제44조의4"}, nil)
	if e == nil || e.Code != hub.CodeNotFound || !strings.Contains(e.Hint, "2026-12-08") {
		t.Errorf("today: got %+v", e)
	}

	var got Law
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "001872", "article": "44의4", "as_of": "2027-01-01"}, &got); e != nil {
		t.Fatal(e)
	}
	// Two versions take effect on 2027-01-01; the later promulgation wins.
	if q := f.query("lawService"); q.Get("MST") != "285279" || q.Get("efYd") != "20270101" {
		t.Errorf("query: %v", q)
	}
	if got.EffectiveDate != "2027-01-01" || got.Articles[0].Number != "제44조의4" || !strings.Contains(got.Articles[0].Text, "임금비용") ||
		len(got.Upcoming) != 1 || got.Upcoming[0].EffectiveDate != "2027-06-10" {
		t.Errorf("got %+v", got)
	}
}

// For a past date, upcoming only lists amendments promulgated by then.
func TestGetLawPast(t *testing.T) {
	f, cs := setup(t, laborRoutes(map[string]string{"lawService?efYd=20191101": "law_labor_50.xml"}))
	var got Law
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "001872", "article": "50", "as_of": "2020-01-01"}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("lawService"); q.Get("MST") != "208569" {
		t.Errorf("query: %v", q)
	}
	if got.EffectiveDate != "2019-11-01" || len(got.Upcoming) != 2 || got.Upcoming[0].EffectiveDate != "2020-01-16" || got.Upcoming[1].EffectiveDate != "2021-07-01" {
		t.Errorf("got %s, upcoming %+v", got.EffectiveDate, got.Upcoming)
	}
}

func TestGetLawDeletedArticle(t *testing.T) {
	_, cs := setup(t, laborRoutes(map[string]string{"lawService?JO=003500": "law_labor_35.xml"}))
	var got Law
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "001872", "article": "35"}, &got); e != nil {
		t.Fatal(e)
	}
	if a := got.Articles[0]; !a.Deleted || a.Text != "제35조 삭제 <2019.1.15>" {
		t.Errorf("got %+v", a)
	}
}

// A whole law by its abbreviation: articles without chapter headings, with
// subparagraphs (호) and items (목), paged.
func TestGetLawByAbbreviation(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"lawSearch?query=중대재해처벌법": "search_none.xml",
		"lawSearch?query=중대":      "search_prefix.xml",
		"lawSearch?LID=013993":    "versions_serious.xml",
		"lawService?MST=228817":   "law_serious_full.xml",
	})
	var got Law
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "중대재해처벌법", "page_size": 5}, &got); e != nil {
		t.Fatal(e)
	}
	if got.LawID != "013993" || got.PageInfo.TotalCount != 16 || len(got.Articles) != 5 || got.Upcoming != nil {
		t.Fatalf("got %+v", got)
	}
	a := got.Articles[1]
	if a.Number != "제2조" || a.Chapter != "제1장 총칙" || !strings.Contains(a.Text, "\n1. \"중대재해\"란") || !strings.Contains(a.Text, "\n가. 사망자가 1명 이상 발생") {
		t.Errorf("제2조: %+v", a)
	}
	if strings.Contains(got.Articles[0].Text, "제1장") {
		t.Errorf("chapter heading in text: %q", got.Articles[0].Text)
	}
}

func TestGetLawValidation(t *testing.T) {
	_, cs := setup(t, map[string]string{"lawSearch?LID=999999": "search_none.xml"})
	for _, args := range []map[string]any{
		{"law": ""},
		{"law": "001872", "article": "제오십조"},
		{"law": "001872", "article": "50항"},
		{"law": "001872", "as_of": "20261028"},
		{"law": "001872", "page_size": 101},
	} {
		if e := call(t, cs, "lawgokr_get_law", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	if e := call(t, cs, "lawgokr_get_law", map[string]any{"law": "999999"}, nil); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown law_id: got %+v", e)
	}
	if e := call(t, cs, "lawgokr_search_laws", map[string]any{"query": " "}, nil); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Errorf("empty query: got %+v", e)
	}
}

// A bad OC and an unregistered IP are the same HTTP 200 document.
func TestErrors(t *testing.T) {
	_, cs := setup(t, map[string]string{"lawSearch": "error_bad_oc.xml"})
	e := call(t, cs, "lawgokr_search_laws", map[string]any{"query": "민법"}, nil)
	if e == nil || e.Code != hub.CodeUnauthenticated || !strings.Contains(e.Hint, "IP") || strings.Contains(e.Message+e.Hint, testOC) {
		t.Errorf("bad OC: got %+v", e)
	}

	b, _ := os.ReadFile("testdata/error_no_law.xml")
	if e := hub.AsError(apiError(b)); e == nil || e.Code != hub.CodeNotFound || !strings.Contains(e.Message, "일치하는 법령이 없습니다") {
		t.Errorf("no law: got %v", e)
	}
	b, _ = os.ReadFile("testdata/law_labor_50.xml")
	if err := apiError(b); err != nil {
		t.Errorf("normal document: %v", err)
	}
}
