package opendart

import (
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// Samsung Electronics' buybacks: unsorted rows, and before 2026 a common and
// a preferred amount that add up to the announced total.
func TestListCorporateActionsBuyback(t *testing.T) {
	f, cs := setup(t, testKey)
	var got CorporateActions
	mustOK(t, call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "005930", "types": []string{"buyback"}}, &got))
	q := f.query("tsstkAqDecsn.json")
	if q.Get("corp_code") != "00126380" || q.Get("bgn_de") != "20221120" || q.Get("end_de") != "20251120" {
		t.Errorf("query: %v", q)
	}
	if f.count("piicDecsn.json") != 0 {
		t.Error("only the requested types should be fetched")
	}
	if got.CorpName != "삼성전자" || got.StartDate != "2022-11-20" || got.EndDate != "2025-11-20" || len(got.Items) != 7 || got.PageInfo.TotalCount != 7 {
		t.Fatalf("got %s %s..%s, %d items", got.CorpName, got.StartDate, got.EndDate, len(got.Items))
	}
	if got.Items[0].ReceiptNo != "20260821000616" || got.Items[6].ReceiptNo != "20241118000328" {
		t.Errorf("order: %s ... %s", got.Items[0].ReceiptNo, got.Items[6].ReceiptNo)
	}
	a := got.Items[6]
	b := a.Buyback
	if a.Type != "buyback" || a.Name != "자기주식취득결정" || a.BoardDate != "2024-11-15" || a.FiledDate != "2024-11-18" || a.URL == "" {
		t.Errorf("action: %+v", a)
	}
	if *b.Amount.Common != 2_682_737_598_000 || *b.Amount.Preferred != 317_262_452_400 || *b.Amount.Total != 3_000_000_050_400 ||
		*b.Shares.Common != 50_144_628 || *b.Shares.Preferred != 6_912_036 || b.Start != "2024-11-18" || b.End != "2025-02-17" || b.Purpose != "주주가치 제고 등" {
		t.Errorf("buyback: %+v %+v", b.Amount, b)
	}
	if b := got.Items[0].Buyback; b.Amount.Preferred != nil || *b.Amount.Total != 14_999_999_992_000 {
		t.Errorf("common only: %+v", b.Amount)
	}
}

// 광진실업: no issue price and no board date in the filing; the latest
// correction of a decision first filed in July is dated October.
func TestListCorporateActionsRightsOffering(t *testing.T) {
	_, cs := setup(t, testKey)
	var got CorporateActions
	mustOK(t, call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "00104120", "types": []string{"rights_offering"}}, &got))
	if len(got.Items) != 5 {
		t.Fatalf("%d items", len(got.Items))
	}
	a := got.Items[1] // 20261029000002
	ro := a.RightsOffering
	if a.ReceiptNo != "20261029000002" || a.FiledDate != "2026-10-29" || a.BoardDate != "" {
		t.Errorf("action: %+v", a)
	}
	if ro.Method != "제3자배정증자" || *ro.NewShares.Common != 1_047_122 || *ro.SharesBefore.Common != 7_928_326 || *ro.NewSharesPercent != 13.21 ||
		*ro.ImpliedPrice != 2865 || *ro.Proceeds.Total != 3_000_004_530 || *ro.Proceeds.Operating != 3_000_004_530 || ro.Proceeds.Facilities != nil || *ro.ParValue != 500 {
		t.Errorf("rights offering: %+v %+v", ro, ro.Proceeds)
	}
}

