package opendart

import (
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// SK Square, 2025: SK hynix filed as 에스케이하이닉스㈜, SK플래닛 listed by
// DART as 에스케이플래닛, ten stakes sold during the year (11번가, 코빗, ...)
// and preferred shares as their own rows.
func TestGetInvestments(t *testing.T) {
	f, cs := setup(t, testKey)
	var got Investments
	mustOK(t, call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "01596425", "year": 2025}, &got))
	if q := f.query("otrCprInvstmntSttus.json"); q.Get("corp_code") != "01596425" || q.Get("bsns_year") != "2025" || q.Get("reprt_code") != "11011" {
		t.Errorf("query: %v", q)
	}
	s := got.Summary
	// the book value adds up to the filer's own 합계 row, which is skipped
	if s.Held != 15 || s.Exited != 10 || *s.BookValue != 5_578_460_000_000 || s.CorpCodes != 2 || got.PageInfo.TotalCount != 25 || got.Note != "" {
		t.Fatalf("summary: %+v %+v %q", s, got.PageInfo, got.Note)
	}
	if got.AsOf != "2025-12-31" || got.ReceiptNo != "20260317000830" || got.URL == "" {
		t.Errorf("header: %+v", got)
	}
	hynix := got.Items[0]
	if hynix.Name != "에스케이하이닉스㈜" || hynix.CorpCode != "00164779" || hynix.StockCode != "000660" || hynix.Purpose != "control" || hynix.PurposeKo != "경영참가" ||
		*hynix.End.Percent != 20.1 || *hynix.End.Shares != 146_100_000 || hynix.FirstAcquired != "2021-11-02" || *hynix.Investee.NetIncome != 19_796_902_000_000 {
		t.Errorf("SK hynix: %+v", hynix)
	}
	if p := got.Items[1]; p.Name != "SK플래닛㈜" || p.CorpCode != "00888736" || p.StockCode != "" || *p.SharesChange != 111_491_086 || *p.End.Percent != 99.5 {
		t.Errorf("SK플래닛: %+v", p)
	}
	last := got.Items[24]
	if last.Name != "11번가㈜" || !last.Exited || last.CorpCode != "" || *last.Start.Percent != 80.3 || last.End.Percent != nil || *last.SharesChange != -41_123_545 {
		t.Errorf("11번가: %+v", last)
	}
	var preferred, exitedPreferred bool
	for i, it := range got.Items {
		if (i < s.Held) == it.Exited {
			t.Errorf("held stakes first: %d %+v", i, it)
		}
		if it.Name == "TGC Square Pte. Ltd" && it.Preferred && *it.End.BookValue == 11_940_000_000 {
			preferred = true
		}
		if it.Name == "㈜스파크플러스" && it.Preferred && it.Exited {
			exitedPreferred = true
		}
	}
	if !preferred || !exitedPreferred {
		t.Error("preferred rows not found")
	}

	mustOK(t, call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "01596425", "year": 2025, "report": "half", "purpose": "investment"}, &got))
	if f.query("otrCprInvstmntSttus.json").Get("reprt_code") != "11012" || got.Report != "half" || got.Summary.Held != 2 || got.Summary.Exited != 3 || *got.Summary.BookValue != 197_109_000_000 {
		t.Errorf("investment only: %+v", got.Summary)
	}
	for _, it := range got.Items {
		if it.Purpose != "investment" {
			t.Errorf("purpose filter: %+v", it)
		}
	}
}

// Samsung Electronics filed its 2020 table only as an attachment: status 000
// with a "-" row and a 합계 row.
func TestGetInvestmentsAttached(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("otrCprInvstmntSttus.json", fixture(t, "otrCprInvstmntSttus_attached.json"))
	var got Investments
	mustOK(t, call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "005930", "year": 2020}, &got))
	if len(got.Items) != 0 || got.Items == nil || got.Note != attachedNote || got.URL == "" || got.Summary.BookValue != nil {
		t.Errorf("got %+v", got)
	}
}

