package ecos

import (
	"slices"
	"strings"
)

// source is where an indicator's data lives for one frequency.
type source struct {
	stat  string
	items []string
	agg   string // how a period's value is formed, e.g. "period average"
}

// indicator is a curated series with a stable name, so agents do not have to
// find stat and item codes for the questions asked most.
type indicator struct {
	name    string
	label   string // English
	labelKo string
	def     freq
	sources map[freq]source
	changes bool // daily data is reported only on the days the value changed
	yoy     bool // index levels: add the change from a year earlier
	note    string
}

const (
	endOfPeriod = "end of period"
	average     = "period average"
)

var indicators = []indicator{
	{
		name: "base_rate", label: "Bank of Korea base rate", labelKo: "한국은행 기준금리", def: daily, changes: true,
		sources: map[freq]source{
			daily:     {"722Y001", []string{"0101000"}, ""},
			monthly:   {"722Y001", []string{"0101000"}, endOfPeriod},
			quarterly: {"722Y001", []string{"0101000"}, endOfPeriod},
			annual:    {"722Y001", []string{"0101000"}, endOfPeriod},
		},
	},
	fx("usd_krw", "KRW per US dollar (base rate of exchange)", "원/미국달러(매매기준율)", "0000001"),
	fx("jpy_krw", "KRW per 100 Japanese yen", "원/일본엔(100엔)", "0000002"),
	fx("eur_krw", "KRW per euro", "원/유로", "0000003"),
	fx("cny_krw", "KRW per Chinese yuan", "원/위안(매매기준율)", "0000053"),
	// The daily and the monthly tables use different item codes.
	marketRate("treasury_3y", "Korea Treasury Bond yield, 3 years", "국고채(3년)", "010200000", "5020000"),
	marketRate("treasury_10y", "Korea Treasury Bond yield, 10 years", "국고채(10년)", "010210000", "5050000"),
	marketRate("cd_91d", "CD rate, 91 days", "CD(91일)", "010502000", "2010000"),
	marketRate("corporate_bond_3y", "Corporate bond yield, 3 years, AA-", "회사채(3년, AA-)", "010300000", "7020000"),
	{
		name: "kospi", label: "KOSPI index (close)", labelKo: "KOSPI지수", def: daily,
		sources: map[freq]source{daily: {"802Y001", []string{"0001000"}, ""}},
	},
	{
		name: "kosdaq", label: "KOSDAQ index (close)", labelKo: "KOSDAQ지수", def: daily,
		sources: map[freq]source{daily: {"802Y001", []string{"0089000"}, ""}},
	},
	{
		name: "cpi", label: "Consumer price index, all items", labelKo: "소비자물가지수(총지수)", def: monthly, yoy: true,
		sources: map[freq]source{
			monthly:   {"901Y009", []string{"0"}, ""},
			quarterly: {"901Y009", []string{"0"}, average},
			annual:    {"901Y009", []string{"0"}, average},
		},
	},
	{
		name: "gdp_growth", label: "Real GDP growth, seasonally adjusted, quarter on quarter", labelKo: "국내총생산(실질, 계절조정, 전기비)", def: quarterly,
		sources: map[freq]source{quarterly: {"200Y102", []string{"10111"}, ""}},
	},
	{
		name: "gdp_growth_yoy", label: "Real GDP growth, not seasonally adjusted, year on year", labelKo: "국내총생산(실질, 원계열, 전년동기비)", def: quarterly,
		sources: map[freq]source{quarterly: {"200Y102", []string{"10211"}, ""}},
	},
	{
		name: "gdp", label: "Nominal GDP", labelKo: "국내총생산(명목, 원화표시)", def: annual,
		sources: map[freq]source{annual: {"200Y101", []string{"10101"}, ""}},
	},
	{
		name: "gdp_per_capita_usd", label: "GDP per capita, nominal, US dollars", labelKo: "1인당 국내총생산(명목, 달러표시)", def: annual,
		sources: map[freq]source{annual: {"200Y101", []string{"1010701"}, ""}},
	},
	{
		name: "unemployment_rate", label: "Unemployment rate", labelKo: "실업률", def: monthly,
		note: "Two series: 원계열 (not seasonally adjusted; the headline figure in Statistics Korea releases) and 계절조정 (seasonally adjusted).",
		sources: map[freq]source{
			monthly:   {"901Y027", []string{"I61BC"}, ""},
			quarterly: {"901Y027", []string{"I61BC"}, ""},
			annual:    {"901Y027", []string{"I61BC"}, ""},
		},
	},
	{
		name: "employment_rate", label: "Employment rate, ages 15 and over", labelKo: "고용률", def: monthly,
		note: "Two series: 원계열 (not seasonally adjusted; the headline figure in Statistics Korea releases) and 계절조정 (seasonally adjusted).",
		sources: map[freq]source{
			monthly:   {"901Y027", []string{"I61E"}, ""},
			quarterly: {"901Y027", []string{"I61E"}, ""},
			annual:    {"901Y027", []string{"I61E"}, ""},
		},
	},
	{
		name: "current_account", label: "Current account balance", labelKo: "경상수지", def: monthly,
		sources: map[freq]source{
			monthly:   {"301Y013", []string{"000000"}, ""},
			quarterly: {"301Y013", []string{"000000"}, "period total"},
			annual:    {"301Y013", []string{"000000"}, "period total"},
		},
	},
	{
		name: "house_price_index", label: "KB housing price index, nationwide, all types", labelKo: "주택매매가격지수(KB) 총지수", def: monthly, yoy: true,
		sources: map[freq]source{monthly: {"901Y062", []string{"P63A"}, ""}},
	},
}

func fx(name, label, labelKo, item string) indicator {
	return indicator{
		name: name, label: label, labelKo: labelKo, def: daily,
		sources: map[freq]source{
			daily:      {"731Y001", []string{item}, ""},
			monthly:    {"731Y004", []string{item, "0000100"}, average},
			quarterly:  {"731Y004", []string{item, "0000100"}, average},
			semiannual: {"731Y004", []string{item, "0000100"}, average},
			annual:     {"731Y004", []string{item, "0000100"}, average},
		},
	}
}

func marketRate(name, label, labelKo, dailyItem, monthlyItem string) indicator {
	return indicator{
		name: name, label: label, labelKo: labelKo, def: daily,
		sources: map[freq]source{
			daily:     {"817Y002", []string{dailyItem}, ""},
			monthly:   {"721Y001", []string{monthlyItem}, average},
			quarterly: {"721Y001", []string{monthlyItem}, average},
			annual:    {"721Y001", []string{monthlyItem}, average},
		},
	}
}

func findIndicator(name string) (indicator, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	i := slices.IndexFunc(indicators, func(ind indicator) bool { return ind.name == name })
	if i < 0 {
		return indicator{}, false
	}
	return indicators[i], true
}

func indicatorNames() []string {
	var names []string
	for _, ind := range indicators {
		names = append(names, ind.name)
	}
	return names
}

// freqs lists an indicator's frequencies, shortest first.
func (ind indicator) freqs() []string {
	var out []string
	for _, f := range []freq{daily, monthly, quarterly, semiannual, annual} {
		if _, ok := ind.sources[f]; ok {
			out = append(out, f.String())
		}
	}
	return out
}
