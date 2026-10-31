package opendart

import (
	"cmp"
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// ---- opendart_get_dividends ----
//
// 배당에 관한 사항 (alotMatter.json) is a table of labeled rows with three
// columns. Quirks handled here:
//   - The unit is part of the label: "현금배당금총액(백만원)", "주당 현금배당금(원)",
//     "현금배당수익률(%)". Amounts are converted to KRW.
//   - In half and quarterly reports thstrm is year-to-date, but frmtrm and
//     lwfr are the two prior full fiscal years, not the same months: Samsung's
//     2025 half-year report puts ₩732 next to ₩1,446 for all of 2024.
//   - Per-share amounts change with stock splits: Samsung paid ₩21,000 a
//     share for 2015 at a par value of ₩5,000, before its 50:1 split.
//   - Labels vary over time ("주당순이익(원)" without "(연결)" in 2015), "-"
//     means none, and share classes are 보통주/우선주, 의결권있는/없는주식 or "-".

type getDividendsInput struct {
	CorpCode string `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Year     int    `json:"year" jsonschema:"fiscal year (사업연도), 2015 or later"`
	Report   string `json:"report,omitempty" jsonschema:"one of: annual (default, 사업보고서), half (반기), q1 (1분기), q3 (3분기)"`
}

type dividendsResponse struct {
	apiStatus
	List []struct {
		RceptNo  string `json:"rcept_no"`
		Se       string `json:"se"`
		StockKnd string `json:"stock_knd"`
		Thstrm   string `json:"thstrm"`
		Frmtrm   string `json:"frmtrm"`
		Lwfr     string `json:"lwfr"`
		StlmDt   string `json:"stlm_dt"`
	} `json:"list"`
}

type Dividends struct {
	CorpCode  string           `json:"corp_code"`
	Year      int              `json:"year"`
	Report    string           `json:"report"`
	ReceiptNo string           `json:"receipt_no"`
	URL       string           `json:"url"`
	Periods   []DividendPeriod `json:"periods" jsonschema:"the report's period first, then the two prior fiscal years"`
}

type DividendPeriod struct {
	FiscalYear            int            `json:"fiscal_year"`
	FullYear              bool           `json:"full_year" jsonschema:"false for the year-to-date period of a half or quarterly report"`
	Through               string         `json:"through,omitempty" jsonschema:"last day covered, YYYY-MM-DD; for the report's own period only"`
	ParValue              *int64         `json:"par_value,omitempty" jsonschema:"par value per share (액면가), KRW; per-share amounts compare across years only when it is the same"`
	NetIncomeConsolidated *int64         `json:"net_income_consolidated,omitempty" jsonschema:"KRW"`
	NetIncomeSeparate     *int64         `json:"net_income_separate,omitempty" jsonschema:"KRW"`
	EPS                   *int64         `json:"eps,omitempty" jsonschema:"consolidated earnings per share, KRW"`
	CashDividendTotal     *int64         `json:"cash_dividend_total,omitempty" jsonschema:"KRW"`
	StockDividendTotal    *int64         `json:"stock_dividend_total,omitempty" jsonschema:"KRW"`
	PayoutRatio           *float64       `json:"payout_ratio,omitempty" jsonschema:"consolidated cash dividend payout ratio, percent"`
	Common                ShareDividend  `json:"common"`
	Preferred             *ShareDividend `json:"preferred,omitempty"`
}

type ShareDividend struct {
	CashPerShare  *int64   `json:"cash_per_share,omitempty" jsonschema:"KRW"`
	CashYield     *float64 `json:"cash_yield,omitempty" jsonschema:"percent"`
	StockPerShare *float64 `json:"stock_per_share,omitempty" jsonschema:"shares"`
	StockYield    *float64 `json:"stock_yield,omitempty" jsonschema:"percent"`
}

func (c *client) getDividends(ctx context.Context, in getDividendsInput) (Dividends, error) {
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return Dividends{}, err
	}
	report, reprt, err := c.reportParams(in.Year, in.Report)
	if err != nil {
		return Dividends{}, err
	}
	var r dividendsResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}}
	if err := c.getJSON(ctx, "alotMatter.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Hint = notFiledHint
		}
		return Dividends{}, err
	}

	out := Dividends{CorpCode: code, Year: in.Year, Report: report}
	periods := []DividendPeriod{
		{FiscalYear: in.Year, FullYear: report == "annual"},
		{FiscalYear: in.Year - 1, FullYear: true},
		{FiscalYear: in.Year - 2, FullYear: true},
	}
	var preferred [3]ShareDividend
	for _, row := range r.List {
		out.ReceiptNo = row.RceptNo
		periods[0].Through = cmp.Or(periods[0].Through, normalize.Date(row.StlmDt))
		label := strings.Join(strings.Fields(row.Se), "")
		class := shareClass(row.StockKnd)
		for i, v := range []string{row.Thstrm, row.Frmtrm, row.Lwfr} {
			p := &periods[i]
			sd := &p.Common
			if class == "preferred" {
				sd = &preferred[i]
			}
			switch {
			case strings.Contains(label, "액면가"):
				p.ParValue = amount(v, label)
			case strings.Contains(label, "당기순이익"):
				if strings.Contains(label, "별도") || strings.Contains(label, "개별") {
					p.NetIncomeSeparate = amount(v, label)
				} else {
					p.NetIncomeConsolidated = amount(v, label)
				}
			case strings.Contains(label, "주당순이익"):
				p.EPS = amount(v, label)
			case strings.Contains(label, "현금배당금총액"):
				p.CashDividendTotal = amount(v, label)
			case strings.Contains(label, "주식배당금총액"):
				p.StockDividendTotal = amount(v, label)
			case strings.Contains(label, "현금배당성향"):
				p.PayoutRatio = decimal(v)
			case strings.Contains(label, "현금배당수익률"):
				setClass(&sd.CashYield, decimal(v), class)
			case strings.Contains(label, "주식배당수익률"):
				setClass(&sd.StockYield, decimal(v), class)
			case strings.Contains(label, "주당현금배당금"):
				setClass(&sd.CashPerShare, amount(v, label), class)
			case strings.Contains(label, "주당주식배당"):
				setClass(&sd.StockPerShare, decimal(v), class)
			}
		}
	}
	for i := range periods {
		if p := preferred[i]; p != (ShareDividend{}) {
			periods[i].Preferred = &p
		}
	}
	out.Periods = periods
	out.URL = viewer(out.ReceiptNo)
	return out, nil
}

// shareClass maps DART's share kinds to common or preferred. "-" is used for
// company-wide rows and, by some companies, for common shares.
func shareClass(s string) string {
	switch strings.Join(strings.Fields(s), "") {
	case "우선주", "의결권없는주식":
		return "preferred"
	case "보통주", "의결권있는주식":
		return "common"
	}
	return ""
}

// amount parses an amount whose unit is given at the end of its label, e.g.
// "현금배당금총액(백만원)", and returns it in KRW.
func amount(v, label string) *int64 {
	n := normalize.Int(v)
	if n == nil {
		return nil
	}
	switch {
	case strings.HasSuffix(label, "(백만원)"):
		*n *= 1_000_000
	case strings.HasSuffix(label, "(천원)"):
		*n *= 1_000
	}
	return n
}

// decimal parses "29.20" or "1,234.5"; "-" and blanks are nil.
func decimal(v string) *float64 {
	f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", ""), 64)
	if err != nil {
		return nil
	}
	return &f
}

// setClass stores a per-share value. A row without a share class only fills
// in for common shares; a 보통주 row always wins.
func setClass[T any](dst **T, v *T, class string) {
	if class != "" || *dst == nil {
		*dst = v
	}
}
