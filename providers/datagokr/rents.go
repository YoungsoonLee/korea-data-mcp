package datagokr

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 국토교통부_아파트 전월세 실거래가 자료 (RTMSDataSvcAptRent)
// https://www.data.go.kr/data/15126474/openapi.do
//
// Quirks handled here:
//   - Deposits and rents are strings in units of 10,000 KRW ("30,000").
//   - Jeonse (전세) and monthly rent (월세) are in one list; the only
//     difference is monthlyRent "0".
//   - Renewals (갱신) are about half of a month's contracts and are mixed with
//     new ones. A renewal under the tenant's right (계약갱신요구권) is capped
//     at +5%, so mixing them pulls figures far from the market: in Jongno in
//     2025-09 the median monthly rent was ₩960,000 for new contracts and
//     ₩215,000 for renewals. Figures are split, and contract filters them.
//   - The contract type is reported only since June 2021, and not always
//     after that.
//   - The contract term is "25.10~27.10".
//   - There is no cancellation flag, unlike sales.
//   - The district code and month quirks of sales apply; see apartments.go.
const aptRentPath = "/1613000/RTMSDataSvcAptRent/getRTMSDataSvcAptRent"

// Rentals are published from January 2011.
var firstRentMonth = time.Date(2011, 1, 1, 0, 0, 0, 0, kst)

func registerRents(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_list_apartment_rents",
		Description: "List apartment rental contracts (아파트 전월세 실거래가), jeonse (전세) and monthly rent (월세), reported to the Ministry of Land, Infrastructure and Transport " +
			"for one district (시군구) and month, newest first, with deposit and rent summaries. Amounts are in KRW. " +
			"The region can be a name (강남구, 서울 중구, 성남시 분당구, 광주 북구) or a 5-digit code; a city with wards (수원시, 화성시) covers all of them. " +
			"About half of the contracts are renewals (갱신), often capped at +5%, so set contract to new for market prices. " +
			"Filter by rent type, apartment name or neighborhood (법정동). " +
			"Contracts must be reported within 30 days, so the latest one or two months are incomplete. Data starts in January 2011; new and renewal are reported since June 2021.",
	}, c.listApartmentRents)
}

type listApartmentRentsInput struct {
	Region       string `json:"region" jsonschema:"district (시군구) name, e.g. 강남구, 서울 중구, 분당구, 화성시, or its 5-digit code, e.g. 11680"`
	YearMonth    string `json:"year_month" jsonschema:"contract month, YYYY-MM, e.g. 2025-09"`
	RentType     string `json:"rent_type,omitempty" jsonschema:"jeonse (전세, no monthly rent) or monthly (월세, including 반전세); both when omitted"`
	Contract     string `json:"contract,omitempty" jsonschema:"new (신규) or renewal (갱신); both when omitted, which also includes contracts of unknown type"`
	Apartment    string `json:"apartment,omitempty" jsonschema:"only apartments whose name contains this, e.g. 래미안 or 잠실엘스"`
	Neighborhood string `json:"neighborhood,omitempty" jsonschema:"only this 법정동, e.g. 삼성동"`
	hub.PageParams
}

type ApartmentRents struct {
	Region        string          `json:"region" jsonschema:"full district name, e.g. 서울특별시 강남구"`
	DistrictCodes []string        `json:"district_codes" jsonschema:"5-digit codes queried; several for a city with wards"`
	YearMonth     string          `json:"year_month"`
	Summary       RentSummary     `json:"summary"`
	Items         []ApartmentRent `json:"items"`
	PageInfo      hub.PageInfo    `json:"page_info"`
}

type RentSummary struct {
	Contracts        int       `json:"contracts" jsonschema:"contracts matching the filters"`
	New              int       `json:"new" jsonschema:"new contracts (신규)"`
	Renewals         int       `json:"renewals" jsonschema:"renewals (갱신); the rest are of unknown type"`
	RenewalRightUsed int       `json:"renewal_right_used" jsonschema:"renewals under the tenant's right (계약갱신요구권), capped at +5%"`
	Jeonse           RentStats `json:"jeonse" jsonschema:"전세 contracts"`
	Monthly          RentStats `json:"monthly" jsonschema:"월세 contracts"`
}

