package ecos

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxRows         = 10000 // rows requested from ECOS in one call
	maxObservations = 1000  // observations returned across all series
)

func registerSeries(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "ecos_get_series",
		Description: "Get a time series from the Bank of Korea (ECOS). Use a named indicator (" + strings.Join(indicatorNames(), ", ") + ") " +
			"or any table's stat_code and item codes from ecos_search_series. " +
			"Periods are YYYY-MM-DD (daily), YYYY-MM, YYYY-Qn, YYYY-Hn or YYYY. Amounts are converted to KRW or USD; the unit says which. " +
			"Each series says whether a period's value is an average or the end-of-period level. " +
			"Index indicators (cpi, house_price_index) include the change from a year earlier. " +
			"The daily base_rate lists only the dates the rate changed.",
	}, c.getSeries)
}

type getSeriesInput struct {
	Indicator string   `json:"indicator,omitempty" jsonschema:"a named indicator such as base_rate, usd_krw, treasury_3y, cpi, gdp_growth. Leave empty when using stat_code"`
	StatCode  string   `json:"stat_code,omitempty" jsonschema:"ECOS table code such as 722Y001, from ecos_search_series"`
	ItemCodes []string `json:"item_codes,omitempty" jsonschema:"up to 4 item codes, one per item dimension of the table, in order. Omitted dimensions return every item"`
	Frequency string   `json:"frequency,omitempty" jsonschema:"daily, monthly, quarterly, semiannual or annual. Defaults to the indicator's usual frequency; required with stat_code"`
	From      string   `json:"from,omitempty" jsonschema:"first period, e.g. 2024-01-01, 2024-01, 2024-Q1, 2024. Default: about 30 days, 24 months, 12 quarters or 10 years back"`
	To        string   `json:"to,omitempty" jsonschema:"last period, same formats. Default: today"`
	YoY       bool     `json:"yoy,omitempty" jsonschema:"also return the percent change from a year earlier (not for daily data)"`
}

