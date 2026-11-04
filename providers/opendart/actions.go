package opendart

import (
	"cmp"
	"context"
	"maps"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// ---- opendart_list_corporate_actions ----
//
// 주요사항보고서 decisions (DS005) come from one endpoint per kind, each with
// its own fields. Quirks handled here:
//   - bgn_de/end_de select by the date a decision was first filed, but the
//     row carries the receipt number of its latest correction and no date:
//     광진실업's rights offering filed 2026-07-02 comes back as 20261029000002
//     and only for a July range.
//   - Rows are not sorted.
//   - Buyback amounts are split into common and preferred shares: Samsung's
//     ₩3 trillion buyback of 2024-11 is ₩2.68 trillion in the common field.
//   - Buybacks through a trust (자기주식취득신탁계약) are separate reports:
//     LG Chem's ₩300 billion trust has no 자기주식취득결정.
//   - Rights offerings have no issue price, no allottees and no board date;
//     the price is the proceeds divided by the new shares.
//   - Convertible bonds may reset their conversion price down to a floor
//     (리픽싱): 파이온엑스's ₩557 conversion price can fall to ₩21.
//   - Dates are "2026년 01월 30일", numbers "1,234", rates "100" or "100.0",
//     and "-" means none.

type actionKind struct {
	endpoint  string
	name      string // Korean report name
	boardDate string // field of the board resolution date
}

var actionKinds = map[string]actionKind{
	"rights_offering":    {"piicDecsn.json", "유상증자결정", ""},
	"bonus_issue":        {"fricDecsn.json", "무상증자결정", "bddd"},
	"capital_reduction":  {"crDecsn.json", "감자결정", "bddd"},
	"convertible_bond":   {"cvbdIsDecsn.json", "전환사채권발행결정", "bddd"},
	"bond_with_warrants": {"bdwtIsDecsn.json", "신주인수권부사채권발행결정", "bddd"},
	"exchangeable_bond":  {"exbdIsDecsn.json", "교환사채권발행결정", "bddd"},
	"buyback":            {"tsstkAqDecsn.json", "자기주식취득결정", "aq_dd"},
	"buyback_trust":      {"tsstkAqTrctrCnsDecsn.json", "자기주식취득신탁계약체결결정", "bddd"},
	"buyback_trust_end":  {"tsstkAqTrctrCcDecsn.json", "자기주식취득신탁계약해지결정", "bddd"},
	"treasury_disposal":  {"tsstkDpDecsn.json", "자기주식처분결정", "dp_dd"},
	"merger":             {"cmpMgDecsn.json", "회사합병결정", "bddd"},
	"split":              {"cmpDvDecsn.json", "회사분할결정", "bddd"},
}

type listCorporateActionsInput struct {
	CorpCode  string   `json:"corp_code" jsonschema:"8-digit corp_code or 6-character stock code"`
	Types     []string `json:"types,omitempty" jsonschema:"any of: rights_offering (유상증자), bonus_issue (무상증자), capital_reduction (감자), convertible_bond (CB), bond_with_warrants (BW), exchangeable_bond (EB), buyback (자기주식 취득), buyback_trust (자기주식 취득 신탁계약 체결), buyback_trust_end (신탁계약 해지), treasury_disposal (자기주식 처분), merger (합병), split (분할); default all"`
	StartDate string   `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD; decisions first filed from this date (default: 3 years before end_date)"`
	EndDate   string   `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD; decisions first filed until this date (default: today in KST)"`
	hub.PageParams
}

type actionsResponse struct {
	apiStatus
	List []map[string]string `json:"list"`
}

type CorporateActions struct {
	CorpCode  string            `json:"corp_code"`
	CorpName  string            `json:"corp_name,omitempty"`
	StartDate string            `json:"start_date"`
	EndDate   string            `json:"end_date" jsonschema:"start_date and end_date match the date each decision was first filed, not filed_date"`
	Items     []CorporateAction `json:"items" jsonschema:"latest filing first"`
	PageInfo  hub.PageInfo      `json:"page_info"`
}

type CorporateAction struct {
	Type      string `json:"type"`
	Name      string `json:"name" jsonschema:"Korean report name"`
	ReceiptNo string `json:"receipt_no" jsonschema:"the latest filing of this decision (접수번호); after a correction (기재정정) it is the latest corrected filing"`
	FiledDate string `json:"filed_date" jsonschema:"date of receipt_no; for a corrected decision this can be months after it was first filed"`
	BoardDate string `json:"board_date,omitempty" jsonschema:"board resolution date (이사회결의일); rights offerings do not report it"`
	URL       string `json:"url"`

	RightsOffering   *RightsOffering   `json:"rights_offering,omitempty"`
	BonusIssue       *BonusIssue       `json:"bonus_issue,omitempty"`
	CapitalReduction *CapitalReduction `json:"capital_reduction,omitempty"`
	Bond             *Bond             `json:"bond,omitempty" jsonschema:"convertible_bond, bond_with_warrants and exchangeable_bond"`
	Buyback          *Buyback          `json:"buyback,omitempty" jsonschema:"buyback and treasury_disposal"`
	Trust            *BuybackTrust     `json:"trust,omitempty" jsonschema:"buyback_trust and buyback_trust_end"`
	Merger           *Merger           `json:"merger,omitempty"`
	Split            *Split            `json:"split,omitempty"`
}

// ShareCounts is a number of shares, or KRW per share, by share class.
type ShareCounts struct {
	Common    *int64 `json:"common,omitempty"`
	Preferred *int64 `json:"preferred,omitempty" jsonschema:"preferred and other classes (종류주식)"`
}

// Amounts is a KRW amount by share class with the total.
type Amounts struct {
	Common    *int64 `json:"common,omitempty"`
	Preferred *int64 `json:"preferred,omitempty"`
	Total     *int64 `json:"total,omitempty" jsonschema:"common and preferred together"`
}

// Proceeds is what raised money is for (자금조달의 목적), KRW.
type Proceeds struct {
	Total               *int64 `json:"total,omitempty"`
	Facilities          *int64 `json:"facilities,omitempty" jsonschema:"시설자금"`
	BusinessAcquisition *int64 `json:"business_acquisition,omitempty" jsonschema:"영업양수자금"`
	Operating           *int64 `json:"operating,omitempty" jsonschema:"운영자금"`
	DebtRepayment       *int64 `json:"debt_repayment,omitempty" jsonschema:"채무상환자금"`
	SecuritiesPurchase  *int64 `json:"securities_purchase,omitempty" jsonschema:"타법인 증권 취득자금"`
	Other               *int64 `json:"other,omitempty" jsonschema:"기타자금"`
}

type RightsOffering struct {
	Method           string      `json:"method,omitempty" jsonschema:"증자방식, e.g. 제3자배정증자, 주주배정증자, 일반공모증자"`
	NewShares        ShareCounts `json:"new_shares"`
	SharesBefore     ShareCounts `json:"shares_before"`
	NewSharesPercent *float64    `json:"new_shares_percent,omitempty" jsonschema:"new common shares as a percent of common shares before the issue"`
	ParValue         *int64      `json:"par_value,omitempty" jsonschema:"KRW per share"`
	Proceeds         Proceeds    `json:"proceeds"`
	ImpliedPrice     *int64      `json:"implied_price,omitempty" jsonschema:"proceeds divided by new shares, KRW; the filing does not report the issue price. Only when common shares alone are issued"`
}

type BonusIssue struct {
	NewShares      ShareCounts `json:"new_shares"`
	SharesBefore   ShareCounts `json:"shares_before"`
	SharesPerShare *float64    `json:"shares_per_share,omitempty" jsonschema:"new common shares per existing share"`
	RecordDate     string      `json:"record_date,omitempty" jsonschema:"신주배정기준일"`
	ListingDate    string      `json:"listing_date,omitempty"`
}

type CapitalReduction struct {
	SharesReduced ShareCounts `json:"shares_reduced"`
	SharesBefore  ShareCounts `json:"shares_before"`
	SharesAfter   ShareCounts `json:"shares_after"`
	CapitalBefore *int64      `json:"capital_before,omitempty" jsonschema:"KRW"`
	CapitalAfter  *int64      `json:"capital_after,omitempty" jsonschema:"KRW"`
	Percent       *float64    `json:"percent,omitempty" jsonschema:"reduction of common shares, percent"`
	RecordDate    string      `json:"record_date,omitempty" jsonschema:"감자기준일"`
	ListingDate   string      `json:"listing_date,omitempty" jsonschema:"new shares listed"`
	Method        string      `json:"method,omitempty"`
	Reason        string      `json:"reason,omitempty"`
}

type Bond struct {
	Series          string   `json:"series,omitempty" jsonschema:"회차"`
	Description     string   `json:"description,omitempty" jsonschema:"e.g. 무기명식 무보증 사모 전환사채"`
	Offering        string   `json:"offering,omitempty" jsonschema:"사모 (private) or 공모 (public)"`
	FaceAmount      *int64   `json:"face_amount,omitempty" jsonschema:"KRW"`
	CouponRate      *float64 `json:"coupon_rate,omitempty" jsonschema:"percent"`
	YieldToMaturity *float64 `json:"yield_to_maturity,omitempty" jsonschema:"percent"`
	Maturity        string   `json:"maturity,omitempty"`
	PaymentDate     string   `json:"payment_date,omitempty"`
	Underlying      string   `json:"underlying,omitempty" jsonschema:"shares received; an exchangeable bond can be for another company's shares or the issuer's treasury shares (자기주식)"`
	Price           *int64   `json:"price,omitempty" jsonschema:"conversion, exercise or exchange price, KRW per share"`
	FloorPrice      *int64   `json:"floor_price,omitempty" jsonschema:"lowest price after downward resets (리픽싱), KRW per share"`
	Shares          *int64   `json:"shares,omitempty" jsonschema:"shares at price"`
	SharesPercent   *float64 `json:"shares_percent,omitempty" jsonschema:"shares as a percent of shares outstanding, as reported"`
	SharesAtFloor   *int64   `json:"shares_at_floor,omitempty" jsonschema:"shares if the price is reset to floor_price"`
	ExerciseStart   string   `json:"exercise_start,omitempty"`
	ExerciseEnd     string   `json:"exercise_end,omitempty"`
	Detachable      string   `json:"detachable,omitempty" jsonschema:"bond_with_warrants: 분리 (warrants trade separately) or 비분리"`
	Proceeds        Proceeds `json:"proceeds"`
}

type Buyback struct {
	Shares        ShareCounts `json:"shares" jsonschema:"planned"`
	PricePerShare ShareCounts `json:"price_per_share,omitzero" jsonschema:"treasury_disposal: KRW"`
	Amount        Amounts     `json:"amount" jsonschema:"planned, KRW"`
	Start         string      `json:"start,omitempty"`
	End           string      `json:"end,omitempty"`
	Purpose       string      `json:"purpose,omitempty" jsonschema:"e.g. 주주가치 제고, 임직원 주식보상; shares are cancelled only when it says so (소각)"`
	Method        string      `json:"method,omitempty"`
	Brokers       string      `json:"brokers,omitempty"`
	HeldBefore    ShareCounts `json:"held_before" jsonschema:"treasury shares held before the decision"`
}

type BuybackTrust struct {
	Amount      *int64      `json:"amount,omitempty" jsonschema:"contract amount, KRW; for buyback_trust_end the amount before termination"`
	AmountAfter *int64      `json:"amount_after,omitempty" jsonschema:"buyback_trust_end: KRW after termination"`
	Start       string      `json:"start,omitempty"`
	End         string      `json:"end,omitempty"`
	Date        string      `json:"date,omitempty" jsonschema:"contract or termination date"`
	Purpose     string      `json:"purpose,omitempty" jsonschema:"for buyback_trust_end the reason"`
	Trustee     string      `json:"trustee,omitempty"`
	HeldBefore  ShareCounts `json:"held_before"`
}

type Merger struct {
	Counterparty        string      `json:"counterparty,omitempty"`
	Relation            string      `json:"relation,omitempty" jsonschema:"counterparty's relation, e.g. 자회사"`
	Form                string      `json:"form,omitempty" jsonschema:"e.g. 소규모합병, 간이합병"`
	Method              string      `json:"method,omitempty"`
	Ratio               string      `json:"ratio,omitempty" jsonschema:"as reported, e.g. 샘표식품㈜ : 샘표㈜ = 1 : 2.0881235"`
	NewShares           ShareCounts `json:"new_shares"`
	AppraisalPrice      *int64      `json:"appraisal_price,omitempty" jsonschema:"주식매수청구가격, KRW per share"`
	ShareholdersMeeting string      `json:"shareholders_meeting,omitempty"`
	Date                string      `json:"date,omitempty" jsonschema:"merger date"`
	Purpose             string      `json:"purpose,omitempty"`
}

type Split struct {
	Kind                string `json:"kind,omitempty" jsonschema:"물적분할 (the company keeps the new company's shares) or 인적분할 (shareholders get them), when the filing says"`
	NewCompany          string `json:"new_company,omitempty"`
	NewCompanyBusiness  string `json:"new_company_business,omitempty"`
	Method              string `json:"method,omitempty"`
	Ratio               string `json:"ratio,omitempty"`
	ShareholdersMeeting string `json:"shareholders_meeting,omitempty"`
	Date                string `json:"date,omitempty" jsonschema:"split date"`
}

func (c *client) listCorporateActions(ctx context.Context, in listCorporateActionsInput) (CorporateActions, error) {
	var zero CorporateActions
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return zero, err
	}
	code, err := c.resolveCorpCode(ctx, in.CorpCode)
	if err != nil {
		return zero, err
	}
	end := c.now().In(kst)
	if in.EndDate != "" {
		if end, err = parseDate("end_date", in.EndDate); err != nil {
			return zero, err
		}
	}
	start := end.AddDate(-3, 0, 0)
	if in.StartDate != "" {
		if start, err = parseDate("start_date", in.StartDate); err != nil {
			return zero, err
		}
	}
	if start.After(end) {
		return zero, hub.Errorf(hub.CodeInvalidArgument, "start_date %s is after end_date %s", start.Format(time.DateOnly), end.Format(time.DateOnly))
	}
	types := slices.Sorted(maps.Keys(actionKinds))
	if len(in.Types) > 0 {
		types = nil
		for _, t := range in.Types {
			t = strings.ToLower(strings.TrimSpace(t))
			if _, ok := actionKinds[t]; !ok {
				return zero, hub.Errorf(hub.CodeInvalidArgument, "unknown type %q; use any of %s", t, actionTypeList())
			}
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
	}

	q := url.Values{"corp_code": {code}, "bgn_de": {start.Format("20060102")}, "end_de": {end.Format("20060102")}}
	results := make([][]map[string]string, len(types))
	errs := make([]error, len(types))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, t := range types {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			var r actionsResponse
			if err := c.getJSON(ctx, actionKinds[t].endpoint, maps.Clone(q), &r); err != nil {
				if hub.AsError(err).Code != hub.CodeNotFound {
					errs[i] = err
				}
				return
			}
			results[i] = r.List
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return zero, err
		}
	}

	out := CorporateActions{CorpCode: code, StartDate: start.Format(time.DateOnly), EndDate: end.Format(time.DateOnly), Items: []CorporateAction{}}
	var all []CorporateAction
	for i, t := range types {
		for _, row := range results[i] {
			out.CorpName = cmp.Or(out.CorpName, row["corp_name"])
			all = append(all, buildAction(t, row))
		}
	}
	slices.SortStableFunc(all, func(a, b CorporateAction) int {
		return cmp.Or(cmp.Compare(b.ReceiptNo, a.ReceiptNo), cmp.Compare(a.Type, b.Type))
	})
	out.Items = append(out.Items, pageOf(all, page, size)...)
	out.PageInfo = hub.NewPageInfo(page, size, len(all))
	return out, nil
}

