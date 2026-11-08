package opendart

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// ---- opendart_get_financials, scope=full ----
//
// 전체 재무제표 (fnlttSinglAcntAll.json) has every line of the statements.
// Quirks handled here:
//   - Rows are ordered by account ID, not as printed: 영업이익 comes before
//     매출액, 자산총계 before 현금. There is no parent or level field.
//   - Most companies have no separate income statement (IS): revenue and
//     operating income are in the comprehensive income statement (CIS).
//     Companies with both repeat 당기순이익.
//   - Cash flow items are XBRL facts, not the printed signs: dividends paid,
//     capex and buybacks are positive, while the section totals are signed.
//   - In interim reports thstrm_amount is the latest quarter for income
//     items but year-to-date for cash flows, and frmtrm_q_amount of a cash
//     flow is the prior year-to-date despite its name.
//   - Periods are labels ("제 58 기 반기") without dates. For a fiscal year
//     ending in March, bsns_year 2025 is the year ending 2025-03 in an annual
//     report but the year starting 2025-04 in interim reports.
//   - About 8% of rows have no standard ID (-표준계정코드 미사용-), and
//     per-share amounts of USD filers have decimals (-0.17).
//   - The statement of changes in equity is a matrix of events by equity
//     component, about half of all rows.

type fullStatementsResponse struct {
	apiStatus
	List []struct {
		RceptNo         string `json:"rcept_no"`
		SjDiv           string `json:"sj_div"`
		AccountID       string `json:"account_id"`
		AccountNm       string `json:"account_nm"`
		AccountDetail   string `json:"account_detail"`
		ThstrmAmount    string `json:"thstrm_amount"`
		ThstrmAddAmount string `json:"thstrm_add_amount"`
		FrmtrmAmount    string `json:"frmtrm_amount"`
		FrmtrmQAmount   string `json:"frmtrm_q_amount"`
		FrmtrmAddAmount string `json:"frmtrm_add_amount"`
		BfefrmtrmAmount string `json:"bfefrmtrm_amount"`
		Ord             string `json:"ord"`
		Currency        string `json:"currency"`
	} `json:"list"`
}

var fullStatementTypes = map[string]string{
	"BS": "balance_sheet", "IS": "income_statement", "CIS": "income_statement", "CF": "cash_flow", "SCE": "equity",
}

var statementOrder = []string{"balance_sheet", "income_statement", "cash_flow", "equity"}

// keyAccounts are the lines an analyst reads first, in the order they are
// printed. Other lines follow in DART's order.
var keyAccounts = map[string][]string{
	"balance_sheet": {
		"ifrs-full_Assets", "ifrs-full_CurrentAssets", "ifrs-full_CashAndCashEquivalents", "ifrs-full_NoncurrentAssets",
		"ifrs-full_Liabilities", "ifrs-full_CurrentLiabilities", "ifrs-full_NoncurrentLiabilities",
		"ifrs-full_Equity", "ifrs-full_EquityAttributableToOwnersOfParent", "ifrs-full_IssuedCapital",
		"ifrs-full_RetainedEarnings", "ifrs-full_NoncontrollingInterests", "ifrs-full_EquityAndLiabilities",
	},
	"income_statement": {
		"ifrs-full_Revenue", "ifrs-full_CostOfSales", "ifrs-full_GrossProfit", "dart_TotalSellingGeneralAdministrativeExpenses",
		"dart_OperatingIncomeLoss", "dart_OtherGains", "dart_OtherLosses", "ifrs-full_FinanceIncome", "ifrs-full_FinanceCosts",
		"ifrs-full_ShareOfProfitLossOfAssociatesAndJointVenturesAccountedForUsingEquityMethod", "ifrs-full_ProfitLossBeforeTax",
		"ifrs-full_IncomeTaxExpenseContinuingOperations", "ifrs-full_ProfitLoss", "ifrs-full_ProfitLossAttributableToOwnersOfParent",
		"ifrs-full_ProfitLossAttributableToNoncontrollingInterests", "ifrs-full_BasicEarningsLossPerShare", "ifrs-full_DilutedEarningsLossPerShare",
		"ifrs-full_OtherComprehensiveIncome", "ifrs-full_ComprehensiveIncome",
	},
	"cash_flow": {
		"ifrs-full_CashFlowsFromUsedInOperatingActivities", "ifrs-full_CashFlowsFromUsedInInvestingActivities",
		"ifrs-full_CashFlowsFromUsedInFinancingActivities", "ifrs-full_EffectOfExchangeRateChangesOnCashAndCashEquivalents",
		"ifrs-full_IncreaseDecreaseInCashAndCashEquivalents", "dart_CashAndCashEquivalentsAtBeginningOfPeriodCf",
		"dart_CashAndCashEquivalentsAtEndOfPeriodCf",
	},
}

// outflowPrefixes mark cash flow lines DART gives as positive amounts that
// are paid out: capex, dividends, repayments, acquisitions.
var outflowPrefixes = []string{"Purchase", "Payments", "Repayments", "AcquisitionOf", "CashFlowsUsedIn", "DividendsPaid", "InterestPaid", "IncomeTaxesPaid"}

