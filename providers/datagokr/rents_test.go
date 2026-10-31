package datagokr

import (
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// Expected values were computed independently from the fixture (217 real
// rental contracts in 서울 종로구, 2025-09: 103 new, 97 renewals, 17 unknown).
func TestListApartmentRents(t *testing.T) {
	f, cs := setup(t, map[string]string{"getRTMSDataSvcAptRent": "apt_rents_jongno_202509.xml"})

	var got ApartmentRents
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "종로구", "year_month": "2025-09"}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("getRTMSDataSvcAptRent")
	if q.Get("LAWD_CD") != "11110" || q.Get("DEAL_YMD") != "202509" || q.Get("numOfRows") != "1000" {
		t.Errorf("query: %v", q)
	}
	s := got.Summary
	if s.Contracts != 217 || s.New != 103 || s.Renewals != 97 || s.RenewalRightUsed != 28 {
		t.Errorf("counts: %+v", s)
	}
	if j := s.Jeonse; j.Contracts != 64 || *j.MedianDeposit != 602500000 || *j.MinDeposit != 165000000 || *j.MaxDeposit != 1450000000 || j.MedianMonthlyRent != nil {
		t.Errorf("jeonse: %+v", j)
	}
	if m := s.Monthly; m.Contracts != 153 || *m.MedianDeposit != 86050000 || *m.MinDeposit != 3000000 || *m.MaxDeposit != 900000000 || *m.MedianMonthlyRent != 590000 {
		t.Errorf("monthly: %+v", m)
	}
	if got.Region != "서울특별시 종로구" || got.PageInfo.TotalCount != 217 || len(got.Items) != 50 || got.Items[0].Date != "2025-09-30" {
		t.Errorf("region %q, page %+v, first %+v", got.Region, got.PageInfo, got.Items[0])
	}

	// New contracts only: the monthly rent median is far above the mixed one.
	got = ApartmentRents{}
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11110", "year_month": "202509", "contract": "new"}, &got); e != nil {
		t.Fatal(e)
	}
	if s := got.Summary; s.Contracts != 103 || s.Renewals != 0 || s.Jeonse.Contracts != 31 || *s.Jeonse.MedianDeposit != 577500000 || *s.Monthly.MedianMonthlyRent != 960000 {
		t.Errorf("new: %+v", s)
	}

	got = ApartmentRents{}
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11110", "year_month": "2025-09", "contract": "갱신", "rent_type": "jeonse"}, &got); e != nil {
		t.Fatal(e)
	}
	if s := got.Summary; s.Contracts != 31 || s.Monthly.Contracts != 0 || *s.Jeonse.MedianDeposit != 605000000 {
		t.Errorf("renewal jeonse: %+v", s)
	}
	for _, it := range got.Items {
		if it.RentType != "jeonse" || it.Contract != "renewal" || *it.MonthlyRent != 0 {
			t.Errorf("filtered item: %+v", it)
		}
	}

	got = ApartmentRents{}
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11110", "year_month": "2025-09", "apartment": "경희궁 자이"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Summary.Contracts != 76 {
		t.Errorf("apartment filter: %+v", got.Summary)
	}
	got = ApartmentRents{}
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11110", "year_month": "2025-09", "neighborhood": "행촌동"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Summary.Contracts != 47 {
		t.Errorf("neighborhood filter: %+v", got.Summary)
	}
}

func TestListApartmentRentsFields(t *testing.T) {
	_, cs := setup(t, map[string]string{"getRTMSDataSvcAptRent": "apt_rents_jongno_202509.xml"})
	var got ApartmentRents
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11110", "year_month": "2025-09", "apartment": "청계로벤하임", "page_size": 500}, &got); e != nil {
		t.Fatal(e)
	}
	// deposit "23,600", monthlyRent "2", contractTerm "25.10~27.10", preMonthlyRent blank.
	var it *ApartmentRent
	for i := range got.Items {
		if x := got.Items[i]; x.Date == "2025-09-04" && x.RenewalRightUsed && *x.Deposit == 236000000 {
			it = &got.Items[i]
		}
	}
	if it == nil {
		t.Fatalf("contract not found in %d items", len(got.Items))
	}
	if it.RentType != "monthly" || *it.MonthlyRent != 20000 || it.Contract != "renewal" || *it.PreviousDeposit != 236000000 || it.PreviousMonthlyRent != nil ||
		it.TermStart != "2025-10" || it.TermEnd != "2027-10" || *it.AreaM2 != 29.797 || *it.Floor != 7 || *it.BuildYear != 2023 ||
		it.Neighborhood != "숭인동" || it.LotNumber != "240-1" || it.District != "종로구" {
		t.Errorf("got %+v", *it)
	}
}

func TestListApartmentRentsValidation(t *testing.T) {
	_, cs := setup(t, map[string]string{"getRTMSDataSvcAptRent": "apt_trades_none.xml"})
	for _, args := range []map[string]any{
		{"region": "11680", "year_month": "2010-12"}, // before rentals start
		{"region": "11680", "year_month": "2026-11"}, // future (now is 2026-10-25 in tests)
		{"region": "11680", "year_month": "2025"},
		{"region": "29170", "year_month": "2025-09"}, // 광주 북구 before the merger
		{"region": "11680", "year_month": "2025-09", "rent_type": "반전세"},
		{"region": "11680", "year_month": "2025-09", "contract": "all"},
	} {
		if e := call(t, cs, "datagokr_list_apartment_rents", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	var got ApartmentRents
	if e := call(t, cs, "datagokr_list_apartment_rents", map[string]any{"region": "11680", "year_month": "2011-01"}, &got); e != nil {
		t.Fatal(e)
	}
	if got.Items == nil || got.Summary.Contracts != 0 || got.Summary.Jeonse.MedianDeposit != nil {
		t.Errorf("no contracts: %+v", got)
	}
}
