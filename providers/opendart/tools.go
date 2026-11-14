package opendart

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTools(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_search_companies",
		Description: "Find a Korean company's corp_code (고유번호), the 8-digit ID that every other opendart tool needs. " +
			"Matches Korean or English company names, stock codes (종목코드), or corp_codes. " +
			"Exact matches and companies with a stock code come first. listed and market are checked with each company's profile: " +
			"DART keeps the stock code of a delisted company. The first call downloads DART's full company list and can take a few seconds.",
	}, c.searchCompanies)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_company",
		Description: "Get a company profile (기업개황) from DART: legal name, stock code, market, CEO, registration numbers, " +
			"address, homepage, industry code, founding date and fiscal year-end month.",
	}, c.getCompany)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_search_disclosures",
		Description: "Search disclosures (공시) filed to DART by receipt date, newest first. Filter by company, disclosure type, or market. " +
			"Without corp_code the date range is limited to 3 months. Each item has a url to the DART viewer.",
	}, c.searchDisclosures)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_financials",
		Description: "Get financial statements for one company and period. scope key (default): main accounts (주요계정) such as assets, liabilities, equity, revenue, " +
			"operating income and net income. scope full: every line of the balance sheet, income statement and cash flow statement (전체 재무제표), " +
			"and on request the statement of changes in equity, in printed order for the main lines, filterable by account name. " +
			"Every amount comes with the start and end of the period it covers, with prior-period comparisons; " +
			"in half and third-quarter reports income statement items have both year-to-date and latest-quarter amounts. Amounts are in the currency field (KRW for most). " +
			"For a fiscal year not ending in December, year is the fiscal year ending in it for annual reports but the one starting in it for half and quarterly reports. " +
			"Annual data is available from fiscal year 2015, half and quarterly from 2016.",
	}, c.getFinancials)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_dividends",
		Description: "Get dividends (배당에 관한 사항) from a periodic report: cash dividend per share, yield and total, payout ratio, " +
			"net income and EPS, for common and preferred shares, for the report's period and the two prior fiscal years. Amounts are in KRW (원). " +
			"In half and quarterly reports the current period is year-to-date, but the prior periods are full fiscal years. " +
			"Per-share amounts are comparable across years only when par_value is the same (stock splits). Data is available from fiscal year 2015.",
	}, c.getDividends)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_major_shareholders",
		Description: "Get the largest shareholder (최대주주) and its related parties (특수관계인) from a periodic report, " +
			"with common and preferred shares held and stakes at the start and end of the period, and the group total. " +
			"This is the controlling group only; for other holders of 5% or more, such as the National Pension Service, use opendart_get_ownership_reports. Data is available from fiscal year 2015.",
	}, c.getMajorShareholders)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_investments",
		Description: "Get a company's stakes in other companies (타법인출자 현황) from its annual or half-year report: subsidiaries, affiliates and " +
			"investments, with shares, percent and book value at the start and end of the period, acquisitions and disposals, and the investee's total assets and net income. " +
			"Stakes sold entirely during the period are marked exited. Each investee gets its DART corp_code when its name identifies one company, " +
			"so it can be followed with other opendart tools; the match is by name, so check it for long-held stakes in companies that no longer exist. Amounts are in KRW (원) unless a note says otherwise. Data is available from fiscal year 2015.",
	}, c.getInvestments)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_list_corporate_actions",
		Description: "List one company's major decisions from 주요사항보고서: rights offerings and bonus issues, capital reductions, " +
			"convertible bonds, bonds with warrants and exchangeable bonds, buybacks (direct and through a trust) and treasury stock disposals, mergers and splits. " +
			"Each item has the terms as numbers: proceeds by purpose and an implied issue price, dilution, bond prices with the reset floor and shares at the floor, " +
			"buyback amounts with common and preferred shares added up. start_date and end_date match the date a decision was first filed; " +
			"receipt_no and filed_date are those of its latest correction, which can be months later. Latest filing first. " +
			"To find these decisions across all companies, use opendart_search_disclosures with type major_report.",
	}, c.listCorporateActions)

	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_get_ownership_reports",
		Description: "Get ownership reports filed for a company. kind major_holders (default): holders of 5% or more (대량보유 상황보고) " +
			"with each holder's latest stake and status, such as the National Pension Service, BlackRock or the controlling group, and the history of reports. " +
			"kind insiders: executives' and major shareholders' own holdings and changes (임원·주요주주 소유보고), filterable by reporter and direction. " +
			"OpenDART returns only about two years of reports; older 5% reporters are listed in older_reporters without their stake.",
	}, c.getOwnershipReports)
}