const (
	cashFlowNote   = "Cash flow lines are as DART stores them, not as printed: money paid out (capex, dividends, buybacks, repayments) is a positive amount, marked flow=outflow when the account is standard, while section totals are signed."
	noRevenueNote  = "This company has no revenue (매출액) line, as is usual for banks, insurers and securities firms; their income is in lines such as 이자수익, 보험수익 or 영업수익."
	fiscalYearNote = "The fiscal year ends in month %d: in annual reports year is the fiscal year ending in that year, in half and quarterly reports the fiscal year starting in it."
)

func (c *client) getFullFinancials(ctx context.Context, in getFinancialsInput, code, report, reprt, statement, fsDiv string) (Financials, error) {
	want := map[string]bool{}
	statements := in.Statements
	if len(statements) == 0 {
		statements = statementOrder[:3]
	}
	for _, s := range statements {
		if !slices.Contains(statementOrder, s) {
			return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "unknown statement type %q; use %s", s, strings.Join(statementOrder, ", "))
		}
		want[s] = true
	}

	var r fullStatementsResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}, "fs_div": {fsDiv}}
	if err := c.getJSON(ctx, "fnlttSinglAcntAll.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			alt := map[string]string{"consolidated": "separate", "separate": "consolidated"}[statement]
			he.Hint = "a company without subsidiaries has no consolidated statements, so try statement=" + alt +
				"; otherwise " + notFiledHint + ". Half and quarterly reports start in 2016"
		}
		return Financials{}, err
	}

	month := 12
	var co companyResponse
	if err := c.getJSON(ctx, "company.json", url.Values{"corp_code": {code}}, &co); err == nil {
		if m, err := strconv.Atoi(co.AccMt); err == nil && m >= 1 && m <= 12 {
			month = m
		}
	}
	p := fiscalPeriods(in.Year, month, report)

	out := Financials{CorpCode: code, Year: in.Year, Report: report, Statement: statement, Scope: "full", Accounts: []Account{}}
	var notes []string
	if month != 12 {
		notes = append(notes, fmt.Sprintf(fiscalYearNote, month))
	}
	filter := strings.ReplaceAll(strings.TrimSpace(in.Account), " ", "")
	seen := map[[3]string]bool{}
	hasRevenue := false
	for _, a := range r.List {
		out.Currency, out.ReceiptNo = a.Currency, a.RceptNo
		typ := fullStatementTypes[a.SjDiv]
		if !want[typ] {
			continue
		}
		name := strings.Join(strings.Fields(a.AccountNm), " ")
		id := a.AccountID
		if !strings.HasPrefix(id, "ifrs") && !strings.HasPrefix(id, "dart") {
			id = ""
		}
		if typ == "income_statement" && (id == "ifrs-full_Revenue" || strings.Contains(name, "매출")) {
			hasRevenue = true
		}
		if filter != "" && !strings.Contains(strings.ReplaceAll(name, " ", ""), filter) && !strings.Contains(strings.ToLower(id), strings.ToLower(filter)) {
			continue
		}
		// 당기순이익 is repeated in the comprehensive income statement.
		k := [3]string{typ, cmp.Or(id, name) + "|" + a.AccountDetail, a.ThstrmAmount + "|" + a.ThstrmAddAmount}
		if seen[k] {
			continue
		}
		seen[k] = true

		acct := Account{Name: name, AccountID: id, StatementType: typ}
		if typ == "equity" {
			acct.Component = equityComponent(a.AccountDetail)
		}
		if typ == "cash_flow" && id != "" && isOutflow(id) {
			acct.Flow = "outflow"
		}
		switch {
		case typ == "balance_sheet":
			acct.Current = fullAmount(Amount{End: p.end}, a.ThstrmAmount)
			acct.Prior = present(fullAmount(Amount{End: p.priorYearEnd}, a.FrmtrmAmount))
			acct.BeforePrior = present(fullAmount(Amount{End: p.twoYearsEnd}, a.BfefrmtrmAmount))
		case report == "annual":
			acct.Current = fullAmount(Amount{Start: p.start, End: p.end}, a.ThstrmAmount)
			acct.Prior = present(fullAmount(shiftYears(p.start, p.end, -1), a.FrmtrmAmount))
			acct.BeforePrior = present(fullAmount(shiftYears(p.start, p.end, -2), a.BfefrmtrmAmount))
		case typ == "income_statement":
			ytd, priorYTD := a.ThstrmAddAmount, a.FrmtrmAddAmount
			if report == "q1" {
				ytd, priorYTD = cmp.Or(ytd, a.ThstrmAmount), cmp.Or(priorYTD, a.FrmtrmQAmount)
			} else {
				acct.CurrentQuarter = present(fullAmount(Amount{Start: p.quarterStart, End: p.end}, a.ThstrmAmount))
				acct.PriorQuarter = present(fullAmount(shiftYears(p.quarterStart, p.end, -1), a.FrmtrmQAmount))
			}
			acct.Current = fullAmount(Amount{Start: p.start, End: p.end}, ytd)
			acct.Prior = present(fullAmount(shiftYears(p.start, p.end, -1), priorYTD))
			if strings.TrimSpace(ytd) == "" && acct.CurrentQuarter != nil {
				acct.Current.Note = "DART gave no year-to-date amount; current_quarter has the latest three months"
			}
		default: // cash flows and equity changes are year-to-date
			acct.Current = fullAmount(Amount{Start: p.start, End: p.end}, a.ThstrmAmount)
			acct.Prior = present(fullAmount(shiftYears(p.start, p.end, -1), cmp.Or(a.FrmtrmAmount, a.FrmtrmQAmount)))
		}
		out.Accounts = append(out.Accounts, acct)
	}
	if want["income_statement"] && !hasRevenue && len(r.List) > 0 {
		notes = append(notes, noRevenueNote)
	}
	if slices.ContainsFunc(out.Accounts, func(a Account) bool { return a.StatementType == "cash_flow" }) {
		notes = append(notes, cashFlowNote)
	}
	out.Note = strings.Join(notes, " ")

	rank := func(a Account) (int, int) {
		s := slices.Index(statementOrder, a.StatementType)
		if i := slices.Index(keyAccounts[a.StatementType], a.AccountID); i >= 0 && a.AccountID != "" {
			return s, i
		}
		return s, len(keyAccounts[a.StatementType])
	}
	slices.SortStableFunc(out.Accounts, func(a, b Account) int {
		sa, ia := rank(a)
		sb, ib := rank(b)
		return cmp.Or(cmp.Compare(sa, sb), cmp.Compare(ia, ib))
	})
	out.URL = viewer(out.ReceiptNo)
	return out, nil
}

