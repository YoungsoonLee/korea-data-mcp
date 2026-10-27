package datagokr

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 국토교통부_아파트 매매 실거래가 자료 (RTMSDataSvcAptTrade)
// https://www.data.go.kr/data/15126469/openapi.do
//
// Quirks handled here:
//   - Prices are strings in units of 10,000 KRW ("84,500").
//   - Cancelled deals stay in the data, marked only by cdealType "O"; about
//     5% of a month's trades in Gangnam. They are excluded by default so
//     prices are not skewed.
//   - An invalid DEAL_YMD ("2025", "203001") is "success, no results".
//   - There is no filter by apartment or neighborhood, and no ordering, so a
//     month is fetched whole and filtered, sorted and paged here.
//   - District codes change when regions are reorganized, and the old codes
//     return nothing even for past months; see regions.go.
const aptTradePath = "/1613000/RTMSDataSvcAptTrade/getRTMSDataSvcAptTrade"

// Transactions are published from January 2006. A busy district has about
// 1,000 trades a month; maxAptPages bounds the fetch at 10,000.
var firstAptMonth = time.Date(2006, 1, 1, 0, 0, 0, 0, kst)

const (
	aptPageRows = 1000
	maxAptPages = 10
)

func registerApartments(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_list_apartment_trades",
		Description: "List apartment sale transactions (아파트 매매 실거래가) reported to the Ministry of Land, Infrastructure and Transport " +
			"for one district (시군구) and month, newest first, with a price summary. Prices are in KRW. " +
			"The region can be a name (강남구, 서울 중구, 성남시 분당구, 광주 북구) or a 5-digit code; a city with wards (수원시, 화성시) covers all of them. " +
			"Cancelled deals (계약 해제) are excluded unless include_cancelled is true. Filter by apartment name or neighborhood (법정동). " +
			"Deals must be reported within 30 days of the contract, so the latest one or two months are incomplete. Data starts in January 2006.",
	}, c.listApartmentTrades)
}

type listApartmentTradesInput struct {
	Region           string `json:"region" jsonschema:"district (시군구) name, e.g. 강남구, 서울 중구, 분당구, 화성시, or its 5-digit code, e.g. 11680"`
	YearMonth        string `json:"year_month" jsonschema:"contract month, YYYY-MM, e.g. 2025-09"`
	Apartment        string `json:"apartment,omitempty" jsonschema:"only apartments whose name contains this, e.g. 래미안 or 잠실엘스"`
	Neighborhood     string `json:"neighborhood,omitempty" jsonschema:"only this 법정동, e.g. 삼성동"`
	IncludeCancelled bool   `json:"include_cancelled,omitempty" jsonschema:"also list cancelled deals (계약 해제); they never count in the summary"`
	hub.PageParams
}

type ApartmentTrades struct {
	Region        string           `json:"region" jsonschema:"full district name, e.g. 서울특별시 강남구"`
	DistrictCodes []string         `json:"district_codes" jsonschema:"5-digit codes queried; several for a city with wards"`
	YearMonth     string           `json:"year_month"`
	Summary       TradeSummary     `json:"summary"`
	Items         []ApartmentTrade `json:"items"`
	PageInfo      hub.PageInfo     `json:"page_info"`
}

type TradeSummary struct {
	Trades          int    `json:"trades" jsonschema:"completed (not cancelled) deals matching the filters"`
	CancelledTrades int    `json:"cancelled_trades" jsonschema:"cancelled deals matching the filters, excluded from the figures below"`
	MedianPrice     *int64 `json:"median_price,omitempty"`
	MinPrice        *int64 `json:"min_price,omitempty"`
	MaxPrice        *int64 `json:"max_price,omitempty"`
}

