package datagokr

import (
	"cmp"
	"context"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 국민연금공단_국민연금 가입 사업장 내역 (NpsBplcInfoInqireServiceV2)
// https://www.data.go.kr/data/3046071/openapi.do
//
// Quirks handled here:
//   - Every month is a separate record with its own seq, and the search
//     returns the last 12 months mixed together (Samsung Electronics: 58
//     records, 12 of them its head office). dataCrtYm cannot be filtered.
//   - The data trails by about two months (2026-09 in November 2026).
//   - The business registration number is published as its first 6 digits
//     (124810****), shared with other companies (601 workplaces under 124810).
//     The full 10 digits, or hyphens, silently match nothing.
//   - wkplNm is a raw substring match: 삼성전자 also matches contractors'
//     sites such as "(주)도원테크/상용/평택삼성전자 P4", while 삼성전자(주)
//     with the prefix, 삼성 전자 and NAVER (filed as 네이버 주식회사) match
//     nothing.
//   - A company files short-term sites as separate workplaces
//     ("삼성전자(주)/일용/…냉난방기 구매설치"), most of them withdrawn.
//   - No withdrawal date is 00010101; the detail record has no month.
//   - numOfRows above 100 fails with a bare CLIENT_ERROR.
const (
	npsBase       = "/B552015/NpsBplcInfoInqireServiceV2"
	npsSearchPath = npsBase + "/getBassInfoSearchV2"
	npsDetailPath = npsBase + "/getDetailInfoSearchV2"
	npsStatusPath = npsBase + "/getPdAcctoSttusInfoSearchV2"

	npsPageSize = 100 // the API's maximum
	npsMaxPages = 10  // 1,000 records, about 80 workplaces over 12 months
	npsHistory  = 3   // workplaces per call that get a monthly history
)

func registerPension(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_get_pension_workplaces",
		Description: "Look up workplaces enrolled in the National Pension Service (국민연금 가입 사업장): " +
			"subscriber count (a close proxy for employees, including unlisted companies), monthly contributions billed, " +
			"hires and departures that month, industry, address and enrollment dates, with a 12-month history on request. " +
			"Pass the business registration number (사업자등록번호; only its first 6 digits are published, and other companies share them) " +
			"and the company name, e.g. 1248100998 and 삼성전자; opendart_get_company and datagokr_get_corporation return the number. " +
			"A name alone matches every workplace containing it, including contractors' construction sites. " +
			"Data trails by about two months. Contributions are capped by an income ceiling, so they cannot be turned into salaries.",
	}, c.getPensionWorkplaces)
}

type getPensionWorkplacesInput struct {
	BusinessNumber string `json:"business_number,omitempty" jsonschema:"사업자등록번호, 10 digits with or without hyphens, or its first 6 digits"`
	Name           string `json:"name,omitempty" jsonschema:"company or workplace name, e.g. 삼성전자; partial names match, (주) and spacing are ignored"`
	History        bool   `json:"history,omitempty" jsonschema:"add the monthly subscriber count for the last 12 months to the first 3 workplaces"`
	hub.PageParams
}

type npsRecord struct {
	Seq        string `xml:"seq"`
	Month      string `xml:"dataCrtYm"`
	BizNo      string `xml:"bzowrRgstNo"`
	Name       string `xml:"wkplNm"`
	Address    string `xml:"wkplRoadNmDtlAddr"`
	Sido       string `xml:"ldongAddrMgplDgCd"`
	Sigungu    string `xml:"ldongAddrMgplSgguCd"`
	Dong       string `xml:"ldongAddrMgplSgguEmdCd"`
	Status     string `xml:"wkplJnngStcd"`
	Form       string `xml:"wkplStylDvcd"`
	Industry   string `xml:"vldtVlKrnNm"`
	IndustryCd string `xml:"wkplIntpCd"`
	Joined     string `xml:"adptDt"`
	Withdrawn  string `xml:"scsnDt"`
	Members    string `xml:"jnngpCnt"`
	Billed     string `xml:"crrmmNtcAmt"`
	New        string `xml:"nwAcqzrCnt"`
	Lost       string `xml:"lssJnngpCnt"`
}

type PensionWorkplaces struct {
	AsOf     string             `json:"as_of" jsonschema:"latest month in the data, YYYY-MM"`
	Note     string             `json:"note"`
	Items    []PensionWorkplace `json:"items" jsonschema:"exact name matches first, then enrolled workplaces before withdrawn ones and company workplaces before short-term sites"`
	PageInfo hub.PageInfo       `json:"page_info"`
}

