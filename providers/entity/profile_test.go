package entity

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/providers/datagokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
)

// Live values from 2026-11-16 to 2026-11-18, added to the fake's tables.
func init() {
	// Files separate statements only.
	ehwa := opendart.CompanyProfile{CorpCode: "00145668", CorpName: "이화공영(주)", StockName: "이화공영", StockCode: "001840", Market: "KOSDAQ",
		CorporateRegistration: "1101110033839", BusinessRegistration: "1058111500", FiscalYearEndMonth: 12}
	// Unlisted; files audit reports only.
	display := opendart.CompanyProfile{CorpCode: "00912006", CorpName: "삼성디스플레이 주식회사", StockName: "삼성디스플레이", Market: "OTHER",
		CorporateRegistration: "1345110187812", BusinessRegistration: "1428145237", FiscalYearEndMonth: 12}
	// Merged into 제일모직 in 2015, which took its name.
	oldCNT := opendart.CompanyProfile{CorpCode: "00126229", CorpName: "삼성물산(주)", StockName: "삼성물산", StockCode: "000830", Market: "OTHER",
		CorporateRegistration: "1101110002975", BusinessRegistration: "1048126728", FiscalYearEndMonth: 12}
	for _, p := range []opendart.CompanyProfile{ehwa, display, oldCNT} {
		dartProfiles[p.CorpCode] = p
		if p.StockCode != "" {
			dartProfiles[p.StockCode] = p
		}
	}
	kakao.FiscalYearEndMonth, hanwhaOcean.FiscalYearEndMonth = 12, 12
	for _, k := range []string{"00258801", "035720"} {
		dartProfiles[k] = kakao
	}
	dartProfiles["00111704"] = hanwhaOcean
	dartSearch["삼성디스플레이"] = []opendart.Company{{CorpCode: "00912006", CorpName: "삼성디스플레이"}}

	fscProfiles["1345110187812"] = datagokr.CorporationProfile{Corporation: datagokr.Corporation{Crno: "1345110187812", Name: "삼성디스플레이 주식회사",
		BizRegNo: "1428145237", DartCorpCode: "00912006", Market: "OTHER", AsOf: "2026-11-16"}, Employees: new(int64(24985)), AvgSalary: new(int64(78000000))}
	fscProfiles["1101110002975"] = datagokr.CorporationProfile{Corporation: datagokr.Corporation{Crno: "1101110002975", Name: "삼성물산(주)",
		BizRegNo: "1048126728", DartCorpCode: "00126229", Market: "OTHER", AsOf: "2026-11-16"}, Employees: new(int64(8663)), DelistedDate: "2015-09-15"}
	kp := fscProfiles["1101111129497"]
	kp.Employees, kp.ListedDate, kp.Auditor = new(int64(3922)), "2017-07-10", "삼정회계법인"
	fscProfiles["1101111129497"] = kp

	nts["1058111500"] = datagokr.BusinessStatus{Registered: true, Status: "active"}
	nts["1428145237"] = datagokr.BusinessStatus{Registered: true, Status: "active"}
	nts["1048126728"] = datagokr.BusinessStatus{Registered: true, Status: "closed", ClosedDate: "2015-09-02"}
}

func amount(n int64) opendart.Amount { return opendart.Amount{End: "2025-12-31", Amount: &n} }