type RentStats struct {
	Contracts         int    `json:"contracts"`
	MedianDeposit     *int64 `json:"median_deposit,omitempty"`
	MinDeposit        *int64 `json:"min_deposit,omitempty"`
	MaxDeposit        *int64 `json:"max_deposit,omitempty"`
	MedianMonthlyRent *int64 `json:"median_monthly_rent,omitempty"`
}

type ApartmentRent struct {
	Date                string   `json:"date" jsonschema:"contract date, YYYY-MM-DD"`
	Apartment           string   `json:"apartment"`
	District            string   `json:"district,omitempty" jsonschema:"시군구 within the province, e.g. 화성시 동탄구"`
	Neighborhood        string   `json:"neighborhood" jsonschema:"법정동"`
	LotNumber           string   `json:"lot_number,omitempty" jsonschema:"지번"`
	Floor               *int     `json:"floor,omitempty"`
	AreaM2              *float64 `json:"area_m2,omitempty" jsonschema:"exclusive area (전용면적), square meters"`
	BuildYear           *int     `json:"build_year,omitempty"`
	RentType            string   `json:"rent_type" jsonschema:"jeonse or monthly"`
	Deposit             *int64   `json:"deposit" jsonschema:"KRW"`
	MonthlyRent         *int64   `json:"monthly_rent" jsonschema:"KRW; 0 for jeonse"`
	Contract            string   `json:"contract,omitempty" jsonschema:"new or renewal, when reported"`
	RenewalRightUsed    bool     `json:"renewal_right_used,omitempty" jsonschema:"renewal under the tenant's right (계약갱신요구권)"`
	TermStart           string   `json:"term_start,omitempty" jsonschema:"YYYY-MM"`
	TermEnd             string   `json:"term_end,omitempty" jsonschema:"YYYY-MM"`
	PreviousDeposit     *int64   `json:"previous_deposit,omitempty" jsonschema:"KRW, for a renewal"`
	PreviousMonthlyRent *int64   `json:"previous_monthly_rent,omitempty" jsonschema:"KRW, for a renewal"`
}

type aptRentItem struct {
	AptNm          string `xml:"aptNm"`
	UmdNm          string `xml:"umdNm"`
	Jibun          string `xml:"jibun"`
	Floor          string `xml:"floor"`
	ExcluUseAr     string `xml:"excluUseAr"`
	Deposit        string `xml:"deposit"`
	MonthlyRent    string `xml:"monthlyRent"`
	DealYear       string `xml:"dealYear"`
	DealMonth      string `xml:"dealMonth"`
	DealDay        string `xml:"dealDay"`
	BuildYear      string `xml:"buildYear"`
	ContractType   string `xml:"contractType"`
	ContractTerm   string `xml:"contractTerm"`
	UseRRRight     string `xml:"useRRRight"`
	PreDeposit     string `xml:"preDeposit"`
	PreMonthlyRent string `xml:"preMonthlyRent"`
	SggCd          string `xml:"sggCd"`
}

var (
	rentTypes      = map[string]string{"jeonse": "jeonse", "전세": "jeonse", "monthly": "monthly", "월세": "monthly"}
	contractKinds  = map[string]string{"new": "new", "신규": "new", "renewal": "renewal", "갱신": "renewal"}
	contractTermRe = regexp.MustCompile(`^(\d{2})\.(\d{2})~(\d{2})\.(\d{2})$`)
)

