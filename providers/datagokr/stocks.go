package datagokr

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/normalize"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 금융위원회_주식시세정보 (GetStockSecuritiesInfoService_V2)
// https://www.data.go.kr/data/15094808/openapi.do
//
// Quirks handled here:
//   - The old path (/service/GetStockSecuritiesInfoService/getStockPriceInfo)
//     answers "key not registered" (30), not "no such service".
//   - srtnCd is silently ignored and returns every stock (4.5 million rows);
//     likeSrtnCd is a substring match, exact only for a full 6-character code.
//   - itmsNm matches the name on each day, so a renamed company's history is
//     cut at the rename (POSCO홀딩스: 1,127 of 1,671 days). Queries go by code.
//   - likeItmsNm is case- and space-sensitive (naver, 삼성 전자: no results),
//     so names are matched here against the latest day's full listing.
//   - endBasDt is exclusive; an invalid date is "success, no results".
//   - On a day without trades (e.g. a trading halt) open, high and low are 0
//     and the close is the previous price.
const stockPricePath = "/1160100/GetStockSecuritiesInfoService_V2/getStockPriceInfo_V2"

// Prices are published from 2020-01-02.
var firstStockDay = time.Date(2020, 1, 1, 0, 0, 0, 0, kst)

func registerStocks(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_get_stock_prices",
		Description: "Daily prices of a stock listed on KOSPI, KOSDAQ or KONEX, from the Financial Services Commission (주식시세정보), newest first: " +
			"close, change, open, high, low, volume, trading value, listed shares and market cap, in KRW. " +
			"The stock can be a name (삼성전자, naver, sk하이닉스), a 6-digit stock code (005930) or an ISIN; preferred shares have their own code (삼성전자우 005935). " +
			"History follows the code across renames. Data starts on 2020-01-02 and is updated after each trading day. " +
			"Days without trades (e.g. trading halts) have no open/high/low.",
	}, c.getStockPrices)
}

type getStockPricesInput struct {
	Stock string `json:"stock" jsonschema:"stock name (e.g. 삼성전자, NAVER) or 6-digit stock code (e.g. 005930)"`
	From  string `json:"from,omitempty" jsonschema:"first day, YYYY-MM-DD; default 30 days before to"`
	To    string `json:"to,omitempty" jsonschema:"last day, YYYY-MM-DD; default today"`
	hub.PageParams
}

type StockPrices struct {
	Stock    Stock        `json:"stock"`
	Items    []StockPrice `json:"items"`
	PageInfo hub.PageInfo `json:"page_info"`
}

type Stock struct {
	Code   string `json:"code" jsonschema:"6-digit stock code (단축코드)"`
	ISIN   string `json:"isin,omitempty"`
	Name   string `json:"name,omitempty" jsonschema:"name on the latest day returned"`
	Market string `json:"market,omitempty" jsonschema:"KOSPI, KOSDAQ or KONEX"`
}

type StockPrice struct {
	Date         string   `json:"date"`
	Name         string   `json:"name,omitempty" jsonschema:"name on that day, only when it differs from stock.name (after a rename)"`
	Close        *int64   `json:"close"`
	Change       *int64   `json:"change" jsonschema:"versus the previous close"`
	ChangeRate   *float64 `json:"change_rate" jsonschema:"percent"`
	Open         *int64   `json:"open,omitempty"`
	High         *int64   `json:"high,omitempty"`
	Low          *int64   `json:"low,omitempty"`
	Volume       *int64   `json:"volume"`
	TradingValue *int64   `json:"trading_value" jsonschema:"KRW"`
	ListedShares *int64   `json:"listed_shares,omitempty"`
	MarketCap    *int64   `json:"market_cap,omitempty" jsonschema:"KRW"`
	NoTrades     bool     `json:"no_trades,omitempty" jsonschema:"no trades that day, e.g. a trading halt; close is the last price"`
}