var (
	prices = map[string][]datagokr.StockPrice{
		"035720": {{Date: "2026-11-16", Close: new(int64(32450)), ChangeRate: new(2.37), MarketCap: new(int64(14375366549500)), ListedShares: new(int64(443000510))}},
		"042660": {{Date: "2026-11-16", Close: new(int64(46800)), ChangeRate: new(-3.21), MarketCap: new(int64(14340146839200)), ListedShares: new(int64(306413394))}},
	}
	financials = map[string]opendart.Financials{
		"00258801/2025/consolidated": {CorpCode: "00258801", Year: 2025, Statement: "consolidated", Currency: "KRW", ReceiptNo: "20260318001423", Accounts: []opendart.Account{
			{Name: "유동자산", Current: amount(12374175844165)},
			{Name: "자산총계", Current: amount(27783524545811), Prior: new(amount(25773028304462))},
			{Name: "매출액", Current: opendart.Amount{Start: "2025-01-01", End: "2025-12-31", Amount: new(int64(8099147815086))}},
			{Name: "영업이익", Current: amount(732036535379)},
			{Name: "당기순이익(손실)", Current: amount(517959587282)},
		}},
		"00258801/2024/consolidated": {CorpCode: "00258801", Year: 2024, Statement: "consolidated", Currency: "KRW", ReceiptNo: "20250324000901", Accounts: []opendart.Account{
			{Name: "매출액", Current: opendart.Amount{Start: "2024-01-01", End: "2024-12-31", Amount: new(int64(7864033326475))}},
		}},
		"00145668/2025/separate": {CorpCode: "00145668", Year: 2025, Statement: "separate", Currency: "KRW", ReceiptNo: "20260325001443", Accounts: []opendart.Account{
			{Name: "매출액", Current: amount(115190727605)},
			{Name: "영업이익", Current: amount(3290259291)},
		}},
	}
	financialsErr = map[string]error{
		"00145668/2025/consolidated": hub.Errorf(hub.CodeNotFound, "no consolidated statements for 00145668 2025 annual"),
	}
	disclosures = map[string]hub.List[opendart.Disclosure]{
		"00258801/major_report": {Items: []opendart.Disclosure{
			{ReceivedDate: "2026-11-05", ReportName: "[기재정정]주요사항보고서(회사분할결정)", ReceiptNo: "20261105000098"},
			{ReceivedDate: "2026-11-05", ReportName: "주요사항보고서(자기주식취득결정)", ReceiptNo: "20261105000008"},
		}, PageInfo: hub.PageInfo{TotalCount: 6}},
		"00258801/":      {Items: []opendart.Disclosure{{ReceivedDate: "2026-11-13", ReportName: "분기보고서 (2026.09)", ReceiptNo: "20261113001344"}}, PageInfo: hub.PageInfo{TotalCount: 14}},
		"00912006/audit": {Items: []opendart.Disclosure{{ReceivedDate: "2026-03-18", ReportName: "연결감사보고서 (2025.12)", ReceiptNo: "20260318000378"}}, PageInfo: hub.PageInfo{TotalCount: 4}},
	}
	ownership = map[string]opendart.OwnershipReports{
		"00258801": {Holders: []opendart.BlockHolder{
			{Reporter: "김범수", Status: "holding", Percent: new(24.16), Date: "2025-07-25", ReceiptNo: "20250725000765"},
			{Reporter: "국민연금공단", Status: "holding", Percent: new(5.39), Date: "2026-07-01", ReceiptNo: "20260701000328"},
		}},
		"00111704": {Holders: []opendart.BlockHolder{
			{Reporter: "한화에어로스페이스", Status: "holding", Percent: new(54.34), Date: "2026-10-19", ReceiptNo: "20261019000344"},
			{Reporter: "국민연금공단", Status: "holding", Percent: new(5.64), Date: "2026-11-02", ReceiptNo: "20261102000290"},
		}, OlderReporters: []opendart.OlderReporter{{Reporter: "한국산업은행", Date: "2023-05-25"}, {Reporter: "HD한국조선해양", Date: "2022-03-08"}}},
	}
	pension = map[string]datagokr.PensionWorkplaces{
		"1208147521": {AsOf: "2026-09", Items: []datagokr.PensionWorkplace{
			{Name: "주식회사 카카오", Kind: "workplace", Status: "enrolled", Month: "2026-09", Subscribers: new(int64(3788)), MonthlyBilled: new(int64(2216917060))},
		}},
		// The company's own workplace, a site filed under its name, and
		// another company sharing the first 6 digits.
		"1048157667": {AsOf: "2026-09", Items: []datagokr.PensionWorkplace{
			{Name: "한화오션(주)", Kind: "workplace", Status: "enrolled", Month: "2026-09", Subscribers: new(int64(10241))},
			{Name: "한화오션 (주)-(상용)HAS MCS PJ", Kind: "site", Status: "enrolled", Month: "2026-09", Subscribers: new(int64(31))},
			{Name: "한화오션에코텍(주)", Kind: "workplace", Status: "enrolled", Month: "2026-09", Subscribers: new(int64(500))},
		}},
	}
)

