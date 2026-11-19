package entity

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
	"github.com/YoungsoonLee/korea-data-mcp/providers/datagokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
)

var kst = time.FixedZone("KST", 9*60*60)

type profileInput struct {
	Company string `json:"company" jsonschema:"entity_id from entity_resolve_company (crno:…, dart:… or bizno:…), or anything it accepts: a name, corp_code, crno, business number, stock code or ISIN"`
}

type Profile struct {
	Company
	AsOf         string      `json:"as_of" jsonschema:"date of this lookup, YYYY-MM-DD (KST)"`
	OtherMatches []Company   `json:"other_matches,omitempty" jsonschema:"other companies that match the name; profile one by its entity_id"`
	Overview     *Overview   `json:"overview,omitempty"`
	Financials   *KeyFigures `json:"financials,omitempty" jsonschema:"main accounts from the latest annual report found"`
	AuditReport  *Filing     `json:"audit_report,omitempty" jsonschema:"the latest audit report (감사보고서) when the company files no annual report; its figures are in the document only"`
	Stock        *Quote      `json:"stock,omitempty" jsonschema:"latest trading day, listed companies only"`
	MajorHolders *Holders    `json:"major_holders,omitempty" jsonschema:"listed companies only"`
	MajorReports *Filings    `json:"major_reports,omitempty" jsonschema:"주요사항보고서 of the last 12 months: capital raises, buybacks, mergers, splits and other board decisions"`
	Recent       *Filings    `json:"recent_filings,omitempty" jsonschema:"every filing of the last 30 days"`
	Pension      *Pension    `json:"pension,omitempty" jsonschema:"National Pension Service subscribers, a proxy for employees"`
	Notes        []string    `json:"notes,omitempty"`
	Incomplete   []string    `json:"incomplete,omitempty" jsonschema:"sources that could not be checked and why, such as a missing API key"`
}

type Overview struct {
	Sources            []string `json:"sources"`
	Address            string   `json:"address,omitempty"`
	Homepage           string   `json:"homepage,omitempty"`
	Phone              string   `json:"phone,omitempty"`
	Industry           string   `json:"industry,omitempty" jsonschema:"표준산업분류 name"`
	IndustryCode       string   `json:"industry_code,omitempty" jsonschema:"KSIC code"`
	MainBusiness       string   `json:"main_business,omitempty"`
	CorporateForm      string   `json:"corporate_form,omitempty"`
	IsSME              *bool    `json:"is_sme,omitempty"`
	EstablishedDate    string   `json:"established_date,omitempty"`
	ListedDate         string   `json:"listed_date,omitempty"`
	DelistedDate       string   `json:"delisted_date,omitempty"`
	FiscalYearEndMonth int      `json:"fiscal_year_end_month,omitempty"`
	Employees          *int64   `json:"employees,omitempty" jsonschema:"from the FSC registry, as of its latest annual data"`
	AvgSalary          *int64   `json:"avg_salary,omitempty" jsonschema:"average annual salary per employee, KRW"`
	Auditor            string   `json:"auditor,omitempty"`
	AuditOpinion       string   `json:"audit_opinion,omitempty"`
	RegistryAsOf       string   `json:"registry_as_of,omitempty" jsonschema:"date the FSC record was last confirmed"`
}

type KeyFigures struct {
	Source    string       `json:"source"`
	Year      int          `json:"year" jsonschema:"fiscal year (사업연도)"`
	Statement string       `json:"statement" jsonschema:"consolidated, or separate when the company files no consolidated statements"`
	Start     string       `json:"start,omitempty" jsonschema:"first day of the fiscal year"`
	End       string       `json:"end"`
	Currency  string       `json:"currency"`
	ReceiptNo string       `json:"receipt_no"`
	URL       string       `json:"url"`
	Accounts  []KeyAccount `json:"accounts" jsonschema:"revenue, operating income, net income, total assets, liabilities and equity, as DART names them; financial companies report no 매출액"`
}

type KeyAccount struct {
	Name   string `json:"name"`
	Amount *int64 `json:"amount"`
	Prior  *int64 `json:"prior,omitempty" jsonschema:"the previous fiscal year"`
}