func (c *client) listApartmentRents(ctx context.Context, in listApartmentRentsInput) (ApartmentRents, error) {
	page, size, err := in.Resolve(50, 500)
	if err != nil {
		return ApartmentRents{}, err
	}
	rentType, ok := rentTypes[strings.ToLower(strings.TrimSpace(in.RentType))]
	if !ok && strings.TrimSpace(in.RentType) != "" {
		return ApartmentRents{}, hub.Errorf(hub.CodeInvalidArgument, "rent_type must be jeonse or monthly, got %q", in.RentType)
	}
	contract, ok := contractKinds[strings.ToLower(strings.TrimSpace(in.Contract))]
	if !ok && strings.TrimSpace(in.Contract) != "" {
		return ApartmentRents{}, hub.Errorf(hub.CodeInvalidArgument, "contract must be new or renewal, got %q", in.Contract)
	}
	label, codes, err := resolveRegion(in.Region)
	if err != nil {
		return ApartmentRents{}, err
	}
	month, err := c.parseYearMonth(in.YearMonth, firstRentMonth)
	if err != nil {
		return ApartmentRents{}, err
	}
	items, err := allAptMonth[aptRentItem](ctx, c, aptRentPath, codes, month)
	if err != nil {
		return ApartmentRents{}, err
	}

	aptQuery := normalize.CompanyName(in.Apartment) // strips spaces and punctuation
	dong := strings.ReplaceAll(strings.TrimSpace(in.Neighborhood), " ", "")
	var rents []ApartmentRent
	var jeonseDeposits, monthlyDeposits, monthlyRents []int64
	out := ApartmentRents{Region: label, DistrictCodes: codes, YearMonth: month.Format("2006-01")}
	for _, it := range items {
		if aptQuery != "" && !strings.Contains(normalize.CompanyName(it.AptNm), aptQuery) {
			continue
		}
		if dong != "" && strings.ReplaceAll(text(it.UmdNm), " ", "") != dong {
			continue
		}
		r := toRent(it)
		if (rentType != "" && r.RentType != rentType) || (contract != "" && r.Contract != contract) {
			continue
		}
		s := &out.Summary
		s.Contracts++
		switch r.Contract {
		case "new":
			s.New++
		case "renewal":
			s.Renewals++
		}
		if r.RenewalRightUsed {
			s.RenewalRightUsed++
		}
		switch r.RentType {
		case "jeonse":
			s.Jeonse.Contracts++
			if r.Deposit != nil {
				jeonseDeposits = append(jeonseDeposits, *r.Deposit)
			}
		case "monthly":
			s.Monthly.Contracts++
			if r.Deposit != nil {
				monthlyDeposits = append(monthlyDeposits, *r.Deposit)
			}
			monthlyRents = append(monthlyRents, *r.MonthlyRent)
		}
		rents = append(rents, r)
	}
	slices.SortStableFunc(rents, func(a, b ApartmentRent) int {
		return cmp.Or(cmp.Compare(b.Date, a.Date), cmp.Compare(a.Apartment, b.Apartment))
	})
	j, m := &out.Summary.Jeonse, &out.Summary.Monthly
	j.MinDeposit, j.MedianDeposit, j.MaxDeposit = spread(jeonseDeposits)
	m.MinDeposit, m.MedianDeposit, m.MaxDeposit = spread(monthlyDeposits)
	_, m.MedianMonthlyRent, _ = spread(monthlyRents)

	out.PageInfo = hub.NewPageInfo(page, size, len(rents))
	out.Items = []ApartmentRent{}
	for i := (page - 1) * size; i < len(rents) && i < page*size; i++ {
		out.Items = append(out.Items, rents[i])
	}
	return out, nil
}

func toRent(it aptRentItem) ApartmentRent {
	r := ApartmentRent{
		Apartment:           text(it.AptNm),
		District:            districtName(text(it.SggCd)),
		Neighborhood:        text(it.UmdNm),
		LotNumber:           text(it.Jibun),
		Floor:               intPtr(it.Floor),
		BuildYear:           intPtr(it.BuildYear),
		Deposit:             manwon(it.Deposit),
		MonthlyRent:         manwon(it.MonthlyRent),
		Contract:            contractKinds[text(it.ContractType)],
		RenewalRightUsed:    text(it.UseRRRight) == "사용",
		PreviousDeposit:     manwon(it.PreDeposit),
		PreviousMonthlyRent: manwon(it.PreMonthlyRent),
	}
	if r.MonthlyRent != nil {
		r.RentType = "monthly"
		if *r.MonthlyRent == 0 {
			r.RentType = "jeonse"
		}
	}
	y, m, d := intPtr(it.DealYear), intPtr(it.DealMonth), intPtr(it.DealDay)
	if y != nil && m != nil && d != nil {
		r.Date = fmt.Sprintf("%04d-%02d-%02d", *y, *m, *d)
	}
	if a, err := strconv.ParseFloat(text(it.ExcluUseAr), 64); err == nil && a > 0 {
		r.AreaM2 = &a
	}
	if t := contractTermRe.FindStringSubmatch(text(it.ContractTerm)); t != nil {
		r.TermStart, r.TermEnd = "20"+t[1]+"-"+t[2], "20"+t[3]+"-"+t[4]
	}
	return r
}

// manwon converts an amount in units of 10,000 KRW ("30,000") to KRW.
func manwon(s string) *int64 {
	p := normalize.Int(s)
	if p == nil {
		return nil
	}
	won := *p * 10000
	return &won
}
