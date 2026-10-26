package datagokr

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 금융위원회_기업기본정보 (GetCorpBasicInfoService_V2)
// https://www.data.go.kr/data/15043184/openapi.do
//
// The service keeps a history: each company has one record per period
// (fstOpegDt..lastOpegDt), returned in no particular order, and older or
// partial records disagree with the current one (former CEOs, missing
// market). Every tool here collapses them to the latest record.
const corpOutlinePath = "/1160100/service/GetCorpBasicInfoService_V2/getCorpOutline_V2"

// maxCorpRecords bounds a name search. corpNm is a substring match, so a
// broad name such as 삼성 matches thousands of records (5.8 MB of XML).
const maxCorpRecords = 1000

func registerCorporations(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_search_corporations",
		Description: "Search Korean corporations by Korean name in the Financial Services Commission's company registry (기업기본정보). " +
			"Covers listed and many unlisted companies. Returns the corporate registration number (법인등록번호, crno) " +
			"for datagokr_get_corporation, and dart_corp_code for opendart tools when the company files with DART. " +
			"Exact matches and listed companies come first. Spacing and (주)/주식회사 are ignored.",
	}, c.searchCorporations)

	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_get_corporation",
		Description: "Get the current profile of a Korean corporation by corporate registration number (법인등록번호, crno): " +
			"name, CEO, market, business registration number, founding and listing dates, address, industry, " +
			"employees, average tenure and salary, auditor and audit opinion. as_of tells how recent the record is; " +
			"some companies were last updated years ago.",
	}, c.getCorporation)
}

type corpOutline struct {
	Crno             string `xml:"crno"`
	CorpNm           string `xml:"corpNm"`
	CorpEnsnNm       string `xml:"corpEnsnNm"`
	EnpPbanCmpyNm    string `xml:"enpPbanCmpyNm"`
	EnpRprFnm        string `xml:"enpRprFnm"`
	CorpRegMrktDcdNm string `xml:"corpRegMrktDcdNm"`
	CorpDcdNm        string `xml:"corpDcdNm"`
	Bzno             string `xml:"bzno"`
	EnpOzpno         string `xml:"enpOzpno"`
	EnpBsadr         string `xml:"enpBsadr"`
	EnpDtadr         string `xml:"enpDtadr"`
	EnpHmpgUrl       string `xml:"enpHmpgUrl"`
	EnpTlno          string `xml:"enpTlno"`
	EnpFxno          string `xml:"enpFxno"`
	SicNm            string `xml:"sicNm"`
	EnpMainBizNm     string `xml:"enpMainBizNm"`
	EnpEstbDt        string `xml:"enpEstbDt"`
	EnpStacMm        string `xml:"enpStacMm"`
	EnpXchgLstgDt    string `xml:"enpXchgLstgDt"`
	EnpXchgLstgAbol  string `xml:"enpXchgLstgAbolDt"`
	EnpKosdaqLstgDt  string `xml:"enpKosdaqLstgDt"`
	EnpKosdaqAbol    string `xml:"enpKosdaqLstgAbolDt"`
	EnpKrxLstgDt     string `xml:"enpKrxLstgDt"`
	EnpKrxAbol       string `xml:"enpKrxLstgAbolDt"`
	SmenpYn          string `xml:"smenpYn"`
	EnpMntrBnkNm     string `xml:"enpMntrBnkNm"`
	EnpEmpeCnt       string `xml:"enpEmpeCnt"`
	EmpeAvgCnwkTerm  string `xml:"empeAvgCnwkTermCtt"`
	EnpPn1AvgSlryAmt string `xml:"enpPn1AvgSlryAmt"`
	ActnAudpnNm      string `xml:"actnAudpnNm"`
	AudtRptOpnnCtt   string `xml:"audtRptOpnnCtt"`
	FssCorpUnqNo     string `xml:"fssCorpUnqNo"`
	FstOpegDt        string `xml:"fstOpegDt"`
	LastOpegDt       string `xml:"lastOpegDt"`
}

// latest collapses history records to the newest record per crno, keeping
// the order in which companies first appear.
func latest(records []corpOutline) []corpOutline {
	idx := map[string]int{}
	var out []corpOutline
	for _, r := range records {
		i, ok := idx[r.Crno]
		if !ok {
			idx[r.Crno] = len(out)
			out = append(out, r)
			continue
		}
		cur := out[i]
		if r.LastOpegDt > cur.LastOpegDt || (r.LastOpegDt == cur.LastOpegDt && r.FstOpegDt > cur.FstOpegDt) {
			out[i] = r
		}
	}
	return out
}

var corpMarkets = map[string]string{"유가": "KOSPI", "코스닥": "KOSDAQ", "코넥스": "KONEX", "기타": "OTHER"}