// ---- opendart_search_companies ----

type searchCompaniesInput struct {
	Query      string `json:"query" jsonschema:"company name in Korean or English, 6-character stock code, or 8-digit corp_code. Examples: 삼성전자, Samsung Electronics, 005930"`
	ListedOnly bool   `json:"listed_only,omitempty" jsonschema:"only return companies listed on KRX (KOSPI, KOSDAQ, KONEX)"`
	hub.PageParams
}

type Company struct {
	CorpCode     string `json:"corp_code"`
	CorpName     string `json:"corp_name"`
	CorpEngName  string `json:"corp_eng_name,omitempty"`
	StockCode    string `json:"stock_code,omitempty" jsonschema:"kept after delisting; listed tells whether it trades now"`
	Listed       bool   `json:"listed"`
	Market       string `json:"market,omitempty" jsonschema:"KOSPI, KOSDAQ or KONEX when listed, OTHER for a delisted company with a stock code"`
	ModifiedDate string `json:"modified_date,omitempty"`
}

// maxListedChecks bounds the profile calls one listed_only search makes.
// "바이오" matches 116 companies with a stock code.
const maxListedChecks = 200

func (c *client) searchCompanies(ctx context.Context, in searchCompaniesInput) (hub.List[Company], error) {
	page, size, err := in.Resolve(10, 50)
	if err != nil {
		return hub.List[Company]{}, err
	}
	if normName(in.Query) == "" {
		return hub.List[Company]{}, hub.Errorf(hub.CodeInvalidArgument, "query is required")
	}
	matches, err := c.corps.search(ctx, in.Query, in.ListedOnly)
	if err != nil {
		return hub.List[Company]{}, err
	}
	var classes map[string]string
	if in.ListedOnly {
		if len(matches) > maxListedChecks {
			return hub.List[Company]{}, hub.Errorf(hub.CodeInvalidArgument, "%d companies with a stock code match %q; listed_only checks at most %d", len(matches), in.Query, maxListedChecks).
				WithHint("use a longer name")
		}
		if classes, err = c.corpClasses(ctx, matches); err != nil {
			return hub.List[Company]{}, err
		}
		matches = slices.DeleteFunc(matches, func(m corp) bool { return !listed(classes[m.Code]) })
	}
	items := pageOf(matches, page, size)
	if classes == nil {
		if classes, err = c.corpClasses(ctx, items); err != nil {
			return hub.List[Company]{}, err
		}
	}
	out := hub.List[Company]{Items: []Company{}, PageInfo: hub.NewPageInfo(page, size, len(matches))}
	for _, m := range items {
		cls := classes[m.Code]
		out.Items = append(out.Items, Company{
			CorpCode:     m.Code,
			CorpName:     m.Name,
			CorpEngName:  m.EngName,
			StockCode:    m.StockCode,
			Listed:       listed(cls),
			Market:       market(cls),
			ModifiedDate: m.ModifyDate,
		})
	}
	return out, nil
}

func pageOf[T any](s []T, page, size int) []T {
	start := (page - 1) * size
	if start >= len(s) {
		return nil
	}
	return s[start:min(start+size, len(s))]
}

// ---- opendart_get_company ----

type corpCodeInput struct {
	CorpCode string `json:"corp_code" jsonschema:"8-digit corp_code from opendart_search_companies, or a 6-character stock code (종목코드)"`
}