type PensionWorkplace struct {
	Name                 string `json:"name"`
	BusinessNumberPrefix string `json:"business_number_prefix" jsonschema:"first 6 digits of the business registration number"`
	Kind                 string `json:"kind" jsonschema:"workplace, or site: a short-term site such as a construction project, filed with 일용 or 상용 in its name"`
	Status               string `json:"status" jsonschema:"enrolled or withdrawn"`
	Month                string `json:"month" jsonschema:"month of this record, YYYY-MM; earlier than as_of if the workplace is no longer listed"`

	Subscribers     *int64 `json:"subscribers,omitempty" jsonschema:"members enrolled at this workplace that month"`
	MonthlyBilled   *int64 `json:"monthly_billed,omitempty" jsonschema:"contributions billed that month in KRW, employer and employee shares together"`
	NewSubscribers  *int64 `json:"new_subscribers,omitempty" jsonschema:"members who joined that month"`
	LostSubscribers *int64 `json:"lost_subscribers,omitempty" jsonschema:"members who left that month"`

	Industry      string `json:"industry,omitempty"`
	IndustryCode  string `json:"industry_code,omitempty"`
	Corporation   *bool  `json:"corporation,omitempty" jsonschema:"true for a corporation (법인), false for a sole proprietor (개인)"`
	Address       string `json:"address,omitempty" jsonschema:"road name address without the building number"`
	LegalDongCode string `json:"legal_dong_code,omitempty" jsonschema:"first 8 digits of the 법정동 code"`
	JoinedDate    string `json:"joined_date,omitempty"`
	WithdrawnDate string `json:"withdrawn_date,omitempty"`

	History []PensionMonth `json:"history,omitempty" jsonschema:"oldest first"`

	seqs map[string]string // month -> seq
}

type PensionMonth struct {
	Month         string `json:"month"`
	Subscribers   *int64 `json:"subscribers"`
	MonthlyBilled *int64 `json:"monthly_billed"`
}

const pensionNote = "National Pension Service data, about two months behind. Subscribers are members aged 18 to 59 enrolled at the workplace, " +
	"so they approximate employees but leave out older staff and some part-timers. Contributions are capped by an income ceiling, " +
	"so dividing them by subscribers does not give an average salary."

func (c *client) getPensionWorkplaces(ctx context.Context, in getPensionWorkplacesInput) (PensionWorkplaces, error) {
	var zero PensionWorkplaces
	page, size, err := in.Resolve(10, 20)
	if err != nil {
		return zero, err
	}
	prefix, err := bizNoPrefix(in.BusinessNumber)
	if err != nil {
		return zero, err
	}
	name := normalize.StripCompanyForm(in.Name)
	if prefix == "" && name == "" {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "pass business_number, name, or both").
			WithHint("opendart_get_company or datagokr_get_corporation returns the business registration number")
	}

	q := url.Values{}
	if prefix != "" {
		q.Set("bzowrRgstNo", prefix)
	}
	if name != "" {
		q.Set("wkplNm", name)
	}
	var records []npsRecord
	total, truncated := 0, false
	for p := 1; ; p++ {
		items, n, err := getList[npsRecord](ctx, c, npsSearchPath, maps.Clone(q), p, npsPageSize)
		if err != nil {
			return zero, err
		}
		records, total = append(records, items...), n
		if p*npsPageSize >= total || len(items) == 0 {
			break
		}
		if p == npsMaxPages {
			truncated = true
			break
		}
	}

	all := groupWorkplaces(records)
	out := PensionWorkplaces{Note: pensionNote, Items: []PensionWorkplace{}}
	for _, w := range all {
		out.AsOf = max(out.AsOf, w.Month)
	}
	if truncated {
		out.Note += " The name matched " + strconv.Itoa(total) + " records; only the latest " + strconv.Itoa(len(records)) +
			" were read. Add business_number or a longer name."
	}
	key := normalize.CompanyKey(in.Name)
	slices.SortStableFunc(all, func(a, b PensionWorkplace) int {
		return cmp.Or(
			boolOrder(normalize.CompanyKey(a.Name) == key, normalize.CompanyKey(b.Name) == key),
			boolOrder(a.Status == "enrolled", b.Status == "enrolled"),
			boolOrder(a.Kind == "workplace", b.Kind == "workplace"),
			cmp.Compare(b.Month, a.Month),
			cmp.Compare(a.Name, b.Name),
		)
	})

	items := pageOfWorkplaces(all, page, size)
	if err := c.fillWorkplaces(ctx, items, in.History); err != nil {
		return zero, err
	}
	for i := range items {
		items[i].Month = monthDash(items[i].Month)
	}
	out.AsOf = monthDash(out.AsOf)
	out.Items = items
	out.PageInfo = hub.NewPageInfo(page, size, len(all))
	return out, nil
}

// bizNoPrefix returns the first 6 digits of a business registration number
// given as 10 digits (hyphens allowed) or as its first 6.
func bizNoPrefix(s string) (string, error) {
	d := strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s))
	if d == "" {
		return "", nil
	}
	if (len(d) != 10 && len(d) != 6) || strings.ContainsFunc(d, func(r rune) bool { return r < '0' || r > '9' }) {
		return "", hub.Errorf(hub.CodeInvalidArgument, "business_number must be 10 digits (사업자등록번호) or its first 6, got %q", s)
	}
	return d[:6], nil
}

