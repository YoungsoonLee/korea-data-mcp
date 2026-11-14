package opendart

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// ---- opendart_get_investments ----
//
// 타법인출자 현황 (otrCprInvstmntSttus.json) lists a company's stakes in other
// companies from the annual and half-year reports. Quirks handled here:
//   - Some reports have no table, only a row of "-" and a "합계" row with
//     status 000: Samsung Electronics and SK hynix filed it as an attached
//     상세표 until 2020. q1 and q3 reports have no table at all.
//   - The last row is "합계", not an investee.
//   - A stake sold during the period has "-" at the end of the period, but
//     "-" also means zero or unknown elsewhere (a written-off book value).
//   - Preferred shares are a separate row, "스파크플러스(우선주)".
//   - Amounts are converted to KRW from the table's 천원/백만원, but a foreign
//     issuer can report in USD with only a footnote saying so (소마젠), and
//     the investee's financials can be in 백만엔 or 천RMB, noted in the
//     purpose cell: "일반투자\n(연결총자산,순이익\n단위:백만엔)".
//   - Purposes are free text: 경영참여, 경영참가, "경영\n참여", 단순투자, 일반투자, ...
//   - Names carry layout whitespace and notes: " 에스케이하이닉스㈜ ",
//     "NITTAN VALVE\n(해외법인\n일본상장)".
//   - Names differ from DART's list: 에스케이하이닉스 is SK하이닉스, SK플래닛
//     is 에스케이플래닛 (see normalize.CompanyKey).