type Quote struct {
	Source       string   `json:"source"`
	Date         string   `json:"date"`
	Close        *int64   `json:"close"`
	ChangeRate   *float64 `json:"change_rate" jsonschema:"percent versus the previous close"`
	MarketCap    *int64   `json:"market_cap,omitempty" jsonschema:"KRW, common shares only"`
	ListedShares *int64   `json:"listed_shares,omitempty"`
}

type Holders struct {
	Source         string                 `json:"source"`
	Holders        []opendart.BlockHolder `json:"holders" jsonschema:"holders at 5% or more by their latest report, largest first; stakes include related parties"`
	OlderReporters int                    `json:"older_reporters,omitempty" jsonschema:"reporters whose last report is older than OpenDART returns (about two years); see opendart_get_ownership_reports"`
}

type Filings struct {
	Source string   `json:"source"`
	Since  string   `json:"since"`
	Total  int      `json:"total" jsonschema:"filings in the period; items holds the newest 10"`
	Items  []Filing `json:"items"`
}

type Filing struct {
	Date      string `json:"date"`
	Name      string `json:"name"`
	ReceiptNo string `json:"receipt_no"`
	URL       string `json:"url"`
}

type Pension struct {
	Source        string `json:"source"`
	Month         string `json:"month" jsonschema:"YYYY-MM, about two months behind"`
	Subscribers   int64  `json:"subscribers" jsonschema:"members at workplaces with the company's name; short-term construction sites are left out"`
	MonthlyBilled int64  `json:"monthly_billed,omitempty" jsonschema:"KRW, employer and employee shares"`
	Workplaces    int    `json:"workplaces"`
}

const maxFilings = 10

func (rs *resolver) profile(ctx context.Context, in profileInput) (Profile, error) {
	x := &run{ctx: ctx, call: rs.call}
	// An entity_id is the number after its prefix.
	q := strings.TrimSpace(in.Company)
	for _, p := range []string{"crno:", "dart:", "bizno:"} {
		if s, ok := strings.CutPrefix(q, p); ok {
			q = s
		}
	}
	res, cands, err := x.find(q)
	if err != nil {
		return Profile{}, err
	}
	c := cands[0]
	now := time.Now
	if rs.now != nil {
		now = rs.now
	}
	today := now().In(kst)
	out := Profile{Company: c.Company, AsOf: today.Format(time.DateOnly)}
	if len(res.Companies) > 1 {
		out.OtherMatches = res.Companies[1:]
	}
	if res.Note != "" {
		out.Notes = append(out.Notes, res.Note)
	}
	if n := countShared(cands); n > 1 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d companies have this name; this is the best match (an active business, listed first). "+
			"Check other_matches, and pass an entity_id to profile another.", n))
	}
	out.Overview = overview(c)

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	note := func(s string) {
		mu.Lock()
		out.Notes = append(out.Notes, s)
		mu.Unlock()
	}
	closed := c.BusinessStatus == "closed"
	if c.CorpCode != "" && !closed {
		wg.Go(func() {
			var n string
			if out.Financials, n = x.keyFigures(c, today); n == "" {
				return
			}
			note(n)
			if a := x.filings(c.CorpCode, "audit", today.AddDate(-2, 0, 0)); a != nil && len(a.Items) > 0 {
				out.AuditReport = &a.Items[0]
			}
		})
	}
	if c.CorpCode != "" {
		wg.Go(func() {
			out.MajorReports = x.filings(c.CorpCode, "major_report", today.AddDate(-1, 0, 0))
		})
		wg.Go(func() { out.Recent = x.filings(c.CorpCode, "", today.AddDate(0, 0, -30)) })
		if c.Listed {
			wg.Go(func() { out.MajorHolders = x.holders(c.CorpCode) })
		}
	}
	if c.Listed && c.StockCode != "" {
		wg.Go(func() { out.Stock = x.quote(c.StockCode) })
	}
	if c.BusinessNumber != "" && c.Name != "" {
		wg.Go(func() { out.Pension = x.pension(c) })
	}
	wg.Wait()
	if c.CorpCode == "" {
		out.Notes = append(out.Notes, "The company does not file with DART, so there are no financials, filings or 5% holders.")
	}
	if closed {
		// The FSC registry keeps a dissolved company's last figures: the
		// 삼성물산 merged away in 2015 still has 8,663 employees.
		since := ""
		if c.ClosedDate != "" {
			since = " since " + c.ClosedDate
		}
		out.Notes = append(out.Notes, "The business is closed"+since+
			" (National Tax Service); the overview, including employees, can describe it before then.")
	}
	out.Incomplete = x.incomplete()
	return out, nil
}

