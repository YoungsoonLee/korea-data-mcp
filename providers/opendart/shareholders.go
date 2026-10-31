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

// ---- opendart_get_major_shareholders ----
//
// 최대주주 현황 (hyslrSttus.json) is the table from the periodic report, one
// row per holder and share class. Quirks handled here:
//   - Each holder has a row per share class, and a group total row "계".
//   - Relations are free text: the largest shareholder is "본인", "최대주주",
//     "최대주주 본인", "본인(최대주주)" or "본인\n(대표이사)".
//   - A row can say only `"` (ditto) for the name and relation of the row
//     above, sometimes for a second row of the same share class.
//   - Names and labels carry layout whitespace: "김   식", "삼성생명보험㈜\n(특별계정)".
//   - Share classes are 보통주/우선주 or 의결권있는/없는주식; "합계" and "-"
//     rows repeat totals.

type getMajorShareholdersInput struct {
	CorpCode string `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Year     int    `json:"year" jsonschema:"fiscal year (사업연도), 2015 or later"`
	Report   string `json:"report,omitempty" jsonschema:"one of: annual (default, 사업보고서), half (반기), q1 (1분기), q3 (3분기)"`
}

type shareholdersResponse struct {
	apiStatus
	List []struct {
		RceptNo  string `json:"rcept_no"`
		Nm       string `json:"nm"`
		Relate   string `json:"relate"`
		StockKnd string `json:"stock_knd"`
		BsisCo   string `json:"bsis_posesn_stock_co"`
		BsisRt   string `json:"bsis_posesn_stock_qota_rt"`
		TrmendCo string `json:"trmend_posesn_stock_co"`
		TrmendRt string `json:"trmend_posesn_stock_qota_rt"`
		Rm       string `json:"rm"`
		StlmDt   string `json:"stlm_dt"`
	} `json:"list"`
}

type MajorShareholders struct {
	CorpCode           string        `json:"corp_code"`
	Year               int           `json:"year"`
	Report             string        `json:"report"`
	AsOf               string        `json:"as_of" jsonschema:"end of the period, YYYY-MM-DD"`
	ReceiptNo          string        `json:"receipt_no"`
	URL                string        `json:"url"`
	LargestShareholder string        `json:"largest_shareholder,omitempty" jsonschema:"최대주주"`
	Total              ShareHoldings `json:"total" jsonschema:"the largest shareholder and related parties together"`
	Holders            []Shareholder `json:"holders" jsonschema:"largest common stake first"`
}

type Shareholder struct {
	Name     string `json:"name"`
	Relation string `json:"relation" jsonschema:"as reported, e.g. 본인, 특수관계인, 계열회사 임원"`
	Largest  bool   `json:"largest,omitempty" jsonschema:"the largest shareholder (최대주주) itself"`
	ShareHoldings
	Note string `json:"note,omitempty" jsonschema:"reason for a change during the period, e.g. 장내 매매"`
}

type ShareHoldings struct {
	Common    *Holding `json:"common,omitempty"`
	Preferred *Holding `json:"preferred,omitempty"`
}

type Holding struct {
	Shares         *int64   `json:"shares"`
	Percent        *float64 `json:"percent" jsonschema:"stake in that share class, percent"`
	SharesAtStart  *int64   `json:"shares_at_start,omitempty"`
	PercentAtStart *float64 `json:"percent_at_start,omitempty"`
}

var hangulOnly = regexp.MustCompile(`^[가-힣]{2,5}$`)

func (c *client) getMajorShareholders(ctx context.Context, in getMajorShareholdersInput) (MajorShareholders, error) {
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return MajorShareholders{}, err
	}
	report, reprt, err := c.reportParams(in.Year, in.Report)
	if err != nil {
		return MajorShareholders{}, err
	}
	var r shareholdersResponse
	q := url.Values{"corp_code": {code}, "bsns_year": {strconv.Itoa(in.Year)}, "reprt_code": {reprt}}
	if err := c.getJSON(ctx, "hyslrSttus.json", q, &r); err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound {
			he.Hint = notFiledHint
		}
		return MajorShareholders{}, err
	}

	out := MajorShareholders{CorpCode: code, Year: in.Year, Report: report, Holders: []Shareholder{}}
	index := map[string]int{}
	var prevName, prevRelation string
	for _, row := range r.List {
		out.ReceiptNo = row.RceptNo
		out.AsOf = cmp.Or(out.AsOf, normalize.Date(row.StlmDt))
		name, relation := holderName(row.Nm), strings.Join(strings.Fields(row.Relate), " ")
		if name == `"` {
			name = prevName
		}
		if relation == `"` {
			relation = prevRelation
		}
		prevName, prevRelation = name, relation
		class := shareClass(row.StockKnd)
		h := &Holding{
			Shares:         normalize.Int(row.TrmendCo),
			Percent:        decimal(row.TrmendRt),
			SharesAtStart:  normalize.Int(row.BsisCo),
			PercentAtStart: decimal(row.BsisRt),
		}
		if name == "계" || name == "합계" {
			switch {
			case h.Shares == nil:
			case class == "preferred":
				out.Total.Preferred = h
			case class == "common" || out.Total.Common == nil:
				out.Total.Common = h
			}
			continue
		}
		if name == "" || class == "" {
			continue
		}
		i, ok := index[name]
		if !ok {
			i = len(out.Holders)
			index[name] = i
			out.Holders = append(out.Holders, Shareholder{Name: name, Relation: relation, Largest: largestRelation(relation)})
		}
		s := &out.Holders[i]
		if class == "preferred" {
			s.Preferred = addHolding(s.Preferred, h)
		} else {
			s.Common = addHolding(s.Common, h)
		}
		if note := strings.Join(strings.Fields(row.Rm), " "); note != "-" && note != "" {
			s.Note = note
		}
		if s.Largest && out.LargestShareholder == "" {
			out.LargestShareholder = name
		}
	}
	slices.SortStableFunc(out.Holders, func(a, b Shareholder) int {
		return cmp.Or(cmp.Compare(shares(b.Common), shares(a.Common)), cmp.Compare(shares(b.Preferred), shares(a.Preferred)))
	})
	out.URL = viewer(out.ReceiptNo)
	return out, nil
}