func profile(t *testing.T, f *fake, q string, today string) (Profile, *hub.Error) {
	t.Helper()
	now, _ := time.ParseInLocation(time.DateOnly, today, kst)
	got, err := (&resolver{call: f.call, now: func() time.Time { return now.Add(12 * time.Hour) }}).profile(context.Background(), profileInput{Company: q})
	return got, hub.AsError(err)
}

func TestProfileSharedName(t *testing.T) {
	f := &fake{}
	got, e := profile(t, f, "카카오", "2026-11-17")
	if e != nil {
		t.Fatal(e)
	}
	if got.EntityID != "crno:1101111129497" || got.AsOf != "2026-11-17" || len(got.OtherMatches) != 2 || got.OtherMatches[1].BusinessStatus != "closed" {
		t.Fatalf("got %s, others %+v", got.EntityID, got.OtherMatches)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "3 companies have this name") {
		t.Errorf("notes %q", got.Notes)
	}
	o := got.Overview
	if o == nil || !slices.Equal(o.Sources, []string{"opendart_get_company", "datagokr_get_corporation"}) || *o.Employees != 3922 || o.ListedDate != "2017-07-10" || o.FiscalYearEndMonth != 12 {
		t.Errorf("overview %+v", o)
	}
	fin := got.Financials
	var names []string
	for _, a := range fin.Accounts {
		names = append(names, a.Name)
	}
	// Main lines only, in a fixed order.
	if fin.Year != 2025 || fin.Statement != "consolidated" || fin.ReceiptNo != "20260318001423" || fin.Start != "2025-01-01" || fin.End != "2025-12-31" ||
		strings.Join(names, ",") != "매출액,영업이익,당기순이익(손실),자산총계" || *fin.Accounts[3].Prior != 25773028304462 {
		t.Errorf("financials %+v", fin)
	}
	if s := got.Stock; s == nil || s.Date != "2026-11-16" || *s.Close != 32450 || *s.MarketCap != 14375366549500 {
		t.Errorf("stock %+v", s)
	}
	if h := got.MajorHolders; h == nil || len(h.Holders) != 2 || h.Holders[0].Reporter != "김범수" {
		t.Errorf("holders %+v", h)
	}
	if m := got.MajorReports; m == nil || m.Total != 6 || len(m.Items) != 2 || m.Since != "2025-11-17" || m.Items[0].ReceiptNo != "20261105000098" {
		t.Errorf("major reports %+v", m)
	}
	if r := got.Recent; r == nil || r.Total != 14 || r.Since != "2026-10-18" {
		t.Errorf("recent %+v", r)
	}
	if p := got.Pension; p == nil || p.Subscribers != 3788 || p.Workplaces != 1 || p.Month != "2026-09" {
		t.Errorf("pension %+v", p)
	}
	if got.Incomplete != nil || got.AuditReport != nil {
		t.Errorf("incomplete %v, audit %+v", got.Incomplete, got.AuditReport)
	}
	// The profiles fetched while resolving are not fetched again.
	if n := len(f.called(`opendart_get_company {"corp_code":"00258801"}`)); n != 1 {
		t.Errorf("DART profile fetched %d times", n)
	}
}

func TestProfileByEntityID(t *testing.T) {
	for _, q := range []string{"crno:1101112095837", "dart:00111704", "bizno:1048157667", "대우조선해양"} {
		got, e := profile(t, &fake{}, q, "2026-11-17")
		if e != nil {
			t.Fatalf("%s: %v", q, e)
		}
		if got.Name != "한화오션(주)" || got.OtherMatches != nil || got.Stock == nil || got.MajorHolders.OlderReporters != 2 {
			t.Errorf("%s: %+v", q, got)
		}
		// A construction site and another company with the same first 6
		// digits are not the company's workplaces.
		if p := got.Pension; p == nil || p.Subscribers != 10241 || p.Workplaces != 1 {
			t.Errorf("%s: pension %+v", q, p)
		}
	}
}

