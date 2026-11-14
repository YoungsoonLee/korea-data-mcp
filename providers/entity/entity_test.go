package entity

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/providers/datagokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
)

// fake answers the opendart and datagokr tools from the values below, taken
// from live responses on 2026-11-12.
type fake struct {
	mu       sync.Mutex
	calls    []string
	disabled map[string]bool // provider IDs
}

var (
	kakao = opendart.CompanyProfile{CorpCode: "00258801", CorpName: "(주)카카오", CorpNameEng: "Kakao Corp.", StockName: "카카오", StockCode: "035720",
		Market: "KOSPI", CEO: "정신아", CorporateRegistration: "1101111129497", BusinessRegistration: "1208147521"}
	// Merged into Daum in 2014; its business number is closed.
	oldKakao = opendart.CompanyProfile{CorpCode: "00918444", CorpName: "(주)카카오", CorpNameEng: "Kakao.Corp", StockName: "카카오",
		Market: "OTHER", CEO: "이제범,이석우", CorporateRegistration: "1101113570440", BusinessRegistration: "1198193985"}
	samsung = opendart.CompanyProfile{CorpCode: "00126380", CorpName: "삼성전자(주)", StockName: "삼성전자", StockCode: "005930",
		Market: "KOSPI", CorporateRegistration: "1301110006246", BusinessRegistration: "1248100998"}
	hanwhaOcean = opendart.CompanyProfile{CorpCode: "00111704", CorpName: "한화오션(주)", StockName: "한화오션", StockCode: "042660",
		Market: "KOSPI", CorporateRegistration: "1101112095837", BusinessRegistration: "1048157667"}
	baropharm = opendart.CompanyProfile{CorpCode: "01835829", CorpName: "주식회사 바로팜", StockName: "바로팜", StockCode: "0153R0",
		Market: "KOSDAQ", CorporateRegistration: "1101117302261", BusinessRegistration: "1058795483"}
	// A foreign company: no crno, and not a business number.
	psomagen = opendart.CompanyProfile{CorpCode: "01442115", CorpName: "주식회사 소마젠", StockName: "소마젠", StockCode: "950200",
		Market: "KOSDAQ", BusinessRegistration: "MC178082"}

	dartProfiles = map[string]opendart.CompanyProfile{
		"00258801": kakao, "035720": kakao, "00918444": oldKakao,
		"00126380": samsung, "005930": samsung, "005935": samsung,
		"00111704": hanwhaOcean, "01835829": baropharm,
		"01442115": psomagen, "950200": psomagen,
	}
	dartSearch = map[string][]opendart.Company{
		"카카오":    {{CorpCode: "00258801", CorpName: "카카오", StockCode: "035720", Listed: true, Market: "KOSPI"}, {CorpCode: "00918444", CorpName: "카카오"}, {CorpCode: "01103245", CorpName: "카카오게임즈", StockCode: "293490", Listed: true, Market: "KOSDAQ"}},
		"바로팜":    {{CorpCode: "01835829", CorpName: "바로팜"}},
		"대우조선해양": {}, // DART's list has the new name only
		"없는회사":   {},
		"삼성":     {{CorpCode: "00126380", CorpName: "삼성전자", StockCode: "005930", Listed: true, Market: "KOSPI"}},
	}
	fscProfiles = map[string]datagokr.CorporationProfile{
		"1101111129497": {Corporation: datagokr.Corporation{Crno: "1101111129497", Name: "(주)카카오", BizRegNo: "1208147521", DartCorpCode: "00258801", Market: "KOSPI"}},
		"1101113570440": {Corporation: datagokr.Corporation{Crno: "1101113570440", Name: "(주)카카오", BizRegNo: "1198193985", DartCorpCode: "00918444", Market: "OTHER"}},
		"2001110423487": {Corporation: datagokr.Corporation{Crno: "2001110423487", Name: "카카오", BizRegNo: "8198700026", CEO: "조승범", Market: "OTHER"}},
		"1301110006246": {Corporation: datagokr.Corporation{Crno: "1301110006246", Name: "삼성전자(주)", BizRegNo: "1248100998", DartCorpCode: "00126380", Market: "KOSPI"}},
		"1101112095837": {Corporation: datagokr.Corporation{Crno: "1101112095837", Name: "한화오션(주)", BizRegNo: "1048157667", DartCorpCode: "00111704", Market: "KOSPI"},
			FormerNames: []datagokr.FormerName{{Name: "대우조선해양(주)", Until: "2023-06-12"}}},
		"1101117302261": {Corporation: datagokr.Corporation{Crno: "1101117302261", Name: "주식회사 바로팜", BizRegNo: "1058795483", DartCorpCode: "01835829", Market: "KOSDAQ"}},
	}
	fscSearch = map[string][]datagokr.Corporation{
		"카카오": {{Crno: "1101111129497", Name: "(주)카카오", DartCorpCode: "00258801", Market: "KOSPI"}, {Crno: "2001110423487", Name: "카카오", BizRegNo: "8198700026", CEO: "조승범", Market: "OTHER"},
			{Crno: "1101113570440", Name: "(주)카카오", DartCorpCode: "00918444", Market: "OTHER"}},
		// A search by a former name finds only the records with that name.
		"대우조선해양": {{Crno: "1101112095837", Name: "대우조선해양(주)", BizRegNo: "1048157667", DartCorpCode: "00111704", CEO: "권혁웅", Market: "KOSPI"}},
	}
	nts = map[string]datagokr.BusinessStatus{
		"1208147521": {Registered: true, Status: "active"},
		"1198193985": {Registered: true, Status: "closed", ClosedDate: "2014-10-01"},
		"8198700026": {Registered: true, Status: "active"},
		"1248100998": {Registered: true, Status: "active"},
		"1048157667": {Registered: true, Status: "active"},
		"1058795483": {Registered: true, Status: "active"},
		"2208162517": {Registered: true, Status: "active"}, // not in the FSC registry here
	}
	stocks = map[string]datagokr.Stock{
		"0153R0":       {Code: "0153R0", Name: "바로팜", Market: "KOSDAQ"},
		"KR8840150005": {Code: "950200", Name: "소마젠", Market: "KOSDAQ"},
	}
)