type companyResponse struct {
	apiStatus
	CorpName    string `json:"corp_name"`
	CorpNameEng string `json:"corp_name_eng"`
	StockName   string `json:"stock_name"`
	StockCode   string `json:"stock_code"`
	CeoNm       string `json:"ceo_nm"`
	CorpCls     string `json:"corp_cls"`
	JurirNo     string `json:"jurir_no"`
	BizrNo      string `json:"bizr_no"`
	Adres       string `json:"adres"`
	HmURL       string `json:"hm_url"`
	IrURL       string `json:"ir_url"`
	PhnNo       string `json:"phn_no"`
	FaxNo       string `json:"fax_no"`
	IndutyCode  string `json:"induty_code"`
	EstDt       string `json:"est_dt"`
	AccMt       string `json:"acc_mt"`
}

type CompanyProfile struct {
	CorpCode              string `json:"corp_code"`
	CorpName              string `json:"corp_name"`
	CorpNameEng           string `json:"corp_name_eng,omitempty"`
	StockName             string `json:"stock_name,omitempty" jsonschema:"ticker name for listed companies, short name otherwise"`
	StockCode             string `json:"stock_code,omitempty" jsonschema:"kept after delisting; market tells whether it trades now"`
	Market                string `json:"market" jsonschema:"KOSPI, KOSDAQ, KONEX, or OTHER (unlisted or delisted)"`
	CEO                   string `json:"ceo,omitempty"`
	CorporateRegistration string `json:"corporate_registration_no,omitempty" jsonschema:"법인등록번호"`
	BusinessRegistration  string `json:"business_registration_no,omitempty" jsonschema:"사업자등록번호"`
	Address               string `json:"address,omitempty"`
	Homepage              string `json:"homepage,omitempty"`
	IRPage                string `json:"ir_page,omitempty"`
	Phone                 string `json:"phone,omitempty"`
	Fax                   string `json:"fax,omitempty"`
	IndustryCode          string `json:"industry_code,omitempty" jsonschema:"Korean Standard Industrial Classification (KSIC) code"`
	EstablishedDate       string `json:"established_date,omitempty" jsonschema:"YYYY-MM-DD"`
	FiscalYearEndMonth    int    `json:"fiscal_year_end_month,omitempty" jsonschema:"결산월, 1-12"`
}

func (c *client) getCompany(ctx context.Context, in corpCodeInput) (CompanyProfile, error) {
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return CompanyProfile{}, err
	}
	var r companyResponse
	if err := c.getJSON(ctx, "company.json", url.Values{"corp_code": {code}}, &r); err != nil {
		return CompanyProfile{}, err
	}
	c.rememberClass(code, r.CorpCls)
	month, _ := strconv.Atoi(r.AccMt)
	return CompanyProfile{
		CorpCode:              code,
		CorpName:              r.CorpName,
		CorpNameEng:           r.CorpNameEng,
		StockName:             r.StockName,
		StockCode:             strings.TrimSpace(r.StockCode),
		Market:                market(r.CorpCls),
		CEO:                   r.CeoNm,
		CorporateRegistration: r.JurirNo,
		BusinessRegistration:  r.BizrNo,
		Address:               r.Adres,
		Homepage:              r.HmURL,
		IRPage:                r.IrURL,
		Phone:                 r.PhnNo,
		Fax:                   r.FaxNo,
		IndustryCode:          r.IndutyCode,
		EstablishedDate:       normalize.Date(r.EstDt),
		FiscalYearEndMonth:    month,
	}, nil
}

// ---- opendart_search_disclosures ----

var disclosureTypes = map[string]string{
	"periodic":       "A", // 정기공시
	"major_report":   "B", // 주요사항보고
	"issuance":       "C", // 발행공시
	"shareholding":   "D", // 지분공시
	"other":          "E", // 기타공시
	"audit":          "F", // 외부감사관련
	"fund":           "G", // 펀드공시
	"securitization": "H", // 자산유동화
	"exchange":       "I", // 거래소공시
	"fair_trade":     "J", // 공정위공시
}

var marketCodes = map[string]string{"KOSPI": "Y", "KOSDAQ": "K", "KONEX": "N", "OTHER": "E"}