// 파이온엑스: a conversion price of ₩557 that can be reset to ₩21, the par
// value of its USD 0.015 shares.
func TestListCorporateActionsBondFloor(t *testing.T) {
	_, cs := setup(t, testKey)
	var got CorporateActions
	mustOK(t, call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "00781202", "types": []string{"convertible_bond", "convertible_bond"}}, &got))
	if len(got.Items) != 2 {
		t.Fatalf("%d items", len(got.Items))
	}
	b := got.Items[1].Bond
	if *b.Price != 557 || *b.FloorPrice != 21 || *b.Shares != 4_308_797 || *b.SharesAtFloor != 114_285_714 || *b.SharesPercent != 8.56 ||
		*b.FaceAmount != 2_400_000_000 || *b.CouponRate != 3 || *b.YieldToMaturity != 5 || b.Maturity != "2028-11-28" || b.Offering != "사모" ||
		b.ExerciseStart != "2026-11-28" || b.Series != "30" || strings.Contains(b.Description, "\n") {
		t.Errorf("bond: %+v", b)
	}
	if got.Items[0].BoardDate != "2026-10-22" || *got.Items[0].Bond.SharesAtFloor != 3_625_377 {
		t.Errorf("second bond: %+v", got.Items[0])
	}
}

// Without types every kind is fetched once; a kind with no data is skipped.
func TestListCorporateActionsAll(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("fricDecsn.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	var got CorporateActions
	mustOK(t, call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "00126380", "start_date": "2015-01-01", "page_size": 100}, &got))
	for _, k := range actionKinds {
		if n := f.count(k.endpoint); n != 1 {
			t.Errorf("%s: %d requests", k.endpoint, n)
		}
	}
	if len(got.Items) != 26 || f.query("cmpMgDecsn.json").Get("bgn_de") != "20150101" {
		t.Fatalf("%d items", len(got.Items))
	}
	by := map[string]CorporateAction{}
	for i, a := range got.Items {
		if i > 0 && a.ReceiptNo > got.Items[i-1].ReceiptNo {
			t.Errorf("not sorted at %d", i)
		}
		by[a.ReceiptNo] = a
	}
	if tr := by["20261028000304"].Trust; tr == nil || *tr.Amount != 300_000_000_000 || tr.Start != "2026-12-01" || tr.Trustee != "KB증권" {
		t.Errorf("trust: %+v", tr)
	}
	if b := by["20250610000393"].Bond; b == nil || !strings.HasSuffix(b.Underlying, "(자기주식)") || b.FloorPrice != nil || b.SharesAtFloor != nil {
		t.Errorf("exchangeable: %+v", b)
	}
	if s := by["20261028000138"].Split; s == nil || s.Kind != "물적분할" || s.Date != "2027-04-01" || s.NewCompany != "현대모비스 램프 주식회사(가칭)" {
		t.Errorf("split: %+v", s)
	}
	if m := by["20261030000485"].Merger; m == nil || !strings.HasPrefix(m.Ratio, "샘표식품㈜ : 샘표㈜ = 1 : 2.0881235") || m.Date != "2027-02-01" || *m.AppraisalPrice != 61_707 {
		t.Errorf("merger: %+v", m)
	}
	if d := by["20251031000189"].Buyback; d == nil || *d.PricePerShare.Common != 116_200 || d.Method != "기타 2,163,918주" {
		t.Errorf("disposal: %+v", d)
	}
	if c := by["20261016000287"].CapitalReduction; c == nil || *c.Percent != 66.67 || *c.CapitalAfter != 14_787_132_500 {
		t.Errorf("capital reduction: %+v", c)
	}
}

func TestListCorporateActionsErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"corp_code": "005930", "types": []string{"dividend"}},
		{"corp_code": "005930", "start_date": "2025-12-01", "end_date": "2025-01-01"},
		{"corp_code": "005930", "end_date": "2025/01/01"},
		{"corp_code": "Samsung"},
	} {
		if e := call(t, cs, "opendart_list_corporate_actions", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	var got CorporateActions
	f.set("piicDecsn.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	mustOK(t, call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "005930", "types": []string{"rights_offering"}}, &got))
	if got.Items == nil || len(got.Items) != 0 {
		t.Errorf("none: %+v", got)
	}
	f.set("cvbdIsDecsn.json", `{"status":"020","message":"요청 제한을 초과하였습니다."}`)
	if e := call(t, cs, "opendart_list_corporate_actions", map[string]any{"corp_code": "005930"}, nil); e == nil || e.Code != hub.CodeRateLimited {
		t.Errorf("rate limited: %+v", e)
	}
}