func (r corpOutline) market() string { return corpMarkets[strings.TrimSpace(r.CorpRegMrktDcdNm)] }

func (r corpOutline) listed() bool {
	m := r.market()
	return m == "KOSPI" || m == "KOSDAQ" || m == "KONEX"
}

// ---- datagokr_search_corporations ----

type searchCorporationsInput struct {
	Query      string `json:"query" jsonschema:"company name in Korean, e.g. 삼성전자 or 카카오. Partial names match."`
	ListedOnly bool   `json:"listed_only,omitempty" jsonschema:"only companies listed on KOSPI, KOSDAQ or KONEX"`
	hub.PageParams
}

type Corporation struct {
	Crno         string `json:"crno" jsonschema:"corporate registration number (법인등록번호), 13 digits"`
	Name         string `json:"name"`
	NameEn       string `json:"name_en,omitempty"`
	Market       string `json:"market,omitempty" jsonschema:"KOSPI, KOSDAQ, KONEX or OTHER (not listed)"`
	BizRegNo     string `json:"business_registration_no,omitempty" jsonschema:"사업자등록번호, 10 digits"`
	CEO          string `json:"ceo,omitempty"`
	DartCorpCode string `json:"dart_corp_code,omitempty" jsonschema:"corp_code for opendart tools"`
	AsOf         string `json:"as_of,omitempty" jsonschema:"date the record was last confirmed, YYYY-MM-DD"`
}

func (c *client) searchCorporations(ctx context.Context, in searchCorporationsInput) (hub.List[Corporation], error) {
	page, size, err := in.Resolve(10, 50)
	if err != nil {
		return hub.List[Corporation]{}, err
	}
	name := normalize.StripCompanyForm(in.Query)
	if name == "" {
		return hub.List[Corporation]{}, hub.Errorf(hub.CodeInvalidArgument, "query is required")
	}
	records, total, err := getList[corpOutline](ctx, c, corpOutlinePath, url.Values{"corpNm": {name}}, 1, maxCorpRecords)
	if err != nil {
		return hub.List[Corporation]{}, err
	}
	if total > maxCorpRecords {
		return hub.List[Corporation]{}, hub.Errorf(hub.CodeInvalidArgument,
			"%q matches %d records, too many to rank; use a more specific name", in.Query, total).
			WithHint("use the full company name, e.g. 삼성전자 instead of 삼성")
	}

	q := normalize.CompanyName(in.Query)
	type hit struct {
		r     corpOutline
		score int
	}
	var hits []hit
	for _, r := range latest(records) {
		if in.ListedOnly && !r.listed() {
			continue
		}
		n := normalize.CompanyName(r.CorpNm)
		score := 2
		if n == q {
			score = 0
		} else if strings.HasPrefix(n, q) {
			score = 1
		}
		hits = append(hits, hit{r, score})
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		return cmp.Or(
			cmp.Compare(a.score, b.score),
			compareBool(a.r.listed(), b.r.listed()),
			cmp.Compare(len(a.r.CorpNm), len(b.r.CorpNm)),
			cmp.Compare(a.r.CorpNm, b.r.CorpNm),
		)
	})

	out := hub.List[Corporation]{Items: []Corporation{}, PageInfo: hub.NewPageInfo(page, size, len(hits))}
	for i := (page - 1) * size; i < len(hits) && i < page*size; i++ {
		r := hits[i].r
		out.Items = append(out.Items, Corporation{
			Crno:         r.Crno,
			Name:         text(r.CorpNm),
			NameEn:       text(r.CorpEnsnNm),
			Market:       r.market(),
			BizRegNo:     text(r.Bzno),
			CEO:          text(r.EnpRprFnm),
			DartCorpCode: text(r.FssCorpUnqNo),
			AsOf:         date(r.LastOpegDt),
		})
	}
	return out, nil
}

// compareBool orders true before false.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// ---- datagokr_get_corporation ----

type getCorporationInput struct {
	Crno string `json:"crno" jsonschema:"corporate registration number (법인등록번호), 13 digits; hyphens allowed, e.g. 130111-0006246"`
}