type stockPriceItem struct {
	BasDt      string `xml:"basDt"`
	SrtnCd     string `xml:"srtnCd"`
	IsinCd     string `xml:"isinCd"`
	ItmsNm     string `xml:"itmsNm"`
	MrktCtg    string `xml:"mrktCtg"`
	Clpr       string `xml:"clpr"`
	Vs         string `xml:"vs"`
	FltRt      string `xml:"fltRt"`
	Mkp        string `xml:"mkp"`
	Hipr       string `xml:"hipr"`
	Lopr       string `xml:"lopr"`
	Trqu       string `xml:"trqu"`
	TrPrc      string `xml:"trPrc"`
	LstgStCnt  string `xml:"lstgStCnt"`
	MrktTotAmt string `xml:"mrktTotAmt"`
}

// Stock codes are six characters starting with a digit; newer ones contain
// letters (0126Z0).
var stockCodePattern = regexp.MustCompile(`^[0-9][0-9A-Z]{5}$`)

func (c *client) getStockPrices(ctx context.Context, in getStockPricesInput) (StockPrices, error) {
	page, size, err := in.Resolve(30, 1000)
	if err != nil {
		return StockPrices{}, err
	}
	from, to, err := c.stockRange(in.From, in.To)
	if err != nil {
		return StockPrices{}, err
	}
	stock, err := c.resolveStock(ctx, in.Stock)
	if err != nil {
		return StockPrices{}, err
	}

	q := url.Values{
		"likeSrtnCd": {stock.Code}, // exact for a full code; srtnCd is ignored
		"beginBasDt": {from.Format("20060102")},
		"endBasDt":   {to.AddDate(0, 0, 1).Format("20060102")}, // exclusive
	}
	items, total, err := getList[stockPriceItem](ctx, c, stockPricePath, q, page, size)
	if err != nil {
		return StockPrices{}, err
	}
	if total == 0 && stock.Name == "" { // by code: is there such a stock at all?
		_, all, err := getList[stockPriceItem](ctx, c, stockPricePath, url.Values{"likeSrtnCd": {stock.Code}}, 1, 1)
		if err != nil {
			return StockPrices{}, err
		}
		if all == 0 {
			return StockPrices{}, hub.Errorf(hub.CodeNotFound, "no stock with code %s has prices since 2020", stock.Code).
				WithHint("pass the stock name instead, e.g. 삼성전자")
		}
	}

	out := StockPrices{Stock: stock, Items: []StockPrice{}, PageInfo: hub.NewPageInfo(page, size, total)}
	if len(items) > 0 && page == 1 {
		it := items[0]
		out.Stock = Stock{Code: stock.Code, ISIN: text(it.IsinCd), Name: text(it.ItmsNm), Market: text(it.MrktCtg)}
	}
	for _, it := range items {
		p := toStockPrice(it)
		if p.Name == out.Stock.Name {
			p.Name = ""
		}
		out.Items = append(out.Items, p)
	}
	return out, nil
}

// stockRange parses from and to (YYYY-MM-DD), defaulting to the 30 days up
// to today.
func (c *client) stockRange(fromS, toS string) (time.Time, time.Time, error) {
	now := c.now().In(kst)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, kst)
	parse := func(name, s string, def time.Time) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return def, nil
		}
		t, err := time.ParseInLocation(time.DateOnly, s, kst)
		if err != nil || t.Before(firstStockDay) || t.After(today) {
			return time.Time{}, hub.Errorf(hub.CodeInvalidArgument, "%s must be a date (YYYY-MM-DD) between 2020-01-02 and today (%s), got %q", name, today.Format(time.DateOnly), s)
		}
		return t, nil
	}
	to, err := parse("to", toS, today)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	def := to.AddDate(0, 0, -30)
	if def.Before(firstStockDay) {
		def = firstStockDay
	}
	from, err := parse("from", fromS, def)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, hub.Errorf(hub.CodeInvalidArgument, "from (%s) is after to (%s)", from.Format(time.DateOnly), to.Format(time.DateOnly))
	}
	return from, to, nil
}