type SeriesResult struct {
	Indicator string   `json:"indicator,omitempty"`
	Label     string   `json:"label,omitempty"`
	StatCode  string   `json:"stat_code"`
	StatName  string   `json:"stat_name"`
	Frequency string   `json:"frequency"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Series    []Series `json:"series"`
	Note      string   `json:"note,omitempty"`
}

type Series struct {
	Items        []Item        `json:"items" jsonschema:"the item in each dimension of the table"`
	Unit         string        `json:"unit" jsonschema:"KRW and USD amounts are in won and dollars"`
	SourceUnit   string        `json:"source_unit,omitempty" jsonschema:"the unit ECOS reports, when values were converted"`
	Aggregation  string        `json:"aggregation,omitempty" jsonschema:"how a period's value is formed: period average, end of period or period total"`
	Observations []Observation `json:"observations"`
}

type Item struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Observation struct {
	Period     string   `json:"period"`
	Value      *float64 `json:"value"`
	YoYPercent *float64 `json:"yoy_percent,omitempty" jsonschema:"percent change from the same period a year earlier"`
	YoYPoints  *float64 `json:"yoy_points,omitempty" jsonschema:"for rates in percent: change in percentage points from the same period a year earlier"`
}

type searchRow struct {
	StatCode  string  `json:"STAT_CODE"`
	StatName  string  `json:"STAT_NAME"`
	ItemCode1 *string `json:"ITEM_CODE1"`
	ItemName1 *string `json:"ITEM_NAME1"`
	ItemCode2 *string `json:"ITEM_CODE2"`
	ItemName2 *string `json:"ITEM_NAME2"`
	ItemCode3 *string `json:"ITEM_CODE3"`
	ItemName3 *string `json:"ITEM_NAME3"`
	ItemCode4 *string `json:"ITEM_CODE4"`
	ItemName4 *string `json:"ITEM_NAME4"`
	Unit      *string `json:"UNIT_NAME"`
	Time      string  `json:"TIME"`
	Value     *string `json:"DATA_VALUE"`
}

func (c *client) getSeries(ctx context.Context, in getSeriesInput) (SeriesResult, error) {
	var (
		f   freq
		src source
		ind indicator
		err error
	)
	if in.Frequency != "" {
		if f, err = parseFreq(in.Frequency); err != nil {
			return SeriesResult{}, err
		}
	}
	switch {
	case in.Indicator != "" && in.StatCode != "":
		return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "give either indicator or stat_code, not both")
	case in.Indicator != "":
		var ok bool
		if ind, ok = findIndicator(in.Indicator); !ok {
			return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "unknown indicator %q", in.Indicator).
				WithHint("named indicators: " + strings.Join(indicatorNames(), ", ") + "; for anything else use ecos_search_series and stat_code")
		}
		f = cmp.Or(f, ind.def)
		if src, ok = ind.sources[f]; !ok {
			return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "%s is available as %s, not %s", ind.name, strings.Join(ind.freqs(), ", "), f)
		}
	case in.StatCode != "":
		if f == "" {
			return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "frequency is required with stat_code").
				WithHint("ecos_search_series with this stat_code lists each item's frequencies")
		}
		if len(in.ItemCodes) > 4 {
			return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "item_codes takes at most 4 codes, one per dimension")
		}
		src = source{stat: strings.ToUpper(strings.TrimSpace(in.StatCode)), items: in.ItemCodes}
	default:
		return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "indicator or stat_code is required").
			WithHint("named indicators: " + strings.Join(indicatorNames(), ", "))
	}
	yoy := in.YoY || ind.yoy
	if in.YoY && f == daily {
		return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "yoy is not available for daily data")
	}
	yoy = yoy && f != daily

	to := c.now().In(kst)
	if in.To != "" {
		if to, err = parsePeriod(in.To, true); err != nil {
			return SeriesResult{}, err
		}
	}
	from := defaultSpan(f, to)
	if ind.changes && f == daily {
		from = to.AddDate(-3, 0, 0) // rate changes are rare; a month would show one row
	}
	if in.From != "" {
		if from, err = parsePeriod(in.From, false); err != nil {
			return SeriesResult{}, err
		}
	}
	if from.After(to) {
		return SeriesResult{}, hub.Errorf(hub.CodeInvalidArgument, "from (%s) is after to (%s)", in.From, cmp.Or(in.To, "today"))
	}
	start := from
	if yoy {
		start = from.AddDate(-1, 0, 0)
	}

	var rows []searchRow
	args := append([]string{src.stat, string(f), ecosTime(start, f), ecosTime(to, f)}, src.items...)
	total, err := c.get(ctx, "StatisticSearch", "kr", maxRows, args, &rows)
	if err != nil {
		if he := hub.AsError(err); he.Code == hub.CodeNotFound && in.StatCode != "" {
			if ok, _ := c.tables.exists(ctx, src.stat); !ok {
				he.Message = "unknown stat_code " + src.stat
				he.Hint = "find table codes with ecos_search_series"
			}
		}
		return SeriesResult{}, err
	}
	if total > len(rows) {
		return SeriesResult{}, tooMany(total)
	}

	out := SeriesResult{
		Indicator: ind.name, Label: ind.label, StatCode: src.stat, Frequency: f.String(),
		From: outPeriod(ecosTime(from, f), f), To: outPeriod(ecosTime(to, f), f), Series: []Series{},
	}
	index := map[string]int{}
	n := 0
	for _, row := range rows {
		out.StatName = numbering.ReplaceAllString(strings.TrimSpace(row.StatName), "")
		key, items := itemsOf(row)
		i, ok := index[key]
		if !ok {
			i = len(out.Series)
			index[key] = i
			unit, _ := convertUnit(deref(row.Unit))
			s := Series{Items: items, Unit: unit, Aggregation: src.agg, Observations: []Observation{}}
			if unit != strings.TrimSpace(deref(row.Unit)) && !strings.HasPrefix(unit, "index") {
				s.SourceUnit = strings.TrimSpace(deref(row.Unit))
			}
			out.Series = append(out.Series, s)
		}
		_, exp := convertUnit(deref(row.Unit))
		out.Series[i].Observations = append(out.Series[i].Observations, Observation{Period: outPeriod(row.Time, f), Value: value(deref(row.Value), exp)})
	}
	first := outPeriod(ecosTime(from, f), f)
	for i := range out.Series {
		s := &out.Series[i]
		if yoy {
			addYoY(s.Observations, f, strings.HasPrefix(s.Unit, "%"))
		}
		s.Observations = dropBefore(s.Observations, first)
		if ind.changes && f == daily {
			s.Observations = changesOnly(s.Observations)
			out.Note = "Only the dates the rate changed are listed, starting with the rate in effect on " + out.From + "; each rate holds until the next date."
		}
		n += len(s.Observations)
	}
	if n > maxObservations {
		return SeriesResult{}, tooMany(n)
	}
	// Some combinations exist only at other frequencies: annual seasonally
	// adjusted unemployment comes back as rows of nulls.
	out.Series = slices.DeleteFunc(out.Series, func(s Series) bool {
		return !slices.ContainsFunc(s.Observations, func(o Observation) bool { return o.Value != nil })
	})
	if len(out.Series) > 1 {
		out.Note = cmp.Or(ind.note, "The table has more items than item_codes selected, so each combination is a separate series.")
	}
	return out, nil
}

func tooMany(n int) error {
	return hub.Errorf(hub.CodeInvalidArgument, "the request covers %d observations, more than %d", n, maxObservations).
		WithHint("narrow from/to, use a lower frequency, or give more item_codes")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// itemsOf returns a row's item combination as a key and as items.
func itemsOf(r searchRow) (string, []Item) {
	var items []Item
	var codes []string
	for _, p := range [][2]*string{{r.ItemCode1, r.ItemName1}, {r.ItemCode2, r.ItemName2}, {r.ItemCode3, r.ItemName3}, {r.ItemCode4, r.ItemName4}} {
		if p[0] == nil || *p[0] == "" {
			continue
		}
		items = append(items, Item{Code: *p[0], Name: strings.TrimSpace(deref(p[1]))})
		codes = append(codes, *p[0])
	}
	return strings.Join(codes, "/"), items
}

// addYoY sets the change from the period a year earlier, rounded to two
// decimals: in percent for levels, in percentage points for rates, since a
// rate going from 4% to 4.2% is up 0.2 points, not 5%.
func addYoY(obs []Observation, f freq, rate bool) {
	byPeriod := map[string]*float64{}
	for _, o := range obs {
		byPeriod[o.Period] = o.Value
	}
	for i, o := range obs {
		prev := byPeriod[yearEarlier(o.Period, f)]
		if o.Value == nil || prev == nil {
			continue
		}
		if rate {
			v := math.Round((*o.Value-*prev)*100) / 100
			obs[i].YoYPoints = &v
		} else if *prev != 0 {
			v := math.Round((*o.Value / *prev - 1)*10000) / 100
			obs[i].YoYPercent = &v
		}
	}
}

// dropBefore removes the extra year fetched for year-on-year changes.
// Output periods of one frequency sort as strings.
func dropBefore(obs []Observation, first string) []Observation {
	i := 0
	for i < len(obs) && obs[i].Period < first {
		i++
	}
	return obs[i:]
}

// changesOnly keeps the first observation and each one whose value differs
// from the previous one.
func changesOnly(obs []Observation) []Observation {
	var out []Observation
	for i, o := range obs {
		if i == 0 || !sameValue(o.Value, obs[i-1].Value) {
			out = append(out, o)
		}
	}
	return out
}

func sameValue(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