type searchDisclosuresInput struct {
	CorpCode  string `json:"corp_code,omitempty" jsonschema:"8-digit corp_code or 6-character stock code; omit to search all companies"`
	StartDate string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD, receipt date from (default: 30 days before end_date)"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD, receipt date to (default: today in KST)"`
	Type      string `json:"type,omitempty" jsonschema:"one of: periodic (정기공시: annual/half/quarterly reports), major_report (주요사항보고), issuance (발행공시), shareholding (지분공시), other (기타공시), audit (외부감사관련), fund (펀드공시), securitization (자산유동화), exchange (거래소공시), fair_trade (공정위공시)"`
	Market    string `json:"market,omitempty" jsonschema:"one of: KOSPI, KOSDAQ, KONEX, OTHER"`
	FinalOnly bool   `json:"final_only,omitempty" jsonschema:"only the final version of each report, excluding superseded originals of corrected filings"`
	hub.PageParams
}

type disclosureResponse struct {
	apiStatus
	TotalCount int `json:"total_count"`
	List       []struct {
		CorpCls   string `json:"corp_cls"`
		CorpName  string `json:"corp_name"`
		CorpCode  string `json:"corp_code"`
		StockCode string `json:"stock_code"`
		ReportNm  string `json:"report_nm"`
		RceptNo   string `json:"rcept_no"`
		FlrNm     string `json:"flr_nm"`
		RceptDt   string `json:"rcept_dt"`
		Rm        string `json:"rm"`
	} `json:"list"`
}

type Disclosure struct {
	ReceiptNo    string `json:"receipt_no"`
	ReceivedDate string `json:"received_date"`
	CorpCode     string `json:"corp_code"`
	CorpName     string `json:"corp_name"`
	StockCode    string `json:"stock_code,omitempty"`
	Market       string `json:"market,omitempty"`
	ReportName   string `json:"report_name" jsonschema:"may carry a correction prefix such as [기재정정]"`
	FilerName    string `json:"filer_name"`
	Remarks      string `json:"remarks,omitempty" jsonschema:"DART remark flags: 유 KOSPI, 코 KOSDAQ, 넥 KONEX, 채 bond-listed, 공 Fair Trade Commission, 연 includes consolidated, 정 later corrected, 철 withdrawn"`
	URL          string `json:"url"`
}

func (c *client) searchDisclosures(ctx context.Context, in searchDisclosuresInput) (hub.List[Disclosure], error) {
	var zero hub.List[Disclosure]
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return zero, err
	}

	end := c.now().In(kst)
	if in.EndDate != "" {
		if end, err = parseDate("end_date", in.EndDate); err != nil {
			return zero, err
		}
	}
	start := end.AddDate(0, 0, -30)
	if in.StartDate != "" {
		if start, err = parseDate("start_date", in.StartDate); err != nil {
			return zero, err
		}
	}
	if start.After(end) {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "start_date %s is after end_date %s", start.Format(time.DateOnly), end.Format(time.DateOnly))
	}

	q := url.Values{
		"bgn_de":     {start.Format("20060102")},
		"end_de":     {end.Format("20060102")},
		"page_no":    {strconv.Itoa(page)},
		"page_count": {strconv.Itoa(size)},
	}
	if in.CorpCode != "" {
		code, err := c.resolveCorpCode(ctx, in.CorpCode)
		if err != nil {
			return zero, err
		}
		q.Set("corp_code", code)
	} else if end.Sub(start) > 92*24*time.Hour {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "without corp_code the date range is limited to 3 months").
			WithHint("narrow start_date/end_date or pass corp_code")
	}
	if in.Type != "" {
		ty, ok := disclosureTypes[in.Type]
		if !ok {
			return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown type %q; use one of %s", in.Type, keys(disclosureTypes))
		}
		q.Set("pblntf_ty", ty)
	}
	if in.Market != "" {
		cls, ok := marketCodes[strings.ToUpper(in.Market)]
		if !ok {
			return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown market %q; use one of %s", in.Market, keys(marketCodes))
		}
		q.Set("corp_cls", cls)
	}
	if in.FinalOnly {
		q.Set("last_reprt_at", "Y")
	}

	var r disclosureResponse
	if err := c.getJSON(ctx, "list.json", q, &r); err != nil {
		if hub.AsError(err).Code == hub.CodeNotFound {
			return hub.List[Disclosure]{Items: []Disclosure{}, PageInfo: hub.NewPageInfo(page, size, 0)}, nil
		}
		return zero, err
	}
	out := hub.List[Disclosure]{Items: make([]Disclosure, 0, len(r.List)), PageInfo: hub.NewPageInfo(page, size, r.TotalCount)}
	for _, d := range r.List {
		out.Items = append(out.Items, Disclosure{
			ReceiptNo:    d.RceptNo,
			ReceivedDate: normalize.Date(d.RceptDt),
			CorpCode:     d.CorpCode,
			CorpName:     d.CorpName,
			StockCode:    strings.TrimSpace(d.StockCode),
			Market:       market(d.CorpCls),
			ReportName:   strings.TrimSpace(d.ReportNm),
			FilerName:    d.FlrNm,
			Remarks:      d.Rm,
			URL:          viewer(d.RceptNo),
		})
	}
	return out, nil
}

func parseDate(field, s string) (time.Time, error) {
	t, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(s), kst)
	if err != nil {
		return time.Time{}, hub.Errorf(hub.CodeInvalidArgument, "%s must be YYYY-MM-DD, got %q", field, s)
	}
	return t, nil
}

func keys(m map[string]string) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return strings.Join(ks, ", ")
}

// ---- opendart_get_financials ----

var reportCodes = map[string]string{
	"annual": "11011", // 사업보고서
	"half":   "11012", // 반기보고서
	"q1":     "11013", // 1분기보고서
	"q3":     "11014", // 3분기보고서
}

// reportParams validates a fiscal year and report name and returns the
// report name with its default and DART's reprt_code.
func (c *client) reportParams(year int, report string) (string, string, error) {
	if thisYear := c.now().In(kst).Year(); year < 2015 || year > thisYear {
		return "", "", hub.Errorf(hub.CodeInvalidArgument, "year must be between 2015 and %d, got %d", thisYear, year)
	}
	report = cmp.Or(report, "annual")
	reprt, ok := reportCodes[report]
	if !ok {
		return "", "", hub.Errorf(hub.CodeInvalidArgument, "unknown report %q; use one of %s", report, keys(reportCodes))
	}
	return report, reprt, nil
}

// notFiledHint explains an empty result for a periodic report.
const notFiledHint = "the report may not be filed yet or the company does not file it; try an earlier year or report=annual"

type getFinancialsInput struct {
	CorpCode   string   `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Year       int      `json:"year" jsonschema:"fiscal year (사업연도), 2015 or later"`
	Report     string   `json:"report,omitempty" jsonschema:"one of: annual (default, 사업보고서), half (반기), q1 (1분기), q3 (3분기)"`
	Statement  string   `json:"statement,omitempty" jsonschema:"one of: consolidated (default, 연결재무제표), separate (별도/개별재무제표)"`
	Scope      string   `json:"scope,omitempty" jsonschema:"key (default): about 15 main accounts; full: every line of the statements (전체 재무제표)"`
	Statements []string `json:"statements,omitempty" jsonschema:"full only: any of balance_sheet, income_statement, cash_flow, equity (statement of changes in equity); default the first three"`
	Account    string   `json:"account,omitempty" jsonschema:"full only: lines whose Korean name or account ID contains this, e.g. 차입금 or Revenue"`
}