// resolveStock turns a code or name into a stock. A name is matched,
// ignoring case and spaces, against the stocks listed on the latest trading
// day; an exact match wins, otherwise it must be the only partial match.
// An ISIN is matched the same way.
// Name is set only for a stock found by name.
func (c *client) resolveStock(ctx context.Context, s string) (Stock, error) {
	s = strings.TrimSpace(s)
	if code := strings.ToUpper(s); stockCodePattern.MatchString(code) {
		return Stock{Code: code}, nil
	}
	key := stockKey(s)
	if key == "" {
		return Stock{}, hub.Errorf(hub.CodeInvalidArgument, "stock is required: a name such as 삼성전자 or a 6-digit code such as 005930")
	}
	listed, err := c.listedStocks(ctx)
	if err != nil {
		return Stock{}, err
	}
	var partial []Stock
	for _, it := range listed {
		st := Stock{Code: text(it.SrtnCd), ISIN: text(it.IsinCd), Name: text(it.ItmsNm), Market: text(it.MrktCtg)}
		switch k := stockKey(st.Name); {
		case k == key, st.ISIN == key: // an ISIN does not always contain the code (삼성전자우: KR7005931001, 005935)
			return st, nil
		case strings.Contains(k, key):
			partial = append(partial, st)
		}
	}
	switch len(partial) {
	case 0:
		return Stock{}, hub.Errorf(hub.CodeNotFound, "no listed stock named %q", s).
			WithHint("use the name as listed on KRX (e.g. 현대차, not 현대자동차) or the 6-digit stock code; a delisted stock needs its code")
	case 1:
		return partial[0], nil
	}
	var opts []string
	for _, st := range partial[:min(len(partial), 10)] {
		opts = append(opts, fmt.Sprintf("%s (%s)", st.Name, st.Code))
	}
	more := ""
	if len(partial) > 10 {
		more = fmt.Sprintf(" and %d more", len(partial)-10)
	}
	return Stock{}, hub.Errorf(hub.CodeInvalidArgument, "%q matches %d stocks: %s%s", s, len(partial), strings.Join(opts, ", "), more).
		WithHint("pass the full name or the 6-digit code")
}

func stockKey(s string) string { return strings.ToUpper(strings.ReplaceAll(s, " ", "")) }

// listedStocks returns every stock on the latest trading day. Without
// filters the service lists the newest day first.
func (c *client) listedStocks(ctx context.Context) ([]stockPriceItem, error) {
	head, _, err := getList[stockPriceItem](ctx, c, stockPricePath, url.Values{}, 1, 1)
	if err != nil {
		return nil, err
	}
	if len(head) == 0 {
		return nil, hub.Errorf(hub.CodeUpstream, "data.go.kr returned no stock prices")
	}
	// About 2,900 stocks; one page avoids a second round trip.
	items, _, err := getList[stockPriceItem](ctx, c, stockPricePath, url.Values{"basDt": {text(head[0].BasDt)}}, 1, 5000)
	return items, err
}

func toStockPrice(it stockPriceItem) StockPrice {
	p := StockPrice{
		Date:         date(it.BasDt),
		Name:         text(it.ItmsNm),
		Close:        normalize.Int(it.Clpr),
		Change:       normalize.Int(it.Vs),
		Volume:       normalize.Int(it.Trqu),
		TradingValue: normalize.Int(it.TrPrc),
		ListedShares: positive(it.LstgStCnt),
		MarketCap:    positive(it.MrktTotAmt),
	}
	if r, err := strconv.ParseFloat(text(it.FltRt), 64); err == nil {
		p.ChangeRate = &r
	}
	if p.Volume != nil && *p.Volume == 0 {
		p.NoTrades = true // open, high and low are reported as 0
	} else {
		p.Open, p.High, p.Low = positive(it.Mkp), positive(it.Hipr), positive(it.Lopr)
	}
	return p
}
