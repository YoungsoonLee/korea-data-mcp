// Package ecos provides tools for the Bank of Korea's Economic Statistics
// System (한국은행 경제통계시스템, ecos.bok.or.kr): interest rates, exchange
// rates, prices, GDP, the balance of payments and thousands of other series.
//
// Quirks handled here, observed on the live API (docs/QUIRKS.md lists them all):
//
//   - The API key is part of the URL path, not the query string, so it is
//     masked in error messages (httpx.WithPathSecret).
//   - Errors are HTTP 200 with {"RESULT":{"CODE":"INFO-200",…}}; INFO-200
//     means no data, INFO-100 a bad key.
//   - A table can have up to four item dimensions. Asking for only the first
//     returns every combination of the others interleaved under the same
//     dates: monthly KRW/USD comes back as the period average and the
//     month-end rate, two rows per month. Rows are grouped into one series
//     per item combination.
//   - Monthly, quarterly and annual values of a rate can be end-of-period
//     (the base rate) or averages (exchange rates); the curated indicators
//     say which.
//   - Amounts are in the unit of the table (십억원, 백만달러, 억원); they are
//     converted to won or dollars.
package ecos

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/httpx"
	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

const (
	defaultBaseURL = "https://ecos.bok.or.kr/api"
	envKey         = "ECOS_API_KEY"
)

// kst is Korea Standard Time. A fixed zone avoids depending on tzdata.
var kst = time.FixedZone("KST", 9*60*60)

type Provider struct {
	BaseURL string           // overridden in tests
	now     func() time.Time // overridden in tests
}

func New() *Provider { return &Provider{BaseURL: defaultBaseURL} }

func (p *Provider) Info() hub.ProviderInfo {
	return hub.ProviderInfo{
		ID:    "ecos",
		Title: "한국은행 ECOS (ecos.bok.or.kr)",
		Description: "Korean economic statistics from the Bank of Korea: the base rate, market interest rates, exchange rates, " +
			"consumer prices, GDP, employment, the balance of payments, stock indices and thousands of other series. " +
			"Requires a free ECOS API key.",
		RequiredEnv: []string{envKey},
		DocsURL:     "https://ecos.bok.or.kr/api/",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	key := r.Env(envKey)
	c := &client{
		base: p.BaseURL,
		key:  key,
		// Most series update at most daily; errors arrive as HTTP 200, so
		// only normal responses are cached.
		http: httpx.New(httpx.WithPathSecret(key), httpx.WithCache(10*time.Minute, 500),
			httpx.WithCacheCheck(func(b []byte) bool { return apiError(b) == nil })),
		now: p.now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.tables = newTableIndex(c.downloadTables, 24*time.Hour)
	registerSeries(r, c)
	registerSearch(r, c)
	return nil
}

type client struct {
	base   string
	key    string
	http   *httpx.Client
	now    func() time.Time
	tables *tableIndex
}

// get calls an ECOS service. The URL is
// <base>/<service>/<key>/json/<lang>/<first row>/<last row>/<args…>, and the
// JSON is {"<service>": {"list_total_count": n, "row": [...]}}.
func (c *client) get(ctx context.Context, service, lang string, rows int, args []string, out any) (total int, err error) {
	parts := []string{c.base, service, url.PathEscape(c.key), "json", lang, "1", strconv.Itoa(rows)}
	for _, a := range args {
		parts = append(parts, url.PathEscape(a))
	}
	body, err := c.http.Get(ctx, strings.Join(parts, "/"), nil, nil)
	if err != nil {
		return 0, err
	}
	if err := apiError(body); err != nil {
		return 0, err
	}
	var doc map[string]struct {
		Total int             `json:"list_total_count"`
		Row   json.RawMessage `json:"row"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0, hub.Errorf(hub.CodeUpstream, "invalid JSON from ECOS").Wrap(err)
	}
	res, ok := doc[service]
	if !ok {
		return 0, hub.Errorf(hub.CodeUpstream, "ECOS returned no %s result", service)
	}
	if err := json.Unmarshal(res.Row, out); err != nil {
		return 0, hub.Errorf(hub.CodeUpstream, "invalid JSON from ECOS").Wrap(err)
	}
	return res.Total, nil
}

// apiError recognizes the {"RESULT":{"CODE":…,"MESSAGE":…}} document ECOS
// returns with HTTP 200.
func apiError(body []byte) error {
	var doc struct {
		Result *struct {
			Code    string `json:"CODE"`
			Message string `json:"MESSAGE"`
		} `json:"RESULT"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return hub.Errorf(hub.CodeUpstream, "ECOS returned a response that is not JSON")
	}
	if doc.Result == nil {
		return nil
	}
	msg := strings.Join(strings.Fields(strings.ReplaceAll(doc.Result.Message, `\n`, " ")), " ")
	switch code := doc.Result.Code; code {
	case "INFO-000":
		return nil
	case "INFO-200":
		return hub.Errorf(hub.CodeNotFound, "ECOS has no data for this request (%s)", code).
			WithHint("check the period and frequency against the series' start and end in ecos_search_series")
	case "INFO-100":
		return hub.Errorf(hub.CodeUnauthenticated, "ECOS rejected the API key (%s)", code).
			WithHint(envKey + " must be the key issued at ecos.bok.or.kr/api (인증키 신청)")
	case "ERROR-602":
		return hub.Errorf(hub.CodeRateLimited, "ECOS: %s (%s)", msg, code)
	default:
		if strings.HasPrefix(code, "ERROR-1") || strings.HasPrefix(code, "ERROR-3") {
			return hub.Errorf(hub.CodeInvalidArgument, "ECOS: %s (%s)", msg, code)
		}
		return hub.Errorf(hub.CodeUpstream, "ECOS: %s (%s)", msg, code)
	}
}
