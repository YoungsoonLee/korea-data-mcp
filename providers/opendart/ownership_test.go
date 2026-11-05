package opendart

import (
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// SK hynix: SK Square holds 20%, BlackRock 5.00%; Capital Research fell below
// 5% and The Capital Group filed 0% when Capital Research became the
// representative reporter. The National Pension Service last reported in
// 2024-09, before the two years majorstock.json returns.
func TestGetOwnershipMajorHolders(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("list.json", fixture(t, "list_D001.json"))
	var got OwnershipReports
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "000660"}, &got))
	if q := f.query("majorstock.json"); q.Get("corp_code") != "00164779" {
		t.Errorf("query: %v", q)
	}
	if q := f.query("list.json"); q.Get("pblntf_detail_ty") != "D001" || q.Get("bgn_de") != "20150101" || q.Get("corp_code") != "00164779" {
		t.Errorf("list query: %v", q)
	}
	if got.CorpName != "SK하이닉스" || got.Kind != "major_holders" || got.Note == "" || len(got.Reports) != 9 || got.PageInfo.TotalCount != 9 {
		t.Fatalf("got %s %s, %d reports", got.CorpName, got.Kind, len(got.Reports))
	}
	want := []struct {
		reporter, status string
		percent          float64
	}{
		{"SK스퀘어", "holding", 20.01},
		{"BlackRockFundAdvisors", "holding", 5},
		{"CapitalResearchandManagementCompany", "below_5_percent", 3.53},
		{"TheCapitalGroupCompanies,Inc.", "exited", 0},
	}
	if len(got.Holders) != len(want) {
		t.Fatalf("holders: %+v", got.Holders)
	}
	for i, w := range want {
		if h := got.Holders[i]; h.Reporter != w.reporter || h.Status != w.status || *h.Percent != w.percent {
			t.Errorf("holder %d: %+v", i, h)
		}
	}
	if h := got.Holders[0]; *h.Shares != 146_174_364 || h.Date != "2026-08-05" || h.ReportType != "일반" || h.URL == "" {
		t.Errorf("SK스퀘어: %+v", h)
	}
	if len(got.OlderReporters) != 2 || got.OlderReporters[0].Reporter != "국민연금공단" || got.OlderReporters[0].Date != "2024-09-25" || got.OlderReporters[1].Reporter != "SK텔레콤" {
		t.Errorf("older: %+v", got.OlderReporters)
	}
	r := got.Reports[1] // Capital Research selling below 5%
	if r.Date != "2026-06-09" || *r.SharesChange != -11_581_573 || *r.PercentChange != -1.52 || r.ReportType != "약식" || r.Reason == "" || r.Position != "" {
		t.Errorf("report: %+v", r)
	}
}

func TestGetOwnershipMajorHoldersFilters(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("list.json", fixture(t, "list_D001.json"))
	var got OwnershipReports
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "000660", "reporter": "capital research", "direction": "decrease"}, &got))
	if len(got.Holders) != 1 || got.Holders[0].Status != "below_5_percent" || len(got.Reports) != 2 || got.OlderReporters != nil {
		t.Errorf("got %+v", got)
	}
	var older OwnershipReports
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "000660", "reporter": "국민연금"}, &older))
	if len(older.Holders) != 0 || len(older.Reports) != 0 || len(older.OlderReporters) != 1 {
		t.Errorf("older only: %+v", older)
	}
}

// Samsung insiders: positions with trailing spaces, executives with a few
// hundred shares filed as 10%이상주주, and 이재용 as 사실상지배주주.
func TestGetOwnershipInsiders(t *testing.T) {
	f, cs := setup(t, testKey)
	var got OwnershipReports
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "005930", "kind": "insiders"}, &got))
	if f.count("majorstock.json") != 0 || f.count("list.json") != 0 {
		t.Error("insiders should not fetch 5% reports")
	}
	if got.Kind != "insiders" || got.Holders != nil || got.PageInfo.TotalCount != 19 || got.Reports[0].Reporter != "이재용" {
		t.Fatalf("got %+v", got.PageInfo)
	}
	r := got.Reports[0]
	if r.Position != "회장" || r.MainShareholder != "사실상지배주주" || *r.Registered || *r.Shares != 105_219_053 || *r.SharesChange != 7_667_100 || *r.Percent != 1.58 || r.ReportType != "" {
		t.Errorf("이재용: %+v", r)
	}
	for _, r := range got.Reports {
		if r.Position == "부사장 " || r.MainShareholder == "-" {
			t.Errorf("not cleaned: %+v", r)
		}
	}

	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "005930", "kind": "insiders", "direction": "decrease", "end_date": "2025-06-30"}, &got))
	if got.PageInfo.TotalCount != 3 {
		t.Errorf("decrease: %d", got.PageInfo.TotalCount)
	}
	for _, r := range got.Reports {
		if *r.SharesChange >= 0 || r.Date > "2025-06-30" {
			t.Errorf("filter: %+v", r)
		}
	}
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "005930", "kind": "insiders", "reporter": "정현호", "start_date": "2026-01-01"}, &got))
	if got.PageInfo.TotalCount != 3 || !(*got.Reports[0].Shares == 45_978 && *got.Reports[2].Shares == 26_787) {
		t.Errorf("정현호: %+v", got.Reports)
	}
}

func TestGetOwnershipErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"corp_code": "005930", "kind": "pension"},
		{"corp_code": "005930", "direction": "up"},
		{"corp_code": "005930", "start_date": "20250101"},
	} {
		if e := call(t, cs, "opendart_get_ownership_reports", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	none := `{"status":"013","message":"조회된 데이타가 없습니다."}`
	f.set("majorstock.json", none)
	f.set("list.json", none)
	var got OwnershipReports
	mustOK(t, call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "005930"}, &got))
	if got.Reports == nil || len(got.Reports) != 0 || len(got.Holders) != 0 {
		t.Errorf("none: %+v", got)
	}
	f.set("elestock.json", `{"status":"020","message":"요청 제한을 초과하였습니다."}`)
	if e := call(t, cs, "opendart_get_ownership_reports", map[string]any{"corp_code": "005930", "kind": "insiders"}, nil); e == nil || e.Code != hub.CodeRateLimited {
		t.Errorf("rate limited: %+v", e)
	}
}