func overview(c *cand) *Overview {
	o := &Overview{}
	if d := c.dart; d != nil {
		o.Sources = append(o.Sources, "opendart_get_company")
		o.Address, o.Homepage, o.Phone = d.Address, d.Homepage, d.Phone
		o.IndustryCode, o.EstablishedDate, o.FiscalYearEndMonth = d.IndustryCode, d.EstablishedDate, d.FiscalYearEndMonth
	}
	if f := c.fsc; f != nil {
		o.Sources = append(o.Sources, "datagokr_get_corporation")
		o.Address = cmp.Or(o.Address, f.Address)
		o.Homepage = cmp.Or(o.Homepage, f.Homepage)
		o.Phone = cmp.Or(o.Phone, f.Phone)
		o.EstablishedDate = cmp.Or(o.EstablishedDate, f.EstablishedDate)
		if o.FiscalYearEndMonth == 0 && f.FiscalYearEndMonth != nil {
			o.FiscalYearEndMonth = *f.FiscalYearEndMonth
		}
		o.Industry, o.MainBusiness, o.CorporateForm, o.IsSME = f.Industry, f.MainBusiness, f.CorporateForm, f.IsSME
		o.ListedDate, o.DelistedDate = f.ListedDate, f.DelistedDate
		o.Employees, o.AvgSalary = f.Employees, f.AvgSalary
		o.Auditor, o.AuditOpinion, o.RegistryAsOf = f.Auditor, f.AuditOpinion, f.AsOf
	}
	if o.Sources == nil {
		return nil
	}
	return o
}

// keyAccounts are the main lines of fnlttSinglAcnt, matched by prefix
// because DART appends (손실) to some.
var keyAccounts = []string{"매출액", "영업이익", "당기순이익", "자산총계", "부채총계", "자본총계"}

// lastFiscalYear is the last fiscal year ended before today, named for the
// year it ends in. Its annual report can still be due (90 days after).
func lastFiscalYear(today time.Time, endMonth int) int {
	if endMonth < 1 || endMonth > 12 {
		endMonth = 12
	}
	for y := today.Year(); ; y-- {
		if today.After(time.Date(y, time.Month(endMonth)+1, 1, 0, 0, 0, 0, kst)) {
			return y
		}
	}
}

// keyFigures finds the latest annual report: a year earlier while it is not
// filed yet, and separate statements for a company without subsidiaries.
func (x *run) keyFigures(c *cand, today time.Time) (*KeyFigures, string) {
	month := 12
	if c.dart != nil && c.dart.FiscalYearEndMonth != 0 {
		month = c.dart.FiscalYearEndMonth
	} else if c.fsc != nil && c.fsc.FiscalYearEndMonth != nil {
		month = *c.fsc.FiscalYearEndMonth
	}
	y := lastFiscalYear(today, month)
	for _, year := range []int{y, y - 1} {
		for _, st := range []string{"consolidated", "separate"} {
			var f opendart.Financials
			err := x.do("opendart_get_financials", map[string]any{"corp_code": c.CorpCode, "year": year, "statement": st}, &f)
			if e := hub.AsError(err); e != nil {
				if e.Code != hub.CodeNotFound {
					return nil, ""
				}
				// A filed report without consolidated statements is
				// retried as separate; an unfiled one is not.
				if st == "consolidated" && strings.HasPrefix(e.Message, "no consolidated statements") {
					continue
				}
				break
			}
			k := &KeyFigures{Source: "opendart_get_financials", Year: f.Year, Statement: f.Statement, Currency: f.Currency, ReceiptNo: f.ReceiptNo, URL: f.URL, Accounts: []KeyAccount{}}
			for _, name := range keyAccounts {
				i := slices.IndexFunc(f.Accounts, func(a opendart.Account) bool { return strings.HasPrefix(a.Name, name) })
				if i < 0 {
					continue
				}
				a := f.Accounts[i]
				ka := KeyAccount{Name: a.Name, Amount: a.Current.Amount}
				if a.Prior != nil {
					ka.Prior = a.Prior.Amount
				}
				k.Accounts = append(k.Accounts, ka)
				if a.Current.Start != "" {
					k.Start = a.Current.Start
				}
				k.End = cmp.Or(k.End, a.Current.End)
			}
			return k, ""
		}
	}
	// Most unlisted companies file audit reports only, which the financial
	// API does not read (삼성디스플레이); a new listing has not filed one yet.
	return nil, fmt.Sprintf("No annual report (사업보고서) for fiscal year %d or %d, the only filings OpenDART's financial API reads: "+
		"a company filing audit reports only (most unlisted companies; see audit_report), or a new listing.", y, y-1)
}