func (f *fake) call(_ context.Context, name string, args, out any) error {
	f.mu.Lock()
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, name+" "+string(b))
	f.mu.Unlock()
	provider := strings.SplitN(name, "_", 2)[0]
	if f.disabled[provider] {
		return &hub.Error{Code: hub.CodeUnauthenticated, Provider: provider, Message: provider + " is not enabled: KEY not set"}
	}
	var a map[string]any
	json.Unmarshal(b, &a)
	str := func(k string) string { s, _ := a[k].(string); return s }
	notFound := hub.Errorf(hub.CodeNotFound, "not found")

	var res any
	switch name {
	case "opendart_get_company":
		p, ok := dartProfiles[str("corp_code")]
		if !ok {
			return notFound
		}
		res = p
	case "opendart_search_companies":
		res = hub.List[opendart.Company]{Items: dartSearch[str("query")]}
	case "datagokr_search_corporations":
		if str("query") == "삼성" {
			return hub.Errorf(hub.CodeInvalidArgument, "\"삼성\" matches 3158 records, too many to rank")
		}
		res = hub.List[datagokr.Corporation]{Items: fscSearch[str("query")]}
	case "datagokr_get_corporation":
		for _, p := range fscProfiles {
			if p.Crno == str("crno") || (str("business_number") != "" && p.BizRegNo == str("business_number")) {
				res = p
			}
		}
		if res == nil {
			return notFound
		}
	case "datagokr_check_business_status":
		var l hub.List[datagokr.BusinessStatus]
		for _, n := range a["business_numbers"].([]any) {
			s := nts[n.(string)]
			s.BizRegNo = n.(string)
			l.Items = append(l.Items, s)
		}
		res = l
	case "datagokr_get_stock_prices":
		s, ok := stocks[str("stock")]
		if !ok {
			return notFound
		}
		res = datagokr.StockPrices{Stock: s}
	default:
		panic(name)
	}
	b, _ = json.Marshal(res)
	return json.Unmarshal(b, out)
}