type ApartmentTrade struct {
	Date            string   `json:"date" jsonschema:"contract date, YYYY-MM-DD"`
	Apartment       string   `json:"apartment"`
	District        string   `json:"district,omitempty" jsonschema:"시군구 within the province, e.g. 화성시 동탄구"`
	Neighborhood    string   `json:"neighborhood" jsonschema:"법정동"`
	LotNumber       string   `json:"lot_number,omitempty" jsonschema:"지번"`
	Building        string   `json:"building,omitempty" jsonschema:"동, when reported"`
	Floor           *int     `json:"floor,omitempty"`
	AreaM2          *float64 `json:"area_m2,omitempty" jsonschema:"exclusive area (전용면적), square meters"`
	Price           *int64   `json:"price" jsonschema:"KRW"`
	BuildYear       *int     `json:"build_year,omitempty"`
	DealType        string   `json:"deal_type,omitempty" jsonschema:"brokered (중개거래) or direct (직거래)"`
	Buyer           string   `json:"buyer,omitempty" jsonschema:"individual, corporation, public_institution or other"`
	Seller          string   `json:"seller,omitempty"`
	RegisteredDate  string   `json:"registered_date,omitempty" jsonschema:"ownership transfer registration date (등기일자)"`
	LandLease       bool     `json:"land_lease,omitempty" jsonschema:"land-lease apartment (토지임대부)"`
	Cancelled       bool     `json:"cancelled,omitempty"`
	CancelledDate   string   `json:"cancelled_date,omitempty"`
	AgentDistrictKo string   `json:"agent_district_ko,omitempty" jsonschema:"district of the broker's office"`
}

type aptTradeItem struct {
	AptNm            string `xml:"aptNm"`
	AptDong          string `xml:"aptDong"`
	UmdNm            string `xml:"umdNm"`
	Jibun            string `xml:"jibun"`
	Floor            string `xml:"floor"`
	ExcluUseAr       string `xml:"excluUseAr"`
	DealAmount       string `xml:"dealAmount"`
	DealYear         string `xml:"dealYear"`
	DealMonth        string `xml:"dealMonth"`
	DealDay          string `xml:"dealDay"`
	BuildYear        string `xml:"buildYear"`
	DealingGbn       string `xml:"dealingGbn"`
	BuyerGbn         string `xml:"buyerGbn"`
	SlerGbn          string `xml:"slerGbn"`
	RgstDate         string `xml:"rgstDate"`
	LandLeaseholdGbn string `xml:"landLeaseholdGbn"`
	CdealType        string `xml:"cdealType"`
	CdealDay         string `xml:"cdealDay"`
	EstateAgentSggNm string `xml:"estateAgentSggNm"`
	SggCd            string `xml:"sggCd"`
}

var (
	districtCodePattern = regexp.MustCompile(`^\d{5}$`)
	yearMonthPattern    = regexp.MustCompile(`^(\d{4})-?(\d{2})$`)
)

var dealTypes = map[string]string{"중개거래": "brokered", "직거래": "direct"}

var partyTypes = map[string]string{"개인": "individual", "법인": "corporation", "공공기관": "public_institution", "기타": "other"}

func (c *client) listApartmentTrades(ctx context.Context, in listApartmentTradesInput) (ApartmentTrades, error) {
	page, size, err := in.Resolve(50, 500)
	if err != nil {
		return ApartmentTrades{}, err
	}
	label, codes, err := resolveRegion(in.Region)
	if err != nil {
		return ApartmentTrades{}, err
	}
	month, err := c.parseYearMonth(in.YearMonth)
	if err != nil {
		return ApartmentTrades{}, err
	}

	var items []aptTradeItem
	for _, code := range codes {
		got, err := c.allAptTrades(ctx, code, month.Format("200601"))
		if err != nil {
			return ApartmentTrades{}, err
		}
		items = append(items, got...)
	}

	aptQuery := normalize.CompanyName(in.Apartment) // strips spaces and punctuation
	dong := strings.ReplaceAll(strings.TrimSpace(in.Neighborhood), " ", "")
	var trades []ApartmentTrade
	var prices []int64
	out := ApartmentTrades{Region: label, DistrictCodes: codes, YearMonth: month.Format("2006-01")}
	for _, it := range items {
		if aptQuery != "" && !strings.Contains(normalize.CompanyName(it.AptNm), aptQuery) {
			continue
		}
		if dong != "" && strings.ReplaceAll(text(it.UmdNm), " ", "") != dong {
			continue
		}
		t := toTrade(it)
		if t.Cancelled {
			out.Summary.CancelledTrades++
			if !in.IncludeCancelled {
				continue
			}
		} else {
			out.Summary.Trades++
			if t.Price != nil {
				prices = append(prices, *t.Price)
			}
		}
		trades = append(trades, t)
	}
	slices.SortStableFunc(trades, func(a, b ApartmentTrade) int {
		return cmp.Or(cmp.Compare(b.Date, a.Date), cmp.Compare(a.Apartment, b.Apartment))
	})
	if len(prices) > 0 {
		slices.Sort(prices)
		out.Summary.MinPrice, out.Summary.MaxPrice = &prices[0], &prices[len(prices)-1]
		median := prices[len(prices)/2]
		if len(prices)%2 == 0 {
			median = (prices[len(prices)/2-1] + median) / 2
		}
		out.Summary.MedianPrice = &median
	}

	out.PageInfo = hub.NewPageInfo(page, size, len(trades))
	out.Items = []ApartmentTrade{}
	for i := (page - 1) * size; i < len(trades) && i < page*size; i++ {
		out.Items = append(out.Items, trades[i])
	}
	return out, nil
}

