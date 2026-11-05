package opendart

import (
	"cmp"
	"context"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// ---- opendart_get_ownership_reports ----
//
// 대량보유 상황보고 (majorstock.json) and 임원·주요주주 소유보고
// (elestock.json) take only a company. Quirks handled here:
//   - Both return about the last two years of reports. A holder whose last
//     report is older is missing: the National Pension Service's 2022 report
//     on Samsung Electronics, though it still holds more than 5%. The
//     disclosure list (list.json) goes back further and names them.
//   - A holder's latest report is its current stake; a report below 5% ends
//     the reporting duty, and a change of the representative reporter files
//     0.00% under the old name (The Capital Group → Capital Research on SK hynix).
//   - stkrt is the reporter together with its related parties.
//   - Insider reports are many (3,368 for Samsung in two years, mostly
//     stock grants to executives), positions carry trailing spaces, and the
//     main shareholder field is as filed: some executives holding 1,000
//     shares are marked 10%이상주주.

type getOwnershipReportsInput struct {
	CorpCode  string `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Kind      string `json:"kind,omitempty" jsonschema:"one of: major_holders (default, 5% 대량보유 상황보고), insiders (임원·주요주주 소유보고)"`
	Reporter  string `json:"reporter,omitempty" jsonschema:"only reports by this reporter; part of the name, ignoring spaces and case"`
	StartDate string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD, reports filed from this date"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD, reports filed until this date"`
	Direction string `json:"direction,omitempty" jsonschema:"one of: increase, decrease; only reports whose shares went up or down"`
	hub.PageParams
}

type majorStockResponse struct {
	apiStatus
	List []struct {
		RceptNo    string `json:"rcept_no"`
		RceptDt    string `json:"rcept_dt"`
		CorpName   string `json:"corp_name"`
		ReportTp   string `json:"report_tp"`
		Repror     string `json:"repror"`
		Stkqy      string `json:"stkqy"`
		StkqyIrds  string `json:"stkqy_irds"`
		Stkrt      string `json:"stkrt"`
		StkrtIrds  string `json:"stkrt_irds"`
		CtrStkqy   string `json:"ctr_stkqy"`
		CtrStkrt   string `json:"ctr_stkrt"`
		ReportResn string `json:"report_resn"`
	} `json:"list"`
}

type insiderResponse struct {
	apiStatus
	List []struct {
		RceptNo      string `json:"rcept_no"`
		RceptDt      string `json:"rcept_dt"`
		CorpName     string `json:"corp_name"`
		Repror       string `json:"repror"`
		Registered   string `json:"isu_exctv_rgist_at"`
		Position     string `json:"isu_exctv_ofcps"`
		MainHolder   string `json:"isu_main_shrholdr"`
		Shares       string `json:"sp_stock_lmp_cnt"`
		SharesChange string `json:"sp_stock_lmp_irds_cnt"`
		Rate         string `json:"sp_stock_lmp_rate"`
		RateChange   string `json:"sp_stock_lmp_irds_rate"`
	} `json:"list"`
}

type OwnershipReports struct {
	CorpCode       string            `json:"corp_code"`
	CorpName       string            `json:"corp_name,omitempty"`
	Kind           string            `json:"kind"`
	Note           string            `json:"note"`
	Holders        []BlockHolder     `json:"holders,omitempty" jsonschema:"major_holders: each reporter's latest report, largest stake first"`
	OlderReporters []OlderReporter   `json:"older_reporters,omitempty" jsonschema:"major_holders: reporters whose last report is older than the reports returned; they may still hold 5% or more, or may have sold"`
	Reports        []OwnershipReport `json:"reports" jsonschema:"newest first"`
	PageInfo       hub.PageInfo      `json:"page_info"`
}

type BlockHolder struct {
	Reporter   string   `json:"reporter" jsonschema:"the representative reporter; the stake includes its related parties (특별관계자)"`
	Status     string   `json:"status" jsonschema:"holding (5% or more), below_5_percent (reporting duty ended), or exited (0%, also filed when the representative reporter changes)"`
	Percent    *float64 `json:"percent"`
	Shares     *int64   `json:"shares"`
	Date       string   `json:"date"`
	ReportType string   `json:"report_type" jsonschema:"일반, or 약식 (short form, for passive investors such as funds and the National Pension Service)"`
	Reason     string   `json:"reason,omitempty"`
	ReceiptNo  string   `json:"receipt_no"`
	URL        string   `json:"url"`
}

type OlderReporter struct {
	Reporter   string `json:"reporter"`
	Date       string `json:"date" jsonschema:"its last report"`
	ReportName string `json:"report_name"`
	ReceiptNo  string `json:"receipt_no"`
	URL        string `json:"url"`
}

type OwnershipReport struct {
	Date      string `json:"date"`
	Reporter  string `json:"reporter"`
	ReceiptNo string `json:"receipt_no"`
	URL       string `json:"url"`

	Shares        *int64   `json:"shares" jsonschema:"shares (특정증권등, including convertible securities) held after the report"`
	SharesChange  *int64   `json:"shares_change"`
	Percent       *float64 `json:"percent" jsonschema:"stake after the report, percent"`
	PercentChange *float64 `json:"percent_change"`

	// major_holders
	ReportType      string   `json:"report_type,omitempty" jsonschema:"major_holders: 일반 or 약식"`
	ContractShares  *int64   `json:"contract_shares,omitempty" jsonschema:"major_holders: shares under contracts such as pledges (주요계약)"`
	ContractPercent *float64 `json:"contract_percent,omitempty"`
	Reason          string   `json:"reason,omitempty" jsonschema:"major_holders: reason for the report"`

	// insiders
	Position        string `json:"position,omitempty" jsonschema:"insiders: e.g. 상무, 부사장, 회장"`
	Registered      *bool  `json:"registered,omitempty" jsonschema:"insiders: a registered director (등기임원)"`
	MainShareholder string `json:"main_shareholder,omitempty" jsonschema:"insiders: as filed, e.g. 10%이상주주, 사실상지배주주; not checked against the stake"`
}

const (
	windowNote   = "OpenDART returns only about the last two years of these reports."
	holdersNote  = windowNote + " A holder whose last report is older is in older_reporters, without its stake. Stakes include the reporter's related parties."
	insidersNote = windowNote + " Shares include convertible securities; most reports of executives are stock grants."
)

func (c *client) getOwnershipReports(ctx context.Context, in getOwnershipReportsInput) (OwnershipReports, error) {
	var zero OwnershipReports
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return zero, err
	}
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return zero, err
	}
	kind := cmp.Or(strings.TrimSpace(in.Kind), "major_holders")
	if kind != "major_holders" && kind != "insiders" {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown kind %q; use major_holders or insiders", in.Kind)
	}
	if in.Direction != "" && in.Direction != "increase" && in.Direction != "decrease" {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown direction %q; use increase or decrease", in.Direction)
	}
	var from, to string
	if in.StartDate != "" {
		t, err := parseDate("start_date", in.StartDate)
		if err != nil {
			return zero, err
		}
		from = t.Format(time.DateOnly)
	}
	if in.EndDate != "" {
		t, err := parseDate("end_date", in.EndDate)
		if err != nil {
			return zero, err
		}
		to = t.Format(time.DateOnly)
	}
	keep := func(date, reporter string, change *int64) bool {
		switch {
		case from != "" && date < from, to != "" && date > to:
			return false
		case in.Reporter != "" && !strings.Contains(nameKey(reporter), nameKey(in.Reporter)):
			return false
		case in.Direction == "increase" && (change == nil || *change <= 0),
			in.Direction == "decrease" && (change == nil || *change >= 0):
			return false
		}
		return true
	}

	out := OwnershipReports{CorpCode: code, Kind: kind}
	var reports []OwnershipReport
	q := url.Values{"corp_code": {code}}
	if kind == "insiders" {
		out.Note = insidersNote
		var r insiderResponse
		if err := c.getJSON(ctx, "elestock.json", q, &r); err != nil && hub.AsError(err).Code != hub.CodeNotFound {
			return zero, err
		}
		for _, row := range r.List {
			out.CorpName = cmp.Or(out.CorpName, row.CorpName)
			rep := OwnershipReport{
				Date: normalize.Date(row.RceptDt), Reporter: holderName(row.Repror), ReceiptNo: row.RceptNo, URL: viewer(row.RceptNo),
				Shares: normalize.Int(row.Shares), SharesChange: normalize.Int(row.SharesChange),
				Percent: decimal(row.Rate), PercentChange: decimal(row.RateChange),
				Position:        strings.Join(strings.Fields(row.Position), " "),
				MainShareholder: strings.TrimSpace(strings.Trim(strings.TrimSpace(row.MainHolder), "-")),
			}
			switch strings.TrimSpace(row.Registered) {
			case "등기임원":
				rep.Registered = new(true)
			case "비등기임원":
				rep.Registered = new(false)
			}
			if keep(rep.Date, rep.Reporter, rep.SharesChange) {
				reports = append(reports, rep)
			}
		}
	} else {
		out.Note = holdersNote
		var r majorStockResponse
		if err := c.getJSON(ctx, "majorstock.json", q, &r); err != nil && hub.AsError(err).Code != hub.CodeNotFound {
			return zero, err
		}
		latest := map[string]BlockHolder{}
		for _, row := range r.List {
			out.CorpName = cmp.Or(out.CorpName, row.CorpName)
			rep := OwnershipReport{
				Date: normalize.Date(row.RceptDt), Reporter: strings.TrimSpace(row.Repror), ReceiptNo: row.RceptNo, URL: viewer(row.RceptNo),
				Shares: normalize.Int(row.Stkqy), SharesChange: normalize.Int(row.StkqyIrds),
				Percent: decimal(row.Stkrt), PercentChange: decimal(row.StkrtIrds),
				ReportType: strings.TrimSpace(row.ReportTp), ContractShares: normalize.Int(row.CtrStkqy), ContractPercent: decimal(row.CtrStkrt),
				Reason: normalize.Text(strings.TrimLeft(strings.TrimSpace(row.ReportResn), "- ")),
			}
			if keep(rep.Date, rep.Reporter, rep.SharesChange) {
				reports = append(reports, rep)
			}
			if in.Reporter != "" && !strings.Contains(nameKey(rep.Reporter), nameKey(in.Reporter)) {
				continue
			}
			if h, ok := latest[nameKey(rep.Reporter)]; !ok || rep.ReceiptNo > h.ReceiptNo {
				latest[nameKey(rep.Reporter)] = BlockHolder{
					Reporter: rep.Reporter, Status: holderStatus(rep.Percent), Percent: rep.Percent, Shares: rep.Shares, Date: rep.Date,
					ReportType: rep.ReportType, Reason: rep.Reason, ReceiptNo: rep.ReceiptNo, URL: rep.URL,
				}
			}
		}
		out.Holders = slices.SortedFunc(maps.Values(latest), func(a, b BlockHolder) int {
			return cmp.Or(cmp.Compare(pct(b.Percent), pct(a.Percent)), cmp.Compare(a.Reporter, b.Reporter))
		})
		older, err := c.olderReporters(ctx, code, latest)
		if err != nil {
			return zero, err
		}
		for _, o := range older {
			if in.Reporter == "" || strings.Contains(nameKey(o.Reporter), nameKey(in.Reporter)) {
				out.OlderReporters = append(out.OlderReporters, o)
			}
		}
	}
	slices.SortStableFunc(reports, func(a, b OwnershipReport) int { return cmp.Compare(b.ReceiptNo, a.ReceiptNo) })
	out.Reports = append([]OwnershipReport{}, pageOf(reports, page, size)...)
	out.PageInfo = hub.NewPageInfo(page, size, len(reports))
	return out, nil
}