type financialsResponse struct {
	apiStatus
	List []struct {
		RceptNo         string `json:"rcept_no"`
		AccountNm       string `json:"account_nm"`
		FsDiv           string `json:"fs_div"`
		SjDiv           string `json:"sj_div"`
		ThstrmDt        string `json:"thstrm_dt"`
		ThstrmAmount    string `json:"thstrm_amount"`
		ThstrmAddAmount string `json:"thstrm_add_amount"`
		FrmtrmDt        string `json:"frmtrm_dt"`
		FrmtrmAmount    string `json:"frmtrm_amount"`
		FrmtrmAddAmount string `json:"frmtrm_add_amount"`
		BfefrmtrmDt     string `json:"bfefrmtrm_dt"`
		BfefrmtrmAmount string `json:"bfefrmtrm_amount"`
		Currency        string `json:"currency"`
	} `json:"list"`
}

type Financials struct {
	CorpCode  string    `json:"corp_code"`
	Year      int       `json:"year"`
	Report    string    `json:"report"`
	Statement string    `json:"statement"`
	Scope     string    `json:"scope"`
	Currency  string    `json:"currency"`
	ReceiptNo string    `json:"receipt_no" jsonschema:"the filing the values come from (접수번호); after a correction (기재정정) it is the latest corrected filing"`
	URL       string    `json:"url"`
	Note      string    `json:"note,omitempty"`
	Accounts  []Account `json:"accounts"`
}