type CorporationProfile struct {
	Corporation
	ShortName          string   `json:"short_name,omitempty" jsonschema:"name used in disclosures"`
	CorporateForm      string   `json:"corporate_form,omitempty" jsonschema:"e.g. 주식회사, 유한회사"`
	IsSME              *bool    `json:"is_sme,omitempty" jsonschema:"small or medium-sized enterprise (중소기업)"`
	EstablishedDate    string   `json:"established_date,omitempty"`
	ListedDate         string   `json:"listed_date,omitempty"`
	DelistedDate       string   `json:"delisted_date,omitempty"`
	FiscalYearEndMonth *int     `json:"fiscal_year_end_month,omitempty"`
	Address            string   `json:"address,omitempty"`
	PostalCode         string   `json:"postal_code,omitempty"`
	Phone              string   `json:"phone,omitempty"`
	Fax                string   `json:"fax,omitempty"`
	Homepage           string   `json:"homepage,omitempty"`
	Industry           string   `json:"industry,omitempty" jsonschema:"industry classification (표준산업분류)"`
	MainBusiness       string   `json:"main_business,omitempty"`
	Employees          *int64   `json:"employees,omitempty"`
	AvgTenureYears     *float64 `json:"avg_tenure_years,omitempty"`
	AvgSalary          *int64   `json:"avg_salary,omitempty" jsonschema:"average annual salary per employee, KRW"`
	MainBank           string   `json:"main_bank,omitempty"`
	Auditor            string   `json:"auditor,omitempty"`
	AuditOpinion       string   `json:"audit_opinion,omitempty"`
}

var crnoPattern = regexp.MustCompile(`^\d{13}$`)

func (c *client) getCorporation(ctx context.Context, in getCorporationInput) (CorporationProfile, error) {
	crno := strings.ReplaceAll(strings.TrimSpace(in.Crno), "-", "")
	if !crnoPattern.MatchString(crno) {
		return CorporationProfile{}, hub.Errorf(hub.CodeInvalidArgument, "crno must be a 13-digit corporate registration number, got %q", in.Crno).
			WithHint("find it with datagokr_search_corporations")
	}
	// One company has a few dozen history records at most.
	records, _, err := getList[corpOutline](ctx, c, corpOutlinePath, url.Values{"crno": {crno}}, 1, 100)
	if err != nil {
		return CorporationProfile{}, err
	}
	all := latest(records)
	if len(all) == 0 {
		return CorporationProfile{}, hub.Errorf(hub.CodeNotFound, "no corporation with crno %s", crno).
			WithHint("check the number, or find it with datagokr_search_corporations")
	}
	r := all[0]

	p := CorporationProfile{
		Corporation: Corporation{
			Crno:         r.Crno,
			Name:         text(r.CorpNm),
			NameEn:       text(r.CorpEnsnNm),
			Market:       r.market(),
			BizRegNo:     text(r.Bzno),
			CEO:          text(r.EnpRprFnm),
			DartCorpCode: text(r.FssCorpUnqNo),
			AsOf:         date(r.LastOpegDt),
		},
		ShortName:       text(r.EnpPbanCmpyNm),
		CorporateForm:   text(r.CorpDcdNm),
		EstablishedDate: date(r.EnpEstbDt),
		ListedDate:      cmp.Or(date(r.EnpXchgLstgDt), date(r.EnpKosdaqLstgDt), date(r.EnpKrxLstgDt)),
		DelistedDate:    cmp.Or(date(r.EnpXchgLstgAbol), date(r.EnpKosdaqAbol), date(r.EnpKrxAbol)),
		PostalCode:      text(r.EnpOzpno),
		Phone:           text(r.EnpTlno),
		Fax:             text(r.EnpFxno),
		Homepage:        text(r.EnpHmpgUrl),
		Industry:        text(r.SicNm),
		MainBusiness:    text(r.EnpMainBizNm),
		Employees:       positive(r.EnpEmpeCnt),
		AvgSalary:       positive(r.EnpPn1AvgSlryAmt),
		MainBank:        text(r.EnpMntrBnkNm),
		Auditor:         text(r.ActnAudpnNm),
		AuditOpinion:    text(r.AudtRptOpnnCtt),
	}
	p.Address = text(r.EnpBsadr)
	if dt := text(r.EnpDtadr); dt != "" && !strings.Contains(p.Address, dt) {
		p.Address = strings.TrimSpace(p.Address + " " + dt)
	}
	switch strings.TrimSpace(r.SmenpYn) {
	case "Y":
		p.IsSME = new(true)
	case "N":
		p.IsSME = new(false)
	}
	if m, err := strconv.Atoi(strings.TrimSpace(r.EnpStacMm)); err == nil && m >= 1 && m <= 12 {
		p.FiscalYearEndMonth = &m
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(r.EmpeAvgCnwkTerm), 64); err == nil && f > 0 {
		p.AvgTenureYears = &f
	}
	return p, nil
}

// text trims and collapses whitespace; data.go.kr pads empty fields with
// spaces and addresses with double spaces.
func text(s string) string { return normalize.Text(s) }

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// date normalizes a date and drops values that are not dates at all (some
// founding dates are just "99").
func date(s string) string {
	if d := normalize.Date(s); isoDate.MatchString(d) {
		return d
	}
	return ""
}

// positive parses a count or amount where the service uses 0 for "unknown".
func positive(s string) *int64 {
	if n := normalize.Int(s); n != nil && *n > 0 {
		return n
	}
	return nil
}
