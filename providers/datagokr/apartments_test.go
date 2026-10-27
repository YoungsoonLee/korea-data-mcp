package datagokr

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

// Expected values were computed independently from the fixture (233 real
// trades in 서울 강남구, 2025-09, 12 of them cancelled).
func TestListApartmentTrades(t *testing.T) {
	f, cs := setup(t, map[string]string{"getRTMSDataSvcAptTrade": "apt_trades_gangnam_202509.xml"})

	var got ApartmentTrades
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "11680", "year_month": "2025-09"}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("getRTMSDataSvcAptTrade")
	if q.Get("LAWD_CD") != "11680" || q.Get("DEAL_YMD") != "202509" || q.Get("numOfRows") != "1000" {
		t.Errorf("query: %v", q)
	}
	s := got.Summary
	if s.Trades != 221 || s.CancelledTrades != 12 || *s.MedianPrice != 2130000000 || *s.MinPrice != 200000000 || *s.MaxPrice != 12200000000 {
		t.Errorf("summary: %+v", s)
	}
	if got.Region != "서울특별시 강남구" || len(got.DistrictCodes) != 1 || got.Items[0].District != "강남구" {
		t.Errorf("region: %q %v, item district %q", got.Region, got.DistrictCodes, got.Items[0].District)
	}
	if got.PageInfo.TotalCount != 221 || len(got.Items) != 50 || got.Items[0].Date != "2025-09-30" {
		t.Errorf("page: %+v, first %+v", got.PageInfo, got.Items[0])
	}
	for _, it := range got.Items {
		if it.Cancelled {
			t.Errorf("cancelled deal listed by default: %+v", it)
		}
	}

	got = ApartmentTrades{}
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "11680", "year_month": "202509", "include_cancelled": true, "page_size": 500}, &got); e != nil {
		t.Fatal(e)
	}
	var cancelled *ApartmentTrade
	for i := range got.Items {
		if got.Items[i].Apartment == "아이파크" && got.Items[i].Cancelled {
			cancelled = &got.Items[i]
		}
	}
	if len(got.Items) != 233 || got.Summary.Trades != 221 || cancelled == nil {
		t.Fatalf("include_cancelled: %d items, %+v", len(got.Items), got.Summary)
	}
	// "750,000" in 10,000 KRW; cancelled on "26.03.18".
	if *cancelled.Price != 7500000000 || cancelled.Date != "2025-09-23" || cancelled.CancelledDate != "2026-03-18" || cancelled.RegisteredDate != "" {
		t.Errorf("cancelled deal: %+v", *cancelled)
	}

	got = ApartmentTrades{}
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "서울 강남구", "year_month": "2025-09", "apartment": "래미안"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Summary.Trades != 25 {
		t.Errorf("apartment filter: %+v", got.Summary)
	}
	got = ApartmentTrades{}
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "11680", "year_month": "2025-09", "neighborhood": "삼성동"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Summary.Trades != 19 || got.Summary.CancelledTrades != 2 {
		t.Errorf("neighborhood filter: %+v", got.Summary)
	}
}

func TestListApartmentTradesFields(t *testing.T) {
	_, cs := setup(t, map[string]string{"getRTMSDataSvcAptTrade": "apt_trades.xml"})
	var got ApartmentTrades
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "11110", "year_month": "2025-09"}, &got); e != nil {
		t.Fatal(e)
	}
	var it ApartmentTrade
	for _, x := range got.Items {
		if x.Apartment == "종로청계힐스테이트" {
			it = x
		}
	}
	if it.Date != "2025-09-01" || *it.Price != 845000000 || *it.AreaM2 != 59.9426 || *it.Floor != 4 || *it.BuildYear != 2009 ||
		it.Building != "101" || it.Neighborhood != "숭인동" || it.DealType != "brokered" || it.Buyer != "individual" ||
		it.RegisteredDate != "2025-11-28" || it.LandLease || it.Cancelled || it.CancelledDate != "" {
		t.Errorf("got %+v", it)
	}
}

func TestListApartmentTradesValidation(t *testing.T) {
	_, cs := setup(t, map[string]string{"getRTMSDataSvcAptTrade": "apt_trades_none.xml"})
	for _, args := range []map[string]any{
		{"region": "11680", "year_month": "2025"},    // upstream: "success, 0 results"
		{"region": "11680", "year_month": "2025-13"}, // same
		{"region": "11680", "year_month": "2005-12"}, // before the data starts
		{"region": "11680", "year_month": "2026-11"}, // future (now is 2026-10-25 in tests)
		{"region": "강남", "year_month": "2025-09"},
		{"region": "중구", "year_month": "2025-09"},    // ambiguous
		{"region": "29110", "year_month": "2025-09"}, // 광주 동구 before the merger
		{"region": "1168", "year_month": "2025-09"},
		{"region": "11680", "year_month": "2025-09", "page_size": 501},
	} {
		if e := call(t, cs, "datagokr_list_apartment_trades", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	var got ApartmentTrades
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "11680", "year_month": "2026-10"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Items == nil || got.Summary.Trades != 0 || got.Summary.MedianPrice != nil {
		t.Errorf("no trades: %+v", got)
	}
}

// A city with wards is queried ward by ward and merged.
func TestListApartmentTradesCity(t *testing.T) {
	trade := func(sgg, apt, amount, day string) string {
		return "<item><aptNm>" + apt + "</aptNm><dealAmount>" + amount + "</dealAmount><dealYear>2025</dealYear><dealMonth>9</dealMonth>" +
			"<dealDay>" + day + "</dealDay><sggCd>" + sgg + "</sggCd><umdNm>동</umdNm><cdealType> </cdealType></item>"
	}
	page := func(items ...string) string {
		return "<response><header><resultCode>000</resultCode></header><body><items>" + strings.Join(items, "") +
			"</items><totalCount>" + strconv.Itoa(len(items)) + "</totalCount></body></response>"
	}
	op := "getRTMSDataSvcAptTrade"
	_, cs := setup(t, map[string]string{
		op + "?LAWD_CD=41591": page(trade("41591", "만세아파트", "30,000", "3")),
		op + "?LAWD_CD=41593": page(),
		op + "?LAWD_CD=41595": page(trade("41595", "병점아파트", "40,000", "1")),
		op + "?LAWD_CD=41597": page(trade("41597", "동탄아파트", "90,000", "2"), trade("41597", "동탄아파트", "70,000", "5")),
	})
	var got ApartmentTrades
	if e := call(t, cs, "datagokr_list_apartment_trades", map[string]any{"region": "화성시", "year_month": "2025-09"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Region != "경기도 화성시" || !slices.Equal(got.DistrictCodes, []string{"41591", "41593", "41595", "41597"}) {
		t.Errorf("region: %q %v", got.Region, got.DistrictCodes)
	}
	if got.Summary.Trades != 4 || *got.Summary.MedianPrice != 550000000 || *got.Summary.MaxPrice != 900000000 {
		t.Errorf("summary: %+v", got.Summary)
	}
	var order []string
	for _, it := range got.Items {
		order = append(order, it.Date[8:]+" "+it.District)
	}
	if want := []string{"05 화성시 동탄구", "03 화성시 만세구", "02 화성시 동탄구", "01 화성시 병점구"}; !slices.Equal(order, want) {
		t.Errorf("items: %v", order)
	}
}