type Account struct {
	Name           string  `json:"name" jsonschema:"account name in Korean, e.g. 자산총계, 매출액, 영업이익, 당기순이익"`
	AccountID      string  `json:"account_id,omitempty" jsonschema:"full scope: IFRS or DART account ID, e.g. ifrs-full_Revenue; omitted for company-specific lines"`
	StatementType  string  `json:"statement_type" jsonschema:"balance_sheet, income_statement, cash_flow or equity"`
	Component      string  `json:"component,omitempty" jsonschema:"equity: the equity component, e.g. 이익잉여금"`
	Flow           string  `json:"flow,omitempty" jsonschema:"cash_flow: outflow for money paid out, which DART gives as a positive amount"`
	Current        Amount  `json:"current" jsonschema:"the report's period; year-to-date for income statement items in interim reports"`
	CurrentQuarter *Amount `json:"current_quarter,omitempty" jsonschema:"the latest three months only, for income statement items in half and third-quarter reports"`
	Prior          *Amount `json:"prior,omitempty" jsonschema:"the comparison period: the same months of the prior year for income statement items, the prior fiscal year-end for balance sheet items"`
	PriorQuarter   *Amount `json:"prior_quarter,omitempty" jsonschema:"the same three months of the prior year"`
	BeforePrior    *Amount `json:"before_prior,omitempty" jsonschema:"two fiscal years back, annual reports only"`
}

// Amount is a value with the period it covers.
type Amount struct {
	Start   string   `json:"start,omitempty" jsonschema:"first day covered, YYYY-MM-DD; omitted for balance sheet items, which are positions on the end date"`
	End     string   `json:"end" jsonschema:"last day covered, or the date of a balance sheet position, YYYY-MM-DD"`
	Amount  *int64   `json:"amount"`
	Decimal *float64 `json:"decimal,omitempty" jsonschema:"set instead of amount when it has decimals, e.g. earnings per share in USD"`
	Note    string   `json:"note,omitempty" jsonschema:"set when the period of the amount could not be confirmed"`
}

var statementTypes = map[string]string{"BS": "balance_sheet", "IS": "income_statement"}

