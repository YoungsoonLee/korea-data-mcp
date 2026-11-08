package opendart

import (
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

func fullFinancials(t *testing.T, fx, company string, args map[string]any) (*fakeDART, Financials) {
	t.Helper()
	f, cs := setup(t, testKey)
	f.set("fnlttSinglAcntAll.json", fixture(t, fx))
	if company != "" {
		f.set("company.json", fixture(t, company))
	}
	args["scope"] = "full"
	var got Financials
	mustOK(t, call(t, cs, "opendart_get_financials", args, &got))
	return f, got
}

func accountByID(got Financials, typ, id string) *Account {
	for i, a := range got.Accounts {
		if a.StatementType == typ && a.AccountID == id {
			return &got.Accounts[i]
		}
	}
	return nil
}

// Samsung Electronics 2025: rows come ordered by account ID (영업이익 before
// 매출액), 당기순이익 in both IS and CIS, outflows as positive amounts.
func TestGetFullFinancials(t *testing.T) {
	f, got := fullFinancials(t, "fnlttSinglAcntAll.json", "", map[string]any{"corp_code": "005930", "year": 2025})
	if q := f.query("fnlttSinglAcntAll.json"); q.Get("fs_div") != "CFS" || q.Get("reprt_code") != "11011" || q.Get("bsns_year") != "2025" {
		t.Errorf("query: %v", q)
	}
	if got.Scope != "full" || got.Currency != "KRW" || got.ReceiptNo != "20260310002820" || got.Note != cashFlowNote {
		t.Errorf("header: %+v", got)
	}
	var order []string
	profit := 0
	for _, a := range got.Accounts {
		if a.StatementType == "equity" {
			t.Fatal("equity only when asked")
		}
		if a.StatementType == "income_statement" && len(order) < 5 {
			order = append(order, a.Name)
		}
		if a.AccountID == "ifrs-full_ProfitLoss" && a.StatementType == "income_statement" {
			profit++
		}
	}
	if got.Accounts[0].AccountID != "ifrs-full_Assets" || profit != 1 {
		t.Errorf("first %+v, 당기순이익 %d times", got.Accounts[0], profit)
	}
	if want := "매출액,매출원가,매출총이익,판매비와관리비,영업이익"; strings.Join(order, ",") != want {
		t.Errorf("order: %s", strings.Join(order, ","))
	}
	rev := accountByID(got, "income_statement", "ifrs-full_Revenue")
	if *rev.Current.Amount != 333_605_938_000_000 || rev.Current.Start != "2025-01-01" || rev.Current.End != "2025-12-31" ||
		rev.Prior.End != "2024-12-31" || *rev.BeforePrior.Amount != 258_935_494_000_000 || rev.BeforePrior.Start != "2023-01-01" || rev.CurrentQuarter != nil {
		t.Errorf("revenue: %+v %+v %+v", rev.Current, rev.Prior, rev.BeforePrior)
	}
	if a := accountByID(got, "balance_sheet", "ifrs-full_Assets"); a.Current.Start != "" || a.Current.End != "2025-12-31" || a.Prior.End != "2024-12-31" || a.BeforePrior.End != "2023-12-31" {
		t.Errorf("assets: %+v", a)
	}
	div := accountByID(got, "cash_flow", "ifrs-full_DividendsPaidClassifiedAsFinancingActivities")
	fin := accountByID(got, "cash_flow", "ifrs-full_CashFlowsFromUsedInFinancingActivities")
	if div.Flow != "outflow" || *div.Current.Amount != 9_897_183_000_000 || fin.Flow != "" || *fin.Current.Amount != -13_478_040_000_000 {
		t.Errorf("cash flow: %+v %+v", div, fin)
	}
	var unnamed int
	for _, a := range got.Accounts {
		if a.AccountID == "" {
			unnamed++
		}
	}
	if unnamed == 0 {
		t.Error("company-specific lines should have no account_id")
	}

	_, got = fullFinancials(t, "fnlttSinglAcntAll.json", "", map[string]any{"corp_code": "005930", "year": 2025, "statements": []string{"equity"}, "account": "기초자본"})
	components := map[string]int64{}
	for _, a := range got.Accounts {
		components[a.Component] = *a.Current.Amount
	}
	if len(got.Accounts) != 7 || components["합계"] != 402_192_070_000_000 || components["이익잉여금"] != 370_513_188_000_000 || got.Note != "" {
		t.Errorf("equity: %+v", components)
	}
}

// SK hynix half-year: income amounts are the quarter plus a year-to-date
// column, cash flows are year-to-date in thstrm_amount.
func TestGetFullFinancialsHalf(t *testing.T) {
	_, got := fullFinancials(t, "fnlttSinglAcntAll_half.json", "", map[string]any{"corp_code": "000660", "year": 2025, "report": "half"})
	rev := accountByID(got, "income_statement", "ifrs-full_Revenue")
	if *rev.Current.Amount != 131_895_033_000_000 || rev.Current.Start != "2025-01-01" || rev.Current.End != "2025-06-30" ||
		*rev.CurrentQuarter.Amount != 79_318_746_000_000 || rev.CurrentQuarter.Start != "2025-04-01" ||
		*rev.Prior.Amount != 39_871_093_000_000 || rev.Prior.End != "2024-06-30" || *rev.PriorQuarter.Amount != 22_231_952_000_000 {
		t.Errorf("revenue: %+v", rev)
	}
	cf := accountByID(got, "cash_flow", "ifrs-full_CashFlowsFromUsedInOperatingActivities")
	if *cf.Current.Amount != 91_742_501_000_000 || cf.Current.Start != "2025-01-01" || cf.CurrentQuarter != nil || *cf.Prior.Amount != 18_189_841_000_000 || cf.Prior.Start != "2024-01-01" {
		t.Errorf("cash flow: %+v %+v", cf.Current, cf.Prior)
	}
	if a := accountByID(got, "balance_sheet", "ifrs-full_Assets"); a.Current.End != "2025-06-30" || a.Prior.End != "2024-12-31" {
		t.Errorf("assets: %+v", a)
	}
}

// 동원모빌리티 closes its year in March: the first quarter of bsns_year 2025
// is April to June 2025.
func TestGetFullFinancialsMarchYearEnd(t *testing.T) {
	_, got := fullFinancials(t, "fnlttSinglAcntAll_march.json", "company_march.json", map[string]any{"corp_code": "00118008", "year": 2025, "report": "q1", "statement": "separate"})
	rev := accountByID(got, "income_statement", "ifrs-full_Revenue")
	if rev.Current.Start != "2025-04-01" || rev.Current.End != "2025-06-30" || *rev.Current.Amount != 95_769_228_433 || rev.CurrentQuarter != nil || rev.Prior.Start != "2024-04-01" {
		t.Errorf("revenue: %+v %+v", rev.Current, rev.Prior)
	}
	if a := accountByID(got, "balance_sheet", "ifrs-full_Assets"); a.Current.End != "2025-06-30" || a.Prior.End != "2025-03-31" {
		t.Errorf("assets: %+v %+v", a.Current, a.Prior)
	}
	if got.Note == "" || got.Note[:30] != "The fiscal year ends in month " {
		t.Errorf("note: %q", got.Note)
	}
}

func TestGetFullFinancialsFinancialAndUSD(t *testing.T) {
	_, got := fullFinancials(t, "fnlttSinglAcntAll_financial.json", "", map[string]any{"corp_code": "00688996", "year": 2025, "statements": []string{"income_statement"}})
	if got.Note != noRevenueNote || accountByID(got, "income_statement", "ifrs-full_Revenue") != nil {
		t.Errorf("KB금융: %q", got.Note)
	}
	_, got = fullFinancials(t, "fnlttSinglAcntAll_usd.json", "", map[string]any{"corp_code": "01442115", "year": 2025, "account": "주당"})
	eps := accountByID(got, "income_statement", "ifrs-full_BasicEarningsLossPerShare")
	if got.Currency != "USD" || len(got.Accounts) != 2 || eps.Current.Amount != nil || *eps.Current.Decimal != -0.11 || *eps.BeforePrior.Decimal != -0.17 || got.Note != "" {
		t.Errorf("소마젠: %+v %+v", got, eps)
	}
}

func TestGetFullFinancialsErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"corp_code": "005930", "year": 2025, "scope": "everything"},
		{"corp_code": "005930", "year": 2025, "scope": "full", "statements": []string{"notes"}},
		{"corp_code": "005930", "year": 2025, "account": "매출"},
	} {
		if e := call(t, cs, "opendart_get_financials", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	f.set("fnlttSinglAcntAll.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	if e := call(t, cs, "opendart_get_financials", map[string]any{"corp_code": "005930", "year": 2025, "scope": "full"}, nil); e == nil || e.Code != hub.CodeNotFound || e.Hint == "" {
		t.Errorf("not found: %+v", e)
	}
}

func TestFiscalPeriods(t *testing.T) {
	for _, c := range []struct {
		year, month                int
		report                     string
		start, quarter, end, prior string
	}{
		{2025, 12, "annual", "2025-01-01", "2025-10-01", "2025-12-31", "2024-12-31"},
		{2025, 12, "q3", "2025-01-01", "2025-07-01", "2025-09-30", "2024-12-31"},
		{2026, 3, "annual", "2025-04-01", "2026-01-01", "2026-03-31", "2025-03-31"},
		{2025, 3, "half", "2025-04-01", "2025-07-01", "2025-09-30", "2025-03-31"},
		{2025, 6, "q1", "2025-07-01", "2025-07-01", "2025-09-30", "2025-06-30"},
	} {
		p := fiscalPeriods(c.year, c.month, c.report)
		if p.start != c.start || p.quarterStart != c.quarter || p.end != c.end || p.priorYearEnd != c.prior {
			t.Errorf("%d/%d %s: %+v", c.year, c.month, c.report, p)
		}
	}
	if a := shiftYears("2024-03-01", "2024-02-29", -1); a.Start != "2023-03-01" || a.End != "2023-02-28" {
		t.Errorf("leap year: %+v", a)
	}
}