type getInvestmentsInput struct {
	CorpCode string `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Year     int    `json:"year" jsonschema:"fiscal year (사업연도), 2015 or later"`
	Report   string `json:"report,omitempty" jsonschema:"annual (default, 사업보고서) or half (반기); quarterly reports have no investment table"`
	Purpose  string `json:"purpose,omitempty" jsonschema:"only stakes with this purpose: control (경영참여) or investment (단순투자)"`
	hub.PageParams
}

type investmentResponse struct {
	apiStatus
	List []struct {
		RceptNo        string `json:"rcept_no"`
		Name           string `json:"inv_prm"`
		FirstDate      string `json:"frst_acqs_de"`
		Purpose        string `json:"invstmnt_purps"`
		FirstAmount    string `json:"frst_acqs_amount"`
		StartShares    string `json:"bsis_blce_qy"`
		StartPercent   string `json:"bsis_blce_qota_rt"`
		StartBook      string `json:"bsis_blce_acntbk_amount"`
		ChangeShares   string `json:"incrs_dcrs_acqs_dsps_qy"`
		ChangeAmount   string `json:"incrs_dcrs_acqs_dsps_amount"`
		ChangeValue    string `json:"incrs_dcrs_evl_lstmn"`
		EndShares      string `json:"trmend_blce_qy"`
		EndPercent     string `json:"trmend_blce_qota_rt"`
		EndBook        string `json:"trmend_blce_acntbk_amount"`
		InvesteeAssets string `json:"recent_bsns_year_fnnr_sttus_tot_assets"`
		InvesteeIncome string `json:"recent_bsns_year_fnnr_sttus_thstrm_ntpf"`
		StlmDt         string `json:"stlm_dt"`
	} `json:"list"`
}

type Investments struct {
	CorpCode  string            `json:"corp_code"`
	Year      int               `json:"year"`
	Report    string            `json:"report"`
	AsOf      string            `json:"as_of,omitempty" jsonschema:"end of the period, YYYY-MM-DD"`
	ReceiptNo string            `json:"receipt_no,omitempty" jsonschema:"the filing the values come from; after a correction it is the latest corrected filing"`
	URL       string            `json:"url,omitempty"`
	Summary   InvestmentSummary `json:"summary"`
	Note      string            `json:"note,omitempty"`
	Items     []Investment      `json:"items" jsonschema:"stakes held at the end of the period, largest book value first, then stakes sold during it"`
	PageInfo  hub.PageInfo      `json:"page_info"`
}

type InvestmentSummary struct {
	Held      int    `json:"held" jsonschema:"stakes held at the end of the period"`
	Exited    int    `json:"exited" jsonschema:"stakes sold entirely during the period"`
	BookValue *int64 `json:"book_value,omitempty" jsonschema:"total book value at the end of the period, of the stakes matching purpose"`
	CorpCodes int    `json:"corp_codes_found" jsonschema:"investees matched to a DART corp_code"`
}

type Investment struct {
	Name      string `json:"name"`
	NameNote  string `json:"name_note,omitempty" jsonschema:"note filed with the name, e.g. 해외법인 일본상장"`
	CorpCode  string `json:"corp_code,omitempty" jsonschema:"the investee's DART corp_code, when its name matches exactly one company (or one listed company)"`
	StockCode string `json:"stock_code,omitempty"`
	Preferred bool   `json:"preferred,omitempty" jsonschema:"a stake in preferred shares, reported apart from common shares"`

	Purpose   string `json:"purpose" jsonschema:"control (경영참여, subsidiaries and affiliates), investment (단순투자) or other"`
	PurposeKo string `json:"purpose_ko,omitempty" jsonschema:"as filed"`

	FirstAcquired   string              `json:"first_acquired,omitempty" jsonschema:"YYYY-MM-DD"`
	FirstAmount     *int64              `json:"first_amount,omitempty"`
	Start           Stake               `json:"start" jsonschema:"at the start of the period"`
	End             Stake               `json:"end" jsonschema:"at the end of the period"`
	SharesChange    *int64              `json:"shares_change,omitempty" jsonschema:"shares acquired (positive) or disposed (negative)"`
	AmountChange    *int64              `json:"amount_change,omitempty" jsonschema:"book value acquired or disposed"`
	ValuationChange *int64              `json:"valuation_change,omitempty" jsonschema:"gain or loss on valuation (평가손익) and impairment"`
	Exited          bool                `json:"exited,omitempty" jsonschema:"sold entirely during the period"`
	Investee        *InvesteeFinancials `json:"investee,omitempty" jsonschema:"the investee's own latest fiscal year"`
}

type Stake struct {
	Shares    *int64   `json:"shares,omitempty"`
	Percent   *float64 `json:"percent,omitempty"`
	BookValue *int64   `json:"book_value,omitempty" jsonschema:"KRW unless a note says otherwise"`
}

type InvesteeFinancials struct {
	TotalAssets *int64 `json:"total_assets,omitempty"`
	NetIncome   *int64 `json:"net_income,omitempty"`
	UnitNote    string `json:"unit_note,omitempty" jsonschema:"set when the filer gives another unit or currency, e.g. 단위:백만엔; the amounts are then not KRW"`
}

const (
	attachedNote = "This report has no investment table in OpenDART's data; some companies, such as Samsung Electronics for 2015 to 2020, " +
		"filed it only as an attachment (타법인출자 현황(상세)). Open url to read it, or try a later year."
	foreignNote = "This is a foreign company listed in Korea: amounts may be in its reporting currency (소마젠 files USD) although OpenDART presents them as KRW. Check the filing at url."
)

var (
	trailingNote = regexp.MustCompile(`^(.+?)\s*\(([^()]*[가-힣][^()]*)\)$`)
	unitNote     = regexp.MustCompile(`\(([^()]*단위\s*:\s*[^()]*)\)`)
)

func (c *client) getInvestments(ctx context.Context, in getInvestmentsInput) (Investments, error) {
	var zero Investments
	page, size, err := in.Resolve(50, 200)
	if err != nil {
		return zero, err
	}
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return zero, err
	}
	if in.Report == "q1" || in.Report == "q3" {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "quarterly reports have no investment table; use report annual or half")
	}
	report, reprt, err := c.reportParams(in.Year, in.Report)
	if err != nil {
		return zero, err
	}
	if in.Purpose != "" && in.Purpose != "control" && in.Purpose != "investment" {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown purpose %q; use control or investment", in.Purpose)
	}

	var r investmentResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}}
	if err := c.getJSON(ctx, "otrCprInvstmntSttus.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Hint = notFiledHint
		}
		return zero, err
	}

	out := Investments{CorpCode: code, Year: in.Year, Report: report}
	var held, exited []Investment
	var book int64
	for _, row := range r.List {
		out.ReceiptNo = cmp.Or(out.ReceiptNo, row.RceptNo)
		out.AsOf = cmp.Or(out.AsOf, normalize.Date(row.StlmDt))
		raw := strings.Join(strings.Fields(row.Name), " ")
		if raw == "" || raw == "-" || raw == "합계" || raw == "계" {
			continue
		}
		inv := Investment{
			FirstAcquired:   normalize.Date(row.FirstDate),
			FirstAmount:     normalize.Int(row.FirstAmount),
			Start:           Stake{Shares: normalize.Int(row.StartShares), Percent: decimal(row.StartPercent), BookValue: normalize.Int(row.StartBook)},
			End:             Stake{Shares: normalize.Int(row.EndShares), Percent: decimal(row.EndPercent), BookValue: normalize.Int(row.EndBook)},
			SharesChange:    normalize.Int(row.ChangeShares),
			AmountChange:    normalize.Int(row.ChangeAmount),
			ValuationChange: normalize.Int(row.ChangeValue),
		}
		inv.Name, inv.NameNote, inv.Preferred = investeeName(raw)
		purpose := strings.Join(strings.Fields(row.Purpose), " ")
		var unit string
		if m := unitNote.FindStringSubmatch(purpose); m != nil {
			unit = strings.Join(strings.Fields(m[1]), " ")
			purpose = strings.TrimSpace(strings.Replace(purpose, m[0], "", 1))
		}
		inv.Purpose, inv.PurposeKo = investmentPurpose(purpose), strings.Trim(purpose, "-")
		if in.Purpose != "" && inv.Purpose != in.Purpose {
			continue
		}
		if f := (InvesteeFinancials{TotalAssets: normalize.Int(row.InvesteeAssets), NetIncome: normalize.Int(row.InvesteeIncome), UnitNote: unit}); f != (InvesteeFinancials{}) {
			inv.Investee = &f
		}
		inv.Exited = zeroOrNil(inv.End.Shares) && (inv.End.Percent == nil || *inv.End.Percent == 0) && zeroOrNil(inv.End.BookValue) &&
			inv.SharesChange != nil && *inv.SharesChange < 0
		if corp, err := c.byName(ctx, inv.Name); err == nil && corp != nil && corp.Code != code {
			inv.CorpCode, inv.StockCode = corp.Code, corp.StockCode
			out.Summary.CorpCodes++
		}
		if inv.Exited {
			exited = append(exited, inv)
			continue
		}
		if inv.End.BookValue != nil {
			book += *inv.End.BookValue
			out.Summary.BookValue = &book
		}
		held = append(held, inv)
	}
	slices.SortStableFunc(held, func(a, b Investment) int {
		return cmp.Compare(value(b.End.BookValue), value(a.End.BookValue))
	})
	out.Summary.Held, out.Summary.Exited = len(held), len(exited)
	items := append(held, exited...)
	out.URL = viewer(out.ReceiptNo)

	switch corp, _ := c.corps.byCorpCode(ctx, code); {
	case len(items) == 0 && out.ReceiptNo != "" && in.Purpose == "":
		out.Note = attachedNote
	case corp != nil && strings.HasPrefix(corp.StockCode, "9"):
		out.Note = foreignNote
	}
	out.PageInfo = hub.NewPageInfo(page, size, len(items))
	out.Items = pageOf(items, page, size)
	if out.Items == nil {
		out.Items = []Investment{}
	}
	return out, nil
}

// investeeName collapses layout whitespace, takes off a preferred-share
// marker and splits a trailing Korean note: "NITTAN VALVE\n(해외법인\n일본상장)"
// is NITTAN VALVE with the note 해외법인 일본상장. A corporate form such as
// (주) stays part of the name.
func investeeName(s string) (name, note string, preferred bool) {
	for _, mark := range []string{"(우선주)", "우선주"} {
		if strings.HasSuffix(s, mark) {
			s, preferred = strings.TrimSpace(strings.TrimSuffix(s, mark)), true
			break
		}
	}
	if m := trailingNote.FindStringSubmatch(s); m != nil && normalize.StripCompanyForm("("+m[2]+")") != "" {
		return m[1], m[2], preferred
	}
	return s, "", preferred
}

// investmentPurpose sorts the free-text purpose into control, investment or
// other.
func investmentPurpose(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	switch {
	case strings.Contains(s, "단순") || strings.Contains(s, "일반투자"):
		return "investment"
	case strings.Contains(s, "경영") || strings.Contains(s, "계열") || strings.Contains(s, "설립") || strings.Contains(s, "합작"):
		return "control"
	case strings.Contains(s, "투자"):
		return "investment"
	}
	return "other"
}

func value(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}