// 소마젠 reports in USD; OpenDART does not say so.
func TestGetInvestmentsForeignIssuer(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("otrCprInvstmntSttus.json", fixture(t, "otrCprInvstmntSttus_usd.json"))
	var got Investments
	mustOK(t, call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "950200", "year": 2025}, &got))
	if got.CorpCode != "01442115" || got.Note != foreignNote || got.Summary.Held != 3 {
		t.Errorf("got %+v", got)
	}
}

// 유성기업: Japanese investees' financials in 백만엔 and a joint venture's in
// 천RMB, noted only in the purpose cell; notes in the investee's name.
func TestGetInvestmentsUnitNotes(t *testing.T) {
	f, cs := setup(t, testKey)
	f.set("otrCprInvstmntSttus.json", fixture(t, "otrCprInvstmntSttus_units.json"))
	var got Investments
	mustOK(t, call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "00174004", "year": 2025}, &got))
	by := map[string]Investment{}
	for _, it := range got.Items {
		by[it.Name] = it
	}
	if n := by["NITTAN VALVE"]; n.NameNote != "해외법인 일본상장" || n.Purpose != "investment" || n.PurposeKo != "일반투자" ||
		n.Investee.UnitNote != "연결총자산,순이익 단위:백만엔" || *n.Investee.TotalAssets != 66_613_000_000 {
		t.Errorf("NITTAN: %+v %+v", n, n.Investee)
	}
	if r := by["일조애사비기차배건유한공사 (RSP)"]; r.Purpose != "control" || r.Investee == nil || r.Investee.UnitNote != "연결총자산,순이익 단위:천RMB" {
		t.Errorf("RSP: %+v", r)
	}
	if d := by["동서페더럴모굴"]; d.Investee == nil || d.Investee.UnitNote != "" {
		t.Errorf("KRW investee: %+v", d)
	}
}

func TestGetInvestmentsErrors(t *testing.T) {
	f, cs := setup(t, testKey)
	for _, args := range []map[string]any{
		{"corp_code": "005930", "year": 2025, "report": "q1"},
		{"corp_code": "005930", "year": 2025, "purpose": "affiliates"},
		{"corp_code": "005930", "year": 2014},
	} {
		if e := call(t, cs, "opendart_get_investments", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	f.set("otrCprInvstmntSttus.json", `{"status":"013","message":"조회된 데이타가 없습니다."}`)
	if e := call(t, cs, "opendart_get_investments", map[string]any{"corp_code": "005930", "year": 2025}, nil); e == nil || e.Code != hub.CodeNotFound || e.Hint != notFiledHint {
		t.Errorf("not filed: %+v", e)
	}
}

func TestInvesteeName(t *testing.T) {
	for in, want := range map[string][3]string{
		"NITTAN VALVE (해외법인 일본상장)":            {"NITTAN VALVE", "해외법인 일본상장", ""},
		"Dong-A Steel Technology (B) Sdn Bhd": {"Dong-A Steel Technology (B) Sdn Bhd", "", ""},
		"대성에너지제5서비스(주)":                       {"대성에너지제5서비스(주)", "", ""},
		"삼성생명보험㈜(특별계정)":                       {"삼성생명보험㈜", "특별계정", ""},
		"㈜스파크플러스(우선주)":                        {"㈜스파크플러스", "", "preferred"},
		"TGC Square Pte. Ltd (우선주)":           {"TGC Square Pte. Ltd", "", "preferred"},
	} {
		name, note, preferred := investeeName(in)
		if name != want[0] || note != want[1] || preferred != (want[2] != "") {
			t.Errorf("investeeName(%q) = %q, %q, %v", in, name, note, preferred)
		}
	}
	for in, want := range map[string]string{
		"경영참여": "control", "경영 참가": "control", "단순투자 (신설법인 지분참여)": "investment", "일반투자": "investment",
		"계열사 투자": "control", "해외투자": "investment", "원료구매 안정화": "other", "": "other",
	} {
		if got := investmentPurpose(in); got != want {
			t.Errorf("investmentPurpose(%q) = %q, want %q", in, got, want)
		}
	}
}