func (f *fake) called(prefix string) []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func resolve(t *testing.T, f *fake, q string) (Resolution, *hub.Error) {
	t.Helper()
	got, err := (&resolver{call: f.call}).resolve(context.Background(), resolveInput{Query: q})
	return got, hub.AsError(err)
}

func ids(r Resolution) string {
	var s []string
	for _, c := range r.Companies {
		s = append(s, c.EntityID)
	}
	return strings.Join(s, ",")
}

func TestResolveSharedName(t *testing.T) {
	f := &fake{}
	got, e := resolve(t, f, "카카오")
	if e != nil {
		t.Fatal(e)
	}
	// Three companies are named 카카오: the listed one, an unrelated
	// company outside DART, and the one closed in 2014. 카카오게임즈 only
	// starts with the name.
	if ids(got) != "crno:1101111129497,crno:2001110423487,crno:1101113570440" || got.MatchedBy != "name" {
		t.Fatalf("got %s", ids(got))
	}
	k := got.Companies[0]
	if k.Name != "(주)카카오" || k.CorpCode != "00258801" || k.BusinessNumber != "1208147521" || !k.Listed || k.Market != "KOSPI" || k.BusinessStatus != "active" || k.StockCode != "035720" {
		t.Errorf("listed: %+v", k)
	}
	old := got.Companies[2]
	if old.CorpCode != "00918444" || old.Listed || old.BusinessStatus != "closed" || old.ClosedDate != "2014-10-01" {
		t.Errorf("closed: %+v", old)
	}
	if other := got.Companies[1]; other.CorpCode != "" || other.Name != "카카오" || other.CEO != "조승범" {
		t.Errorf("outside DART: %+v", other)
	}
	if !strings.Contains(got.Note, "3 companies have this name") || got.Incomplete != nil {
		t.Errorf("note %q, incomplete %v", got.Note, got.Incomplete)
	}
	// One status call for all business numbers.
	if n := len(f.called("datagokr_check_business_status")); n != 1 {
		t.Errorf("status calls: %d", n)
	}
}

func TestResolveFormerName(t *testing.T) {
	got, e := resolve(t, &fake{}, "대우조선해양")
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Companies) != 1 {
		t.Fatalf("got %+v", got)
	}
	c := got.Companies[0]
	if c.Name != "한화오션(주)" || c.MatchedName != "대우조선해양(주)" || c.CorpCode != "00111704" || c.StockCode != "042660" ||
		len(c.FormerNames) != 1 || c.FormerNames[0].Name != "대우조선해양(주)" || c.EntityID != "crno:1101112095837" {
		t.Errorf("got %+v", c)
	}
}

func TestResolveNumbers(t *testing.T) {
	for _, tc := range []struct{ q, by, id, note string }{
		{"00126380", "corp_code", "crno:1301110006246", ""},
		{"130111-0006246", "crno", "crno:1301110006246", ""},
		{"124-81-00998", "business_number", "crno:1301110006246", ""},
		{"005930", "stock_code", "crno:1301110006246", ""},
		{"005935", "stock_code", "crno:1301110006246", "common share code is usually 005930"},
		{"KR7005930003", "isin", "crno:1301110006246", ""},
		{"KR7005931001", "isin", "crno:1301110006246", ""}, // 삼성전자우, code 005935
		{"KR8840150005", "isin", "dart:01442115", ""},
		{"950200", "stock_code", "dart:01442115", ""},
	} {
		f := &fake{}
		got, e := resolve(t, f, tc.q)
		if e != nil {
			t.Fatalf("%s: %v", tc.q, e)
		}
		if got.MatchedBy != tc.by || ids(got) != tc.id || !strings.Contains(got.Note, tc.note) {
			t.Errorf("%s: by %s, ids %s, note %q", tc.q, got.MatchedBy, ids(got), got.Note)
		}
		if c := got.Companies[0]; c.CorpCode == "" || c.Name == "" {
			t.Errorf("%s: %+v", tc.q, c)
		}
	}
	// 소마젠's business number is not one; it is not sent to the NTS.
	f := &fake{}
	got, _ := resolve(t, f, "950200")
	if got.Companies[0].BusinessNumber != "" || len(f.called("datagokr_check_business_status")) != 0 {
		t.Errorf("foreign company: %+v %v", got.Companies[0], f.calls)
	}
}