func actionTypeList() string {
	return strings.Join(slices.Sorted(maps.Keys(actionKinds)), ", ")
}

// row reads the fields of one decision.
type row map[string]string

func (r row) text(k string) string {
	s := normalize.Text(r[k])
	if s == "-" {
		return ""
	}
	const max = 300
	if rs := []rune(s); len(rs) > max {
		return string(rs[:max]) + "…"
	}
	return s
}
func (r row) int(k string) *int64     { return normalize.Int(r[k]) }
func (r row) float(k string) *float64 { return decimal(r[k]) }
func (r row) date(k string) string    { return normalize.Date(r[k]) }
func (r row) shares(common, preferred string) ShareCounts {
	return ShareCounts{Common: r.int(common), Preferred: r.int(preferred)}
}

func buildAction(t string, m map[string]string) CorporateAction {
	r := row(m)
	k := actionKinds[t]
	a := CorporateAction{Type: t, Name: k.name, ReceiptNo: r["rcept_no"], URL: viewer(r["rcept_no"])}
	if len(a.ReceiptNo) >= 8 {
		a.FiledDate = normalize.Date(a.ReceiptNo[:8])
	}
	if k.boardDate != "" {
		a.BoardDate = r.date(k.boardDate)
	}
	switch t {
	case "rights_offering":
		ro := &RightsOffering{
			Method:       r.text("ic_mthn"),
			NewShares:    r.shares("nstk_ostk_cnt", "nstk_estk_cnt"),
			SharesBefore: r.shares("bfic_tisstk_ostk", "bfic_tisstk_estk"),
			ParValue:     r.int("fv_ps"),
			Proceeds:     r.proceeds(),
		}
		ro.NewSharesPercent = percent(ro.NewShares.Common, ro.SharesBefore.Common)
		if n, p := ro.NewShares.Common, ro.Proceeds.Total; n != nil && *n > 0 && p != nil && zeroOrNil(ro.NewShares.Preferred) {
			price := int64(math.Round(float64(*p) / float64(*n)))
			ro.ImpliedPrice = &price
		}
		a.RightsOffering = ro
	case "bonus_issue":
		a.BonusIssue = &BonusIssue{
			NewShares:      r.shares("nstk_ostk_cnt", "nstk_estk_cnt"),
			SharesBefore:   r.shares("bfic_tisstk_ostk", "bfic_tisstk_estk"),
			SharesPerShare: r.float("nstk_ascnt_ps_ostk"),
			RecordDate:     r.date("nstk_asstd"),
			ListingDate:    r.date("nstk_lstprd"),
		}
	case "capital_reduction":
		a.CapitalReduction = &CapitalReduction{
			SharesReduced: r.shares("crstk_ostk_cnt", "crstk_estk_cnt"),
			SharesBefore:  r.shares("bfcr_tisstk_ostk", "bfcr_tisstk_estk"),
			SharesAfter:   r.shares("atcr_tisstk_ostk", "atcr_tisstk_estk"),
			CapitalBefore: r.int("bfcr_cpt"),
			CapitalAfter:  r.int("atcr_cpt"),
			Percent:       r.float("cr_rt_ostk"),
			RecordDate:    r.date("cr_std"),
			ListingDate:   r.date("crsc_nstklstprd"),
			Method:        r.text("cr_mth"),
			Reason:        r.text("cr_rs"),
		}
	case "convertible_bond", "bond_with_warrants", "exchangeable_bond":
		a.Bond = r.bond(t)
	case "buyback":
		a.Buyback = &Buyback{
			Shares:     r.shares("aqpln_stk_ostk", "aqpln_stk_estk"),
			Amount:     r.amounts("aqpln_prc_ostk", "aqpln_prc_estk"),
			Start:      r.date("aqexpd_bgd"),
			End:        r.date("aqexpd_edd"),
			Purpose:    r.text("aq_pp"),
			Method:     r.text("aq_mth"),
			Brokers:    r.text("cs_iv_bk"),
			HeldBefore: r.held(),
		}
	case "treasury_disposal":
		a.Buyback = &Buyback{
			Shares:        r.shares("dppln_stk_ostk", "dppln_stk_estk"),
			PricePerShare: r.shares("dpstk_prc_ostk", "dpstk_prc_estk"),
			Amount:        r.amounts("dppln_prc_ostk", "dppln_prc_estk"),
			Start:         r.date("dpprpd_bgd"),
			End:           r.date("dpprpd_edd"),
			Purpose:       r.text("dp_pp"),
			Method:        disposalMethod(r),
			Brokers:       r.text("cs_iv_bk"),
			HeldBefore:    r.held(),
		}
	case "buyback_trust":
		a.Trust = &BuybackTrust{
			Amount: r.int("ctr_prc"), Start: r.date("ctr_pd_bgd"), End: r.date("ctr_pd_edd"),
			Date: r.date("ctr_cns_prd"), Purpose: r.text("ctr_pp"), Trustee: r.text("ctr_cns_int"), HeldBefore: r.held(),
		}
	case "buyback_trust_end":
		a.Trust = &BuybackTrust{
			Amount: r.int("ctr_prc_bfcc"), AmountAfter: r.int("ctr_prc_atcc"), Start: r.date("ctr_pd_bfcc_bgd"), End: r.date("ctr_pd_bfcc_edd"),
			Date: r.date("cc_prd"), Purpose: r.text("cc_pp"), Trustee: r.text("cc_int"), HeldBefore: r.held(),
		}
	case "merger":
		a.Merger = &Merger{
			Counterparty:        r.text("mgptncmp_cmpnm"),
			Relation:            r.text("mgptncmp_rl_cmpn"),
			Form:                strings.TrimPrefix(r.text("mg_stn"), "해당사항없음"),
			Method:              r.text("mg_mth"),
			Ratio:               r.text("mg_rt"),
			NewShares:           r.shares("mgnstk_ostk_cnt", "mgnstk_cstk_cnt"),
			AppraisalPrice:      r.int("aprskh_plnprc"),
			ShareholdersMeeting: r.date("mgsc_gmtsck_prd"),
			Date:                r.date("mgsc_mgdt"),
			Purpose:             r.text("mg_pp"),
		}
	case "split":
		a.Split = &Split{
			Kind:                splitKind(r["ex_sm_r"] + r["dv_rt"] + r["dv_mth"]),
			NewCompany:          r.text("dvfcmp_cmpnm"),
			NewCompanyBusiness:  r.text("dvfcmp_mbsn"),
			Method:              r.text("dv_mth"),
			Ratio:               r.text("dv_rt"),
			ShareholdersMeeting: r.date("gmtsck_prd"),
			Date:                r.date("dvdt"),
		}
	}
	return a
}