func (c *client) getFinancials(ctx context.Context, in getFinancialsInput) (Financials, error) {
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return Financials{}, err
	}
	report, reprt, err := c.reportParams(in.Year, in.Report)
	if err != nil {
		return Financials{}, err
	}
	statement := cmp.Or(in.Statement, "consolidated")
	fsDiv := map[string]string{"consolidated": "CFS", "separate": "OFS"}[statement]
	if fsDiv == "" {
		return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "unknown statement %q; use consolidated or separate", statement)
	}
	switch cmp.Or(in.Scope, "key") {
	case "full":
		return c.getFullFinancials(ctx, in, code, report, reprt, statement, fsDiv)
	case "key":
		if len(in.Statements) > 0 || in.Account != "" {
			return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "statements and account need scope=full")
		}
	default:
		return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "unknown scope %q; use key or full", in.Scope)
	}

	var r financialsResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}}
	if err := c.getJSON(ctx, "fnlttSinglAcnt.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Hint = notFiledHint
		}
		return Financials{}, err
	}

	out := Financials{CorpCode: code, Year: in.Year, Report: report, Statement: statement, Scope: "key", Accounts: []Account{}}
	other := 0
	seen := map[[4]string]bool{}
	for _, a := range r.List {
		if a.FsDiv != fsDiv {
			other++
			continue
		}
		// DART repeats 당기순이익 with identical amounts at the top of the
		// comprehensive income section; keep only the first.
		k := [4]string{a.AccountNm, a.SjDiv, a.ThstrmAmount, a.ThstrmAddAmount}
		if seen[k] {
			continue
		}
		seen[k] = true
		out.Currency, out.ReceiptNo = a.Currency, a.RceptNo
		acct := Account{
			Name:          a.AccountNm,
			StatementType: cmp.Or(statementTypes[a.SjDiv], a.SjDiv),
			Current:       amountFor(a.ThstrmDt, a.ThstrmAmount),
			Prior:         optional(amountFor(a.FrmtrmDt, a.FrmtrmAmount)),
			BeforePrior:   optional(amountFor(a.BfefrmtrmDt, a.BfefrmtrmAmount)),
		}
		// In interim reports DART labels income statement items with the
		// year-to-date range ("2025.01.01 ~ 2025.06.30") but puts the latest
		// quarter in *_amount and the year-to-date figure in *_add_amount.
		// Each amount is reported with the period it actually covers.
		if acct.Current.Start != "" && report != "annual" {
			quarter := quarterOf(acct.Current)
			switch {
			case strings.TrimSpace(a.ThstrmAddAmount) != "":
				if quarter.Start != acct.Current.Start { // a first-quarter report has one quarter only
					quarter.Amount = acct.Current.Amount
					acct.CurrentQuarter = &quarter
					if acct.Prior != nil {
						pq := quarterOf(*acct.Prior)
						pq.Amount = acct.Prior.Amount
						acct.PriorQuarter = &pq
					}
				}
				acct.Current.Amount = normalize.Int(a.ThstrmAddAmount)
				if acct.Prior != nil {
					acct.Prior.Amount = normalize.Int(a.FrmtrmAddAmount)
				}
			case quarter.Start != acct.Current.Start:
				// Not seen in any filing checked, but without a separate
				// year-to-date figure the one amount given could be either.
				acct.Current.Note = "DART gave no separate year-to-date amount; this may cover only " + quarter.Start + " ~ " + quarter.End
			}
		}
		out.Accounts = append(out.Accounts, acct)
	}
	if len(out.Accounts) == 0 {
		e := hub.Errorf(hub.CodeNotFound, "no %s statements for %s %d %s", statement, code, in.Year, report)
		if other > 0 {
			alt := map[string]string{"consolidated": "separate", "separate": "consolidated"}[statement]
			e = e.WithHint(fmt.Sprintf("this company reports %s statements; retry with statement=%s", alt, alt))
		}
		return Financials{}, e
	}
	out.URL = viewer(out.ReceiptNo)
	return out, nil
}

var dotDate = regexp.MustCompile(`\d{4}\.\d{2}\.\d{2}`)

// amountFor reads a DART period label, "2023.12.31 현재" (a balance sheet
// position) or "2023.01.01 ~ 2023.12.31", with its amount.
func amountFor(label, amount string) Amount {
	dates := dotDate.FindAllString(label, 2)
	for i := range dates {
		dates[i] = normalize.Date(dates[i])
	}
	a := Amount{Amount: normalize.Int(amount)}
	switch len(dates) {
	case 1:
		a.End = dates[0]
	case 2:
		a.Start, a.End = dates[0], dates[1]
	}
	return a
}

// optional drops a comparison column that DART left empty.
func optional(a Amount) *Amount {
	if a.End == "" && a.Amount == nil {
		return nil
	}
	return &a
}

// quarterOf returns the last three months of a period, without an amount.
func quarterOf(a Amount) Amount {
	end, err := time.Parse("2006-01-02", a.End)
	if err != nil {
		return Amount{}
	}
	start := time.Date(end.Year(), end.Month()-2, 1, 0, 0, 0, 0, time.UTC)
	return Amount{Start: start.Format("2006-01-02"), End: a.End}
}