func TestProfileFinancials(t *testing.T) {
	// Before the 2025 report is filed, the 2024 one is used.
	filed := financials["00258801/2025/consolidated"]
	delete(financials, "00258801/2025/consolidated")
	defer func() { financials["00258801/2025/consolidated"] = filed }()
	f := &fake{}
	got, _ := profile(t, f, "00258801", "2026-02-10")
	if got.Financials == nil || got.Financials.Year != 2024 || got.Financials.ReceiptNo != "20250324000901" {
		t.Errorf("before filing: %+v", got.Financials)
	}
	// The unfiled year is not retried as separate.
	if calls := f.called("opendart_get_financials"); len(calls) != 2 {
		t.Errorf("financials calls: %v", calls)
	}
	// Separate statements when there are no consolidated ones.
	f = &fake{}
	got, _ = profile(t, f, "001840", "2026-11-17")
	if got.Financials == nil || got.Financials.Statement != "separate" || got.Financials.ReceiptNo != "20260325001443" {
		t.Errorf("separate: %+v", got.Financials)
	}

	// An unlisted company filing audit reports only: no figures, a link to
	// the latest audit report, and no stock or 5% holders.
	got, e := profile(t, &fake{}, "삼성디스플레이", "2026-11-17")
	if e != nil {
		t.Fatal(e)
	}
	if got.Financials != nil || got.AuditReport == nil || got.AuditReport.ReceiptNo != "20260318000378" || got.Stock != nil || got.MajorHolders != nil ||
		len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "audit reports only") || *got.Overview.AvgSalary != 78000000 {
		t.Errorf("audit only: %+v", got)
	}
}

func TestProfileClosed(t *testing.T) {
	// DART keeps the stock code of the 삼성물산 merged away in 2015, and the
	// FSC registry its last employee count.
	got, e := profile(t, &fake{}, "000830", "2026-11-17")
	if e != nil {
		t.Fatal(e)
	}
	if got.Listed || got.Stock != nil || got.MajorHolders != nil || got.Financials != nil || got.BusinessStatus != "closed" ||
		len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "closed since 2015-09-02") {
		t.Errorf("got %+v", got)
	}
}

func TestProfileWithOneKey(t *testing.T) {
	got, e := profile(t, &fake{disabled: map[string]bool{"datagokr": true}}, "035720", "2026-11-17")
	if e != nil {
		t.Fatal(e)
	}
	if got.Financials == nil || got.MajorHolders == nil || got.Stock != nil || got.Pension != nil ||
		len(got.Incomplete) != 1 || !strings.HasPrefix(got.Incomplete[0], "datagokr") {
		t.Errorf("got %+v", got)
	}
	// Without DART, a company outside it still gets the registry's view.
	got, e = profile(t, &fake{disabled: map[string]bool{"opendart": true}}, "2001110423487", "2026-11-17")
	if e != nil {
		t.Fatal(e)
	}
	if got.Name != "카카오" || got.Overview == nil || got.Financials != nil || got.Recent != nil ||
		!slices.ContainsFunc(got.Notes, func(s string) bool { return strings.Contains(s, "does not file with DART") }) {
		t.Errorf("outside DART: %+v", got)
	}
}

func TestProfileErrors(t *testing.T) {
	if _, e := profile(t, &fake{}, "없는회사", "2026-11-17"); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown: %v", e)
	}
	if _, e := profile(t, &fake{}, "crno:", "2026-11-17"); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Errorf("empty entity_id: %v", e)
	}
}

func TestLastFiscalYear(t *testing.T) {
	for _, tc := range []struct {
		today string
		month int
		want  int
	}{
		{"2026-11-17", 12, 2025},
		{"2026-01-01", 12, 2025},
		{"2026-12-31", 12, 2025},
		{"2026-11-17", 3, 2026}, // 2025-04-01 ~ 2026-03-31
		{"2026-03-31", 3, 2025},
		{"2026-04-01", 3, 2026},
		{"2026-11-17", 0, 2025},
	} {
		d, _ := time.ParseInLocation(time.DateOnly, tc.today, kst)
		if got := lastFiscalYear(d.Add(time.Hour), tc.month); got != tc.want {
			t.Errorf("%s, month %d: %d, want %d", tc.today, tc.month, got, tc.want)
		}
	}
}