// parseYearMonth accepts YYYY-MM or YYYYMM between January 2006 and the
// current month. The upstream answers anything else with zero results.
func (c *client) parseYearMonth(s string) (time.Time, error) {
	now := c.now().In(kst)
	last := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, kst)
	bad := hub.Errorf(hub.CodeInvalidArgument, "year_month must be YYYY-MM between 2006-01 and %s, got %q", last.Format("2006-01"), s)
	m := yearMonthPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}, bad
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	t := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, kst)
	if mo < 1 || mo > 12 || t.Before(firstAptMonth) || t.After(last) {
		return time.Time{}, bad
	}
	return t, nil
}

// allAptTrades fetches every trade of one district and month.
func (c *client) allAptTrades(ctx context.Context, region, ym string) ([]aptTradeItem, error) {
	var all []aptTradeItem
	for page := 1; page <= maxAptPages; page++ {
		q := url.Values{"LAWD_CD": {region}, "DEAL_YMD": {ym}}
		items, total, err := getList[aptTradeItem](ctx, c, aptTradePath, q, page, aptPageRows)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < aptPageRows || len(all) >= total {
			return all, nil
		}
	}
	return nil, hub.Errorf(hub.CodeUpstream, "more than %d trades in %s for %s", maxAptPages*aptPageRows, ym, region)
}

func toTrade(it aptTradeItem) ApartmentTrade {
	t := ApartmentTrade{
		Apartment:       text(it.AptNm),
		District:        districtName(text(it.SggCd)),
		Neighborhood:    text(it.UmdNm),
		LotNumber:       text(it.Jibun),
		Building:        text(it.AptDong),
		Floor:           intPtr(it.Floor),
		BuildYear:       intPtr(it.BuildYear),
		DealType:        dealTypes[text(it.DealingGbn)],
		Buyer:           cmp.Or(partyTypes[text(it.BuyerGbn)], text(it.BuyerGbn)),
		Seller:          cmp.Or(partyTypes[text(it.SlerGbn)], text(it.SlerGbn)),
		RegisteredDate:  date(it.RgstDate),
		LandLease:       text(it.LandLeaseholdGbn) == "Y",
		Cancelled:       text(it.CdealType) == "O",
		CancelledDate:   date(it.CdealDay),
		AgentDistrictKo: text(it.EstateAgentSggNm),
	}
	y, m, d := intPtr(it.DealYear), intPtr(it.DealMonth), intPtr(it.DealDay)
	if y != nil && m != nil && d != nil {
		t.Date = fmt.Sprintf("%04d-%02d-%02d", *y, *m, *d)
	}
	if p := normalize.Int(it.DealAmount); p != nil {
		won := *p * 10000 // reported in units of 10,000 KRW (만원)
		t.Price = &won
	}
	if a, err := strconv.ParseFloat(text(it.ExcluUseAr), 64); err == nil && a > 0 {
		t.AreaM2 = &a
	}
	return t
}

func intPtr(s string) *int {
	n, err := strconv.Atoi(text(s))
	if err != nil {
		return nil
	}
	return &n
}