// maxListPages bounds the disclosure list scan for older reporters.
const maxListPages = 5

// olderReporters finds 5% reporters in the disclosure list (주식등의대량보유
// 상황보고서, since 2015) that majorstock.json no longer returns.
func (c *client) olderReporters(ctx context.Context, code string, known map[string]BlockHolder) ([]OlderReporter, error) {
	seen := map[string]bool{}
	var out []OlderReporter
	end := c.now().In(kst)
	for page := 1; page <= maxListPages; page++ {
		q := url.Values{
			"corp_code": {code}, "pblntf_detail_ty": {"D001"},
			"bgn_de": {"20150101"}, "end_de": {end.Format("20060102")},
			"page_no": {strconv.Itoa(page)}, "page_count": {"100"},
		}
		var r disclosureResponse
		if err := c.getJSON(ctx, "list.json", q, &r); err != nil {
			if hub.AsError(err).Code == hub.CodeNotFound {
				break
			}
			return nil, err
		}
		for _, d := range r.List { // newest first
			key := nameKey(d.FlrNm)
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, ok := known[key]; ok {
				continue
			}
			out = append(out, OlderReporter{
				Reporter: strings.TrimSpace(d.FlrNm), Date: normalize.Date(d.RceptDt), ReportName: strings.TrimSpace(d.ReportNm),
				ReceiptNo: d.RceptNo, URL: viewer(d.RceptNo),
			})
		}
		if page*100 >= r.TotalCount {
			break
		}
	}
	return out, nil
}

func holderStatus(p *float64) string {
	switch {
	case p == nil:
		return ""
	case *p == 0:
		return "exited"
	case *p < 5:
		return "below_5_percent"
	}
	return "holding"
}

// nameKey compares reporter names ignoring spaces, case and corporate forms.
func nameKey(s string) string { return normalize.CompanyName(s) }

func pct(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