func (x *run) filings(corpCode, typ string, since time.Time) *Filings {
	args := map[string]any{"corp_code": corpCode, "start_date": since.Format(time.DateOnly), "final_only": true, "page_size": maxFilings}
	if typ != "" {
		args["type"] = typ
	}
	var l hub.List[opendart.Disclosure]
	if err := x.do("opendart_search_disclosures", args, &l); err != nil && hub.AsError(err).Code != hub.CodeNotFound {
		return nil
	}
	out := &Filings{Source: "opendart_search_disclosures", Since: since.Format(time.DateOnly), Total: l.PageInfo.TotalCount, Items: []Filing{}}
	for _, d := range l.Items {
		out.Items = append(out.Items, Filing{Date: d.ReceivedDate, Name: d.ReportName, ReceiptNo: d.ReceiptNo, URL: d.URL})
	}
	return out
}

func (x *run) holders(corpCode string) *Holders {
	var r opendart.OwnershipReports
	if err := x.do("opendart_get_ownership_reports", map[string]any{"corp_code": corpCode, "page_size": 1}, &r); err != nil {
		if hub.AsError(err).Code == hub.CodeNotFound {
			return &Holders{Source: "opendart_get_ownership_reports", Holders: []opendart.BlockHolder{}}
		}
		return nil
	}
	out := &Holders{Source: "opendart_get_ownership_reports", Holders: []opendart.BlockHolder{}, OlderReporters: len(r.OlderReporters)}
	for _, h := range r.Holders {
		if h.Status == "holding" {
			out.Holders = append(out.Holders, h)
		}
	}
	return out
}

func (x *run) quote(code string) *Quote {
	var sp datagokr.StockPrices
	if x.do("datagokr_get_stock_prices", map[string]any{"stock": code, "page_size": 1}, &sp) != nil || len(sp.Items) == 0 {
		return nil
	}
	p := sp.Items[0]
	return &Quote{Source: "datagokr_get_stock_prices", Date: p.Date, Close: p.Close, ChangeRate: p.ChangeRate, MarketCap: p.MarketCap, ListedShares: p.ListedShares}
}

// pension adds up the company's own workplaces. Only the first 6 digits of
// the business number are published and other companies share them, so the
// name must match too; construction sites filed under the company's name
// (삼성전자(주)-(일용)…) are separate workplaces and left out.
func (x *run) pension(c *cand) *Pension {
	var l datagokr.PensionWorkplaces
	if x.do("datagokr_get_pension_workplaces", map[string]any{"business_number": c.BusinessNumber, "name": normalize.StripCompanyForm(c.Name), "page_size": 20}, &l) != nil {
		return nil
	}
	out := &Pension{Source: "datagokr_get_pension_workplaces", Month: l.AsOf}
	for _, w := range l.Items {
		if w.Kind != "workplace" || w.Status != "enrolled" || w.Month != l.AsOf || !sameName(w.Name, c.Name) {
			continue
		}
		out.Workplaces++
		if w.Subscribers != nil {
			out.Subscribers += *w.Subscribers
		}
		if w.MonthlyBilled != nil {
			out.MonthlyBilled += *w.MonthlyBilled
		}
	}
	if out.Workplaces == 0 {
		return nil
	}
	return out
}