// bond reads a convertible bond, a bond with warrants or an exchangeable
// bond; the three name the same terms differently.
func (r row) bond(t string) *Bond {
	b := &Bond{
		Series:          r.text("bd_tm"),
		Description:     r.text("bd_knd"),
		Offering:        r.text("bdis_mthn"),
		FaceAmount:      r.int("bd_fta"),
		CouponRate:      r.float("bd_intr_ex"),
		YieldToMaturity: r.float("bd_intr_sf"),
		Maturity:        r.date("bd_mtd"),
		PaymentDate:     r.date("pymd"),
		FloorPrice:      r.int("act_mktprcfl_cvprc_lwtrsprc"),
		Proceeds:        r.proceeds(),
	}
	var rate *float64
	switch t {
	case "convertible_bond":
		b.Underlying, b.Price, b.Shares, b.SharesPercent = r.text("cvisstk_knd"), r.int("cv_prc"), r.int("cvisstk_cnt"), r.float("cvisstk_tisstk_vs")
		b.ExerciseStart, b.ExerciseEnd = r.date("cvrqpd_bgd"), r.date("cvrqpd_edd")
		rate = r.float("cv_rt")
	case "bond_with_warrants":
		b.Underlying, b.Price, b.Shares, b.SharesPercent = r.text("nstk_isstk_knd"), r.int("ex_prc"), r.int("nstk_isstk_cnt"), r.float("nstk_isstk_tisstk_vs")
		b.ExerciseStart, b.ExerciseEnd = r.date("expd_bgd"), r.date("expd_edd")
		b.Detachable = r.text("bdwt_div_atn")
		rate = r.float("ex_rt")
	case "exchangeable_bond":
		b.Underlying, b.Price, b.Shares, b.SharesPercent = r.text("extg"), r.int("ex_prc"), r.int("extg_stkcnt"), r.float("extg_tisstk_vs")
		b.ExerciseStart, b.ExerciseEnd = r.date("exrqpd_bgd"), r.date("exrqpd_edd")
	}
	if f, p := b.FaceAmount, b.FloorPrice; f != nil && p != nil && *p > 0 && (b.Price == nil || *p < *b.Price) {
		ratio := 1.0
		if rate != nil && *rate > 0 {
			ratio = *rate / 100
		}
		n := int64(math.Floor(float64(*f) * ratio / float64(*p)))
		b.SharesAtFloor = &n
	}
	return b
}