type periods struct {
	start, quarterStart, end, priorYearEnd, twoYearsEnd string
}

// fiscalPeriods dates a report from the fiscal year-end month. An annual
// report's year is the fiscal year ending in it; an interim report's year is
// the fiscal year starting in it (they differ only when the year does not end
// in December).
func fiscalPeriods(year, endMonth int, report string) periods {
	var start time.Time
	if report == "annual" {
		start = time.Date(year-1, time.Month(endMonth)+1, 1, 0, 0, 0, 0, time.UTC)
	} else {
		start = time.Date(year, time.Month(endMonth%12)+1, 1, 0, 0, 0, 0, time.UTC)
	}
	months := map[string]int{"annual": 12, "half": 6, "q1": 3, "q3": 9}[report]
	end := start.AddDate(0, months, -1)
	day := func(t time.Time) string { return t.Format(time.DateOnly) }
	priorYearEnd := start.AddDate(0, 0, -1)
	if report == "annual" {
		priorYearEnd = end.AddDate(-1, 0, 0)
	}
	return periods{
		start:        day(start),
		quarterStart: day(start.AddDate(0, months-3, 0)),
		end:          day(end),
		priorYearEnd: day(lastOfMonth(priorYearEnd)),
		twoYearsEnd:  day(lastOfMonth(priorYearEnd.AddDate(-1, 0, 0))),
	}
}

func lastOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC)
}

// shiftYears moves a period by whole years, keeping month ends.
func shiftYears(start, end string, years int) Amount {
	s, err1 := time.Parse(time.DateOnly, start)
	e, err2 := time.Parse(time.DateOnly, end)
	if err1 != nil || err2 != nil {
		return Amount{}
	}
	e = lastOfMonth(time.Date(e.Year()+years, e.Month(), 1, 0, 0, 0, 0, time.UTC))
	return Amount{Start: s.AddDate(years, 0, 0).Format(time.DateOnly), End: e.Format(time.DateOnly)}
}

// fullAmount sets the amount of a dated period; per-share amounts of USD
// filers have decimals.
func fullAmount(a Amount, v string) Amount {
	if a.Amount = normalize.Int(v); a.Amount == nil && strings.Contains(v, ".") {
		a.Decimal = decimal(v)
	}
	return a
}

// present drops a comparison period without an amount.
func present(a Amount) *Amount {
	if a.Amount == nil && a.Decimal == nil {
		return nil
	}
	return &a
}

func isOutflow(id string) bool {
	_, name, _ := strings.Cut(id, "_")
	for _, p := range outflowPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// equityComponent shortens "자본 [구성요소]|지배기업의 소유주에게 귀속되는 지분
// [구성요소]|이익잉여금 [구성요소]" to its last part, 이익잉여금. The total
// column is "연결재무제표 [member]" (or 별도재무제표), here 합계.
func equityComponent(detail string) string {
	parts := strings.Split(detail, "|")
	last := strings.TrimSpace(parts[len(parts)-1])
	switch {
	case strings.HasSuffix(last, "[member]"):
		return "합계"
	case last == "-":
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(last, "[구성요소]"))
}