// holderName collapses layout whitespace; a short Korean personal name loses
// its inner spaces ("김   식" is 김식).
func holderName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if joined := strings.ReplaceAll(s, " ", ""); hangulOnly.MatchString(joined) {
		return joined
	}
	return s
}

// largestRelation reports whether a free-text relation names the largest
// shareholder itself rather than a related party.
func largestRelation(rel string) bool {
	rel = strings.ReplaceAll(rel, " ", "")
	return strings.HasPrefix(rel, "본인") || rel == "최대주주" || rel == "최대주주본인"
}

// addHolding adds a second row of the same holder and class, which some
// filers write as a ditto row.
func addHolding(a, b *Holding) *Holding {
	if a == nil {
		return b
	}
	sum := func(x, y *int64) *int64 {
		if x == nil || y == nil {
			return cmp.Or(x, y)
		}
		n := *x + *y
		return &n
	}
	fsum := func(x, y *float64) *float64 {
		if x == nil || y == nil {
			return cmp.Or(x, y)
		}
		n := *x + *y
		return &n
	}
	return &Holding{
		Shares:         sum(a.Shares, b.Shares),
		Percent:        fsum(a.Percent, b.Percent),
		SharesAtStart:  sum(a.SharesAtStart, b.SharesAtStart),
		PercentAtStart: fsum(a.PercentAtStart, b.PercentAtStart),
	}
}

func shares(h *Holding) int64 {
	if h == nil || h.Shares == nil {
		return 0
	}
	return *h.Shares
}