func (r row) proceeds() Proceeds {
	p := Proceeds{
		Facilities:          r.int("fdpp_fclt"),
		BusinessAcquisition: r.int("fdpp_bsninh"),
		Operating:           r.int("fdpp_op"),
		DebtRepayment:       r.int("fdpp_dtrp"),
		SecuritiesPurchase:  r.int("fdpp_ocsa"),
		Other:               r.int("fdpp_etc"),
	}
	p.Total = sum(p.Facilities, p.BusinessAcquisition, p.Operating, p.DebtRepayment, p.SecuritiesPurchase, p.Other)
	return p
}

func (r row) amounts(common, preferred string) Amounts {
	a := Amounts{Common: r.int(common), Preferred: r.int(preferred)}
	a.Total = sum(a.Common, a.Preferred)
	return a
}

// held is the treasury stock held before a buyback decision: shares bought
// within distributable profit (배당가능이익 범위 내 취득) and otherwise (기타취득).
func (r row) held() ShareCounts {
	return ShareCounts{
		Common:    sum(r.int("aq_wtn_div_ostk"), r.int("eaq_ostk")),
		Preferred: sum(r.int("aq_wtn_div_estk"), r.int("eaq_estk")),
	}
}

// disposalMethod lists how treasury shares are sold, with share counts.
func disposalMethod(r row) string {
	var parts []string
	for _, m := range []struct{ field, name string }{
		{"dp_m_mkt", "시장을 통한 매도"}, {"dp_m_ovtm", "시간외대량매매"}, {"dp_m_otc", "장외처분"}, {"dp_m_etc", "기타"},
	} {
		if r.int(m.field) != nil {
			parts = append(parts, m.name+" "+r[m.field]+"주")
		}
	}
	return strings.Join(parts, ", ")
}

func splitKind(s string) string {
	switch {
	case strings.Contains(s, "물적"):
		return "물적분할"
	case strings.Contains(s, "인적"):
		return "인적분할"
	}
	return ""
}

// sum adds the values that are set; nil when none is.
func sum(vs ...*int64) *int64 {
	var total *int64
	for _, v := range vs {
		if v == nil {
			continue
		}
		if total == nil {
			total = new(int64)
		}
		*total += *v
	}
	return total
}

func percent(part, whole *int64) *float64 {
	if part == nil || whole == nil || *whole == 0 {
		return nil
	}
	p := math.Round(float64(*part)/float64(*whole)*10000) / 100
	return &p
}

func zeroOrNil(n *int64) bool { return n == nil || *n == 0 }