func TestResolveNewListing(t *testing.T) {
	// DART's list lacks the stock code of a company listed weeks ago, so
	// opendart_get_company does not know 0153R0.
	got, e := resolve(t, &fake{}, "0153R0")
	if e != nil {
		t.Fatal(e)
	}
	if c := got.Companies[0]; c.CorpCode != "01835829" || c.StockCode != "0153R0" || !c.Listed || !strings.Contains(got.Note, "matched by the listed name 바로팜") {
		t.Errorf("got %+v, note %q", c, got.Note)
	}

	// Without OpenDART, KRX prices still name it.
	got, e = resolve(t, &fake{disabled: map[string]bool{"opendart": true}}, "0153R0")
	if e != nil {
		t.Fatal(e)
	}
	if c := got.Companies[0]; c.Name != "바로팜" || c.CorpCode != "" || !c.Listed || len(got.Incomplete) != 1 || !strings.Contains(got.Incomplete[0], "opendart") {
		t.Errorf("got %+v, incomplete %v", c, got.Incomplete)
	}
}

func TestResolveBusinessNumberOnlyAtNTS(t *testing.T) {
	got, e := resolve(t, &fake{}, "220-81-62517")
	if e != nil {
		t.Fatal(e)
	}
	if c := got.Companies[0]; c.EntityID != "bizno:2208162517" || c.BusinessStatus != "active" || c.Name != "" || !strings.Contains(got.Note, "National Tax Service") {
		t.Errorf("got %+v, note %q", c, got.Note)
	}
}

func TestResolveWithOneKey(t *testing.T) {
	// Without data.go.kr: no crno from the FSC, no business status.
	got, e := resolve(t, &fake{disabled: map[string]bool{"datagokr": true}}, "카카오")
	if e != nil {
		t.Fatal(e)
	}
	if ids(got) != "crno:1101111129497,crno:1101113570440" || got.Companies[0].BusinessStatus != "" ||
		len(got.Incomplete) != 1 || !strings.HasPrefix(got.Incomplete[0], "datagokr") {
		t.Errorf("got %s, incomplete %v", ids(got), got.Incomplete)
	}
	// A broad name the FSC refuses still resolves through DART.
	got, e = resolve(t, &fake{}, "삼성")
	if e != nil || len(got.Companies) != 1 || !strings.Contains(got.Note, "closest") || !slices.ContainsFunc(got.Incomplete, func(s string) bool { return strings.Contains(s, "too many") }) {
		t.Errorf("got %+v, %v", got, e)
	}
}

func TestResolveErrors(t *testing.T) {
	if _, e := resolve(t, &fake{}, " (주) "); e == nil || e.Code != hub.CodeInvalidArgument {
		t.Errorf("empty: %v", e)
	}
	if _, e := resolve(t, &fake{}, "9999999999999"); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown crno: %v", e)
	}
	if _, e := resolve(t, &fake{}, "없는회사"); e == nil || e.Code != hub.CodeNotFound {
		t.Errorf("unknown name: %v", e)
	}
	_, e := resolve(t, &fake{disabled: map[string]bool{"opendart": true, "datagokr": true}}, "카카오")
	if e == nil || e.Code != hub.CodeUnauthenticated || !strings.Contains(e.Message, "opendart") || !strings.Contains(e.Message, "datagokr") {
		t.Errorf("no keys: %v", e)
	}
	// Only data.go.kr knows crnos: not "not found" when it could not look.
	_, e = resolve(t, &fake{disabled: map[string]bool{"datagokr": true}}, "1234567890123")
	if e == nil || e.Code != hub.CodeUnauthenticated {
		t.Errorf("crno without data.go.kr: %v", e)
	}
}
