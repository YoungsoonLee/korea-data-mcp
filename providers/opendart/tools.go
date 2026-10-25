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

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTools(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "opendart_search_companies",
		Description: "Find a Korean company's corp_code (고유번호), the 8-digit ID that every other opendart tool needs. " +
			"Matches Korean or English company names, stock codes (종목코드), or corp_codes. " +
			"Exact matches and listed companies come first. The first call downloads DART's full company list and can take a few seconds.",
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
		Description: "Get key financial statement accounts (주요계정) for one company and period: assets, liabilities, equity, revenue, " +
			"operating income, net income, with prior-period comparisons. Amounts are in KRW (원). Data is available from fiscal year 2015.",
	}, c.getFinancials)
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
	StockCode    string `json:"stock_code,omitempty"`
	Listed       bool   `json:"listed"`
	ModifiedDate string `json:"modified_date,omitempty"`
}

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
	out := hub.List[Company]{Items: []Company{}, PageInfo: hub.NewPageInfo(page, size, len(matches))}
	for _, m := range pageOf(matches, page, size) {
		out.Items = append(out.Items, Company{
			CorpCode:     m.Code,
			CorpName:     m.Name,
			CorpEngName:  m.EngName,
			StockCode:    m.StockCode,
			Listed:       m.StockCode != "",
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
	StockCode             string `json:"stock_code,omitempty"`
	Market                string `json:"market" jsonschema:"KOSPI, KOSDAQ, KONEX, or OTHER (unlisted)"`
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

type getFinancialsInput struct {
	CorpCode  string `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Year      int    `json:"year" jsonschema:"fiscal year (사업연도), 2015 or later"`
	Report    string `json:"report,omitempty" jsonschema:"one of: annual (default, 사업보고서), half (반기), q1 (1분기), q3 (3분기)"`
	Statement string `json:"statement,omitempty" jsonschema:"one of: consolidated (default, 연결재무제표), separate (별도/개별재무제표)"`
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
		BfefrmtrmAmount string `json:"bfefrmtrm_amount"`
		Currency        string `json:"currency"`
	} `json:"list"`
}

type Financials struct {
	CorpCode  string    `json:"corp_code"`
	Year      int       `json:"year"`
	Report    string    `json:"report"`
	Statement string    `json:"statement"`
	Currency  string    `json:"currency"`
	ReceiptNo string    `json:"receipt_no"`
	URL       string    `json:"url"`
	Accounts  []Account `json:"accounts"`
}

type Account struct {
	Name                    string `json:"name" jsonschema:"account name in Korean, e.g. 자산총계, 매출액, 영업이익, 당기순이익"`
	StatementType           string `json:"statement_type" jsonschema:"balance_sheet or income_statement"`
	CurrentPeriod           string `json:"current_period"`
	CurrentAmount           *int64 `json:"current_amount"`
	CurrentCumulativeAmount *int64 `json:"current_cumulative_amount,omitempty" jsonschema:"year-to-date amount, in quarterly and half-year reports"`
	PriorPeriod             string `json:"prior_period,omitempty"`
	PriorAmount             *int64 `json:"prior_amount"`
	BeforePriorAmount       *int64 `json:"before_prior_amount,omitempty" jsonschema:"two periods back, annual reports only"`
}

var statementTypes = map[string]string{"BS": "balance_sheet", "IS": "income_statement"}

func (c *client) getFinancials(ctx context.Context, in getFinancialsInput) (Financials, error) {
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return Financials{}, err
	}
	if thisYear := c.now().In(kst).Year(); in.Year < 2015 || in.Year > thisYear {
		return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "year must be between 2015 and %d, got %d", thisYear, in.Year)
	}
	report := cmp.Or(in.Report, "annual")
	reprt, ok := reportCodes[report]
	if !ok {
		return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "unknown report %q; use one of %s", report, keys(reportCodes))
	}
	statement := cmp.Or(in.Statement, "consolidated")
	fsDiv := map[string]string{"consolidated": "CFS", "separate": "OFS"}[statement]
	if fsDiv == "" {
		return Financials{}, hub.Errorf(hub.CodeInvalidArgument, "unknown statement %q; use consolidated or separate", statement)
	}

	var r financialsResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}}
	if err := c.getJSON(ctx, "fnlttSinglAcnt.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Hint = "the report may not be filed yet or the company does not file it; try an earlier year or report=annual"
		}
		return Financials{}, err
	}

	out := Financials{CorpCode: code, Year: in.Year, Report: report, Statement: statement, Accounts: []Account{}}
	other := 0
	for _, a := range r.List {
		if a.FsDiv != fsDiv {
			other++
			continue
		}
		out.Currency, out.ReceiptNo = a.Currency, a.RceptNo
		out.Accounts = append(out.Accounts, Account{
			Name:                    a.AccountNm,
			StatementType:           cmp.Or(statementTypes[a.SjDiv], a.SjDiv),
			CurrentPeriod:           period(a.ThstrmDt),
			CurrentAmount:           normalize.Int(a.ThstrmAmount),
			CurrentCumulativeAmount: normalize.Int(a.ThstrmAddAmount),
			PriorPeriod:             period(a.FrmtrmDt),
			PriorAmount:             normalize.Int(a.FrmtrmAmount),
			BeforePriorAmount:       normalize.Int(a.BfefrmtrmAmount),
		})
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

// period turns "2023.12.31 현재" into "2023-12-31" and
// "2023.01.01 ~ 2023.12.31" into "2023-01-01 ~ 2023-12-31".
func period(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "현재"))
	return dotDate.ReplaceAllStringFunc(s, normalize.Date)
}
