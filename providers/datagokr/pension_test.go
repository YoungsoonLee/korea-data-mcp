package datagokr

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

func pensionFixtures(search string) map[string]string {
	return map[string]string{
		"getBassInfoSearchV2": search,
		// Other months and the sites get these; the head office's latest
		// month has its own.
		"getDetailInfoSearchV2":                   "nps_detail_samsung_202608.xml",
		"getDetailInfoSearchV2?seq=6509296":       "nps_detail_samsung_202609.xml",
		"getDetailInfoSearchV2?seq=6416987":       "nps_detail_site_withdrawn.xml",
		"getPdAcctoSttusInfoSearchV2":             "nps_status_site.xml",
		"getPdAcctoSttusInfoSearchV2?seq=6509296": "nps_status_samsung_202609.xml",
	}
}

func TestPensionWorkplaces(t *testing.T) {
	f, cs := setup(t, pensionFixtures("nps_search_samsung.xml"))

	var got PensionWorkplaces
	if e := call(t, cs, "datagokr_get_pension_workplaces", map[string]any{
		"business_number": "124-81-00998", "name": "삼성 전자(주)", "history": true,
	}, &got); e != nil {
		t.Fatal(e)
	}
	// Only the first 6 digits match; (주) and spaces match nothing.
	if q := f.query("getBassInfoSearchV2"); q.Get("bzowrRgstNo") != "124810" || q.Get("wkplNm") != "삼성전자" || q.Get("numOfRows") != "100" {
		t.Errorf("query: %v", q)
	}
	// 58 records: the head office in each of 12 months, and 46 short-term
	// sites.
	if got.AsOf != "2026-09" || got.PageInfo.TotalCount != 47 || len(got.Items) != 10 {
		t.Fatalf("as_of %s, total %d, items %d", got.AsOf, got.PageInfo.TotalCount, len(got.Items))
	}
	hq := got.Items[0]
	if hq.Name != "삼성전자(주)" || hq.Kind != "workplace" || hq.Status != "enrolled" || hq.Month != "2026-09" || hq.BusinessNumberPrefix != "124810" {
		t.Errorf("head office: %+v", hq)
	}
	if *hq.Subscribers != 126090 || *hq.MonthlyBilled != 78093096440 || *hq.NewSubscribers != 656 || *hq.LostSubscribers != 341 {
		t.Errorf("figures: %+v", hq)
	}
	if hq.Industry != "비디오 및 기타 영상 기기 제조업" || hq.JoinedDate != "1995-05-01" || hq.WithdrawnDate != "" ||
		hq.Corporation == nil || !*hq.Corporation || hq.LegalDongCode != "41117101" || hq.Address != "경기도 수원시 영통구 삼성로" {
		t.Errorf("profile: %+v", hq)
	}
	if len(hq.History) != 12 || hq.History[0].Month != "2025-10" || hq.History[11].Month != "2026-09" ||
		*hq.History[11].Subscribers != 126090 || *hq.History[10].Subscribers != 125843 {
		t.Errorf("history: %+v", hq.History)
	}
	for _, w := range got.Items[1:] {
		if w.Kind != "site" {
			t.Errorf("after the head office come the sites: %+v", w)
		}
	}
	if len(got.Items[2].History) == 0 || len(got.Items[3].History) != 0 {
		t.Errorf("history for the first 3 only")
	}
	if !strings.Contains(got.Note, "two months") || !strings.Contains(got.Note, "ceiling") {
		t.Errorf("note: %s", got.Note)
	}
}

func TestPensionWorkplaceWithdrawnSite(t *testing.T) {
	_, cs := setup(t, pensionFixtures("nps_search_samsung.xml"))
	var got PensionWorkplaces
	if e := call(t, cs, "datagokr_get_pension_workplaces", map[string]any{"business_number": "124810", "name": "삼성전자", "page_size": 20}, &got); e != nil {
		t.Fatal(e)
	}
	for _, w := range got.Items {
		if !strings.Contains(w.Name, "청노화 메타융합관") {
			continue
		}
		// Listed until 2026-08, withdrawn on 2026-07-01.
		if w.Status != "withdrawn" || w.Month != "2026-08" || w.WithdrawnDate != "2026-07-01" || *w.Subscribers != 0 || w.History != nil {
			t.Errorf("withdrawn site: %+v", w)
		}
		return
	}
	t.Errorf("site not on the first page")
}

func TestPensionWorkplacesByNameOnly(t *testing.T) {
	_, cs := setup(t, pensionFixtures("nps_search_name_only.xml"))
	var got PensionWorkplaces
	if e := call(t, cs, "datagokr_get_pension_workplaces", map[string]any{"name": "삼성전자"}, &got); e != nil {
		t.Fatal(e)
	}
	// 1,739 records; reading stops after 1,000.
	if !strings.Contains(got.Note, "matched 1739 records; only the latest 1000 were read") {
		t.Errorf("note: %s", got.Note)
	}
	// A bare name matches cafés, daycare centers and contractors' sites at
	// Samsung plants, under other business numbers.
	var others int
	for _, w := range got.Items {
		if w.BusinessNumberPrefix != "124810" {
			others++
		}
	}
	if len(got.Items) != 10 || others != 10 {
		t.Errorf("items: %d, other companies: %d", len(got.Items), others)
	}
}

func TestPensionWorkplacesNone(t *testing.T) {
	_, cs := setup(t, pensionFixtures("nps_search_none.xml"))
	var got PensionWorkplaces
	if e := call(t, cs, "datagokr_get_pension_workplaces", map[string]any{"business_number": "2208162517", "name": "NAVER"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Items == nil || len(got.Items) != 0 || got.PageInfo.TotalCount != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestPensionWorkplacesErrors(t *testing.T) {
	_, cs := setup(t, pensionFixtures("nps_search_none.xml"))
	for _, args := range []map[string]any{
		{},
		{"name": "(주)"},
		{"business_number": "12345"},
		{"business_number": "124-81-0099X"},
		{"name": "삼성전자", "page_size": 21},
	} {
		if e := call(t, cs, "datagokr_get_pension_workplaces", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
}

func TestWorkplaceKind(t *testing.T) {
	for name, want := range map[string]string{
		"삼성전자(주)":   "workplace",
		"주식회사 상용건설": "workplace",
		"일용산업(주)":   "workplace",
		"삼성전자(주)/일용/대구매곡초등학교 냉난방기 구입 설치":  "site",
		"삼성전자(주)-(일용)장흥군 여흥두드림센터":         "site",
		"삼성전자(주）(일용)신병교육대 교육생활관":          "site",
		"(주)도원테크/상용/평택삼성전자 P4 Ph4 HookUp": "site",
		"(주)엑사이엔씨(상용)네이버세종데이터센터":          "site",
	} {
		if got := workplaceKind(name); got != want {
			t.Errorf("workplaceKind(%q) = %q, want %q", name, got, want)
		}
	}
}

// numOfRows above 100 is a bare CLIENT_ERROR; the tool never sends it, but
// the code must not pass for success.
func TestPensionPageSizeError(t *testing.T) {
	c, _ := serve(t, 200, "nps_error_rows.xml")
	_, _, err := getList[npsRecord](context.Background(), c, npsSearchPath, url.Values{}, 1, 101)
	if e := hub.AsError(err); e == nil || e.UpstreamCode != "97" {
		t.Errorf("got %v", err)
	}
}
