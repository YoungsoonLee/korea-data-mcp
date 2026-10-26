package datagokr

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 한국천문연구원_특일 정보 (SpcdeInfoService)
// https://www.data.go.kr/data/15012690/openapi.do
const spcdePath = "/B090041/openapi/service/SpcdeInfoService/"

// The service has no data before 2004 and publishes about two years ahead.
// Outside that range it answers "success, no results", so the range is
// checked here instead.
const firstHolidayYear = 2004

// holidayKinds maps the kind input to its operation. getHoliDeInfo
// (국경일, national holidays) is left out: it returns exactly the same days
// as getRestDeInfo, not just the five national holidays.
var holidayKinds = map[string]string{
	"public":      "getRestDeInfo",      // 공휴일
	"anniversary": "getAnniversaryInfo", // 기념일
	"solar_term":  "get24DivisionsInfo", // 24절기
	"seasonal":    "getSundryDayInfo",   // 잡절
}

func registerHolidays(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_list_holidays",
		Description: "List Korean public holidays (공휴일) for a year or month, including substitute holidays (대체공휴일), " +
			"Lunar New Year (설날) and Chuseok (추석) on their solar dates, and election days. " +
			"Use it to tell whether a date is a day off in Korea. Also lists anniversaries (기념일), " +
			"the 24 solar terms (24절기) and seasonal days (잡절) with kind. " +
			"Source: Korea Astronomy and Space Science Institute. Data covers 2004 through two years ahead; " +
			"holidays declared later (temporary holidays, by-elections) appear once announced.",
	}, c.listHolidays)
}

type listHolidaysInput struct {
	Year  int    `json:"year" jsonschema:"calendar year, e.g. 2026"`
	Month int    `json:"month,omitempty" jsonschema:"1-12; omit for the whole year"`
	Kind  string `json:"kind,omitempty" jsonschema:"one of: public (default, 공휴일: days off), anniversary (기념일), solar_term (24절기), seasonal (잡절)"`
	hub.PageParams
}

type Holiday struct {
	Date      string `json:"date" jsonschema:"YYYY-MM-DD"`
	Weekday   string `json:"weekday" jsonschema:"Monday ... Sunday"`
	Name      string `json:"name" jsonschema:"official Korean name"`
	NameEn    string `json:"name_en,omitempty" jsonschema:"English name, for public holidays"`
	IsHoliday bool   `json:"is_holiday" jsonschema:"true if it is a public day off (공휴일)"`
}

type spcdeItem struct {
	DateName  string `xml:"dateName"`
	IsHoliday string `xml:"isHoliday"`
	Locdate   string `xml:"locdate"`
}

func (c *client) listHolidays(ctx context.Context, in listHolidaysInput) (hub.List[Holiday], error) {
	page, size, err := in.Resolve(100, 100)
	if err != nil {
		return hub.List[Holiday]{}, err
	}
	if last := c.now().In(kst).Year() + 2; in.Year < firstHolidayYear || in.Year > last {
		return hub.List[Holiday]{}, hub.Errorf(hub.CodeInvalidArgument,
			"year must be between %d and %d (the data starts in %d and is published about two years ahead), got %d",
			firstHolidayYear, last, firstHolidayYear, in.Year)
	}
	if in.Month < 0 || in.Month > 12 {
		return hub.List[Holiday]{}, hub.Errorf(hub.CodeInvalidArgument, "month must be 1-12 or omitted, got %d", in.Month)
	}
	kind := in.Kind
	if kind == "" {
		kind = "public"
	}
	op, ok := holidayKinds[kind]
	if !ok {
		return hub.List[Holiday]{}, hub.Errorf(hub.CodeInvalidArgument, "unknown kind %q; use public, anniversary, solar_term or seasonal", kind)
	}

	q := url.Values{"solYear": {strconv.Itoa(in.Year)}}
	if in.Month > 0 {
		q.Set("solMonth", fmt.Sprintf("%02d", in.Month))
	}
	items, total, err := getList[spcdeItem](ctx, c, spcdePath+op, q, page, size)
	if err != nil {
		return hub.List[Holiday]{}, err
	}
	out := hub.List[Holiday]{Items: []Holiday{}, PageInfo: hub.NewPageInfo(page, size, total)}
	for _, it := range items {
		date := normalize.Date(it.Locdate)
		h := Holiday{
			Date:      date,
			Name:      strings.TrimSpace(it.DateName),
			IsHoliday: it.IsHoliday == "Y",
		}
		if t, err := time.Parse(time.DateOnly, date); err == nil {
			h.Weekday = t.Weekday().String()
		}
		if h.IsHoliday {
			h.NameEn = holidayNameEn(h.Name)
		}
		out.Items = append(out.Items, h)
	}
	return out, nil
}

var holidayNamesEn = map[string]string{
	"1월1일":   "New Year's Day",
	"신정":     "New Year's Day",
	"설날":     "Seollal (Lunar New Year)",
	"삼일절":    "Independence Movement Day",
	"노동절":    "Labor Day",
	"어린이날":   "Children's Day",
	"부처님오신날": "Buddha's Birthday",
	"석가탄신일":  "Buddha's Birthday",
	"현충일":    "Memorial Day",
	"제헌절":    "Constitution Day",
	"광복절":    "Liberation Day",
	"추석":     "Chuseok (Korean Thanksgiving)",
	"개천절":    "National Foundation Day",
	"한글날":    "Hangeul Day",
	"기독탄신일":  "Christmas Day",
	"임시공휴일":  "Temporary holiday",
}

// holidayNameEn translates a public holiday name, or returns "" if unknown.
func holidayNameEn(name string) string {
	if inner, ok := strings.CutPrefix(name, "대체공휴일("); ok {
		if en := holidayNameEn(strings.TrimSuffix(inner, ")")); en != "" {
			return "Substitute holiday for " + en
		}
		return "Substitute holiday"
	}
	if en, ok := holidayNamesEn[name]; ok {
		return en
	}
	if strings.Contains(name, "선거") {
		return "Election day"
	}
	return ""
}