// groupWorkplaces folds monthly records into one workplace each, described
// by its latest record. A workplace keeps its name, address and number
// prefix from month to month while its seq changes.
func groupWorkplaces(records []npsRecord) []PensionWorkplace {
	idx := map[string]int{}
	var out []PensionWorkplace
	for _, r := range records {
		k := strings.Join([]string{r.BizNo, r.Name, r.Address, r.Sido, r.Sigungu, r.Dong}, "\x00")
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			out = append(out, PensionWorkplace{seqs: map[string]string{}})
			i = len(out) - 1
		}
		w := &out[i]
		w.seqs[r.Month] = r.Seq
		if r.Month < w.Month {
			continue
		}
		*w = PensionWorkplace{
			Name:                 strings.TrimSpace(r.Name),
			BusinessNumberPrefix: strings.TrimRight(r.BizNo, "*"),
			Kind:                 workplaceKind(r.Name),
			Status:               map[string]string{"1": "enrolled", "2": "withdrawn"}[r.Status],
			Month:                r.Month,
			Address:              strings.TrimSpace(r.Address),
			seqs:                 w.seqs,
		}
		if r.Sido != "" && r.Sigungu != "" && r.Dong != "" {
			w.LegalDongCode = r.Sido + r.Sigungu + r.Dong
		}
		switch r.Form {
		case "1":
			w.Corporation = new(true)
		case "2":
			w.Corporation = new(false)
		}
	}
	return out
}

// workplaceKind tells a company's own workplace from a short-term site,
// which carries 일용 (daily workers) or 상용 (regular workers) in its name.
func workplaceKind(name string) string {
	if sitePattern.MatchString(name) {
		return "site"
	}
	return "workplace"
}

// sitePattern finds 일용 or 상용 set off as a label: "/일용/", "-(일용)",
// "(상용)", but not inside a company name such as 상용건설.
var sitePattern = regexp.MustCompile(`[/(（-]\s*(일용|상용)|(일용|상용)\s*[/)）]`)

// fillWorkplaces adds the detail and monthly status of each workplace, and
// the monthly history of the first few when asked.
func (c *client) fillWorkplaces(ctx context.Context, items []PensionWorkplace, history bool) error {
	type job struct {
		item  int
		month string
		seq   string
		full  bool // detail, and the month's hires and departures
	}
	var jobs []job
	for i, w := range items {
		jobs = append(jobs, job{i, w.Month, w.seqs[w.Month], true})
		if history && i < npsHistory {
			for m, seq := range w.seqs {
				if m != w.Month {
					jobs = append(jobs, job{i, m, seq, false})
				}
			}
		}
	}
	type result struct{ detail, status *npsRecord }
	results := make([]result, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for j, jb := range jobs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[j].detail, errs[j] = c.npsOne(ctx, npsDetailPath, jb.seq)
			if errs[j] == nil && jb.full {
				results[j].status, errs[j] = c.npsOne(ctx, npsStatusPath, jb.seq)
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	for j, jb := range jobs {
		w, d := &items[jb.item], results[j].detail
		if history && jb.item < npsHistory && d != nil {
			w.History = append(w.History, PensionMonth{Month: monthDash(jb.month), Subscribers: normalize.Int(d.Members), MonthlyBilled: normalize.Int(d.Billed)})
		}
		if !jb.full {
			continue
		}
		if d != nil {
			w.Subscribers, w.MonthlyBilled = normalize.Int(d.Members), normalize.Int(d.Billed)
			w.Industry, w.IndustryCode = strings.TrimSpace(d.Industry), strings.TrimSpace(d.IndustryCd)
			w.JoinedDate, w.WithdrawnDate = npsDate(d.Joined), npsDate(d.Withdrawn)
		}
		if s := results[j].status; s != nil {
			w.NewSubscribers, w.LostSubscribers = normalize.Int(s.New), normalize.Int(s.Lost)
		}
	}
	for i := range items {
		slices.SortFunc(items[i].History, func(a, b PensionMonth) int { return cmp.Compare(a.Month, b.Month) })
	}
	return nil
}

// npsOne fetches the single record of a detail or status call, or nil.
func (c *client) npsOne(ctx context.Context, path, seq string) (*npsRecord, error) {
	items, _, err := getList[npsRecord](ctx, c, path, url.Values{"seq": {seq}}, 1, 10)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

// npsDate formats YYYYMMDD; 00010101 means no date.
func npsDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "00010101" {
		return ""
	}
	return normalize.Date(s)
}

func monthDash(ym string) string {
	if len(ym) != 6 {
		return ym
	}
	return ym[:4] + "-" + ym[4:]
}

func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

func pageOfWorkplaces(all []PensionWorkplace, page, size int) []PensionWorkplace {
	from := min((page-1)*size, len(all))
	return append([]PensionWorkplace{}, all[from:min(from+size, len(all))]...)
}
