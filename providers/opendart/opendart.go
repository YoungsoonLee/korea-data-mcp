// Package opendart provides OpenDART (금융감독원 전자공시시스템) tools:
// company lookup, company profiles, disclosure search and key financials.
// API reference: https://opendart.fss.or.kr/guide/main.do
package opendart

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/httpx"
	"github.com/YoungsoonLee/mcp-hub/hub"
)

const (
	defaultBaseURL = "https://opendart.fss.or.kr/api"
	viewerURL      = "https://dart.fss.or.kr/dsaf001/main.do?rcpNo="
	envKey         = "OPENDART_API_KEY"
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
		ID:          "opendart",
		Title:       "OpenDART (전자공시)",
		Description: "Korean corporate disclosures, company profiles and key financial statements from DART.",
		RequiredEnv: []string{envKey},
		DocsURL:     "https://opendart.fss.or.kr",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	c := &client{
		base: p.BaseURL,
		key:  r.Env(envKey),
		// Disclosures change during the day, so cache briefly, and only
		// successful responses: DART reports errors inside HTTP 200.
		// opendart.fss.or.kr only negotiates RSA key exchange ciphers.
		api: httpx.New(httpx.WithLegacyRSATLS(), httpx.WithCache(10*time.Minute, 1000), httpx.WithCacheCheck(cacheable)),
		// corpCode.xml is a large zip; corpIndex keeps the parsed result instead.
		raw: httpx.New(httpx.WithLegacyRSATLS()),
		now: p.now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.corps = newCorpIndex(c.downloadCorps, 24*time.Hour)
	registerTools(r, c)
	return nil
}

type client struct {
	base  string
	key   string
	api   *httpx.Client
	raw   *httpx.Client
	corps *corpIndex
	now   func() time.Time
}

// apiStatus is embedded in every DART JSON response.
type apiStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s apiStatus) result() apiStatus { return s }

type response interface{ result() apiStatus }

func (c *client) getJSON(ctx context.Context, endpoint string, q url.Values, out response) error {
	q.Set("crtfc_key", c.key)
	if err := c.api.GetJSON(ctx, c.base+"/"+endpoint, q, nil, out); err != nil {
		return err
	}
	return statusError(out.result())
}

// cacheable reports whether a DART JSON body is a success or "no data" result.
func cacheable(body []byte) bool {
	var s apiStatus
	if json.Unmarshal(body, &s) != nil {
		return false
	}
	return s.Status == "000" || s.Status == "013"
}

// statusError maps DART status codes to hub error codes.
// https://opendart.fss.or.kr/guide/detail.do?apiGrpCd=DS001&apiId=2019001
func statusError(s apiStatus) error {
	var e *hub.Error
	switch s.Status {
	case "000":
		return nil
	case "013", "014":
		e = hub.Errorf(hub.CodeNotFound, "OpenDART: %s", s.Message)
	case "010", "011", "901":
		e = hub.Errorf(hub.CodeUnauthenticated, "OpenDART: %s", s.Message).
			WithHint("check that " + envKey + " is a valid, active OpenDART API key")
	case "012", "101":
		e = hub.Errorf(hub.CodePermission, "OpenDART: %s", s.Message)
	case "020":
		e = hub.Errorf(hub.CodeRateLimited, "OpenDART: %s", s.Message).
			WithHint("OpenDART allows about 20,000 calls per key per day; retry tomorrow")
	case "021", "100":
		e = hub.Errorf(hub.CodeInvalidArgument, "OpenDART: %s", s.Message)
	case "800":
		e = hub.Errorf(hub.CodeUnavailable, "OpenDART: %s", s.Message)
	default:
		e = hub.Errorf(hub.CodeUpstream, "OpenDART: %s", s.Message)
	}
	return e.WithUpstreamCode(s.Status)
}

var (
	corpCodePattern  = regexp.MustCompile(`^\d{8}$`)
	stockCodePattern = regexp.MustCompile(`^[0-9A-Z]{6}$`)
)

// resolveCorpCode accepts an 8-digit corp_code or a 6-character stock code.
func (c *client) resolveCorpCode(ctx context.Context, s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if corpCodePattern.MatchString(s) {
		return s, nil
	}
	if stockCodePattern.MatchString(s) {
		corp, err := c.corps.byStockCode(ctx, s)
		if err != nil {
			return "", err
		}
		if corp == nil {
			return "", hub.Errorf(hub.CodeNotFound, "no listed company with stock code %s", s).
				WithHint("use opendart_search_companies to find the corp_code")
		}
		return corp.Code, nil
	}
	return "", hub.Errorf(hub.CodeInvalidArgument, "corp_code must be an 8-digit corp_code or a 6-character stock code, got %q", s).
		WithHint("use opendart_search_companies to find the corp_code")
}

// market maps DART corp_cls to a market name.
func market(cls string) string {
	switch cls {
	case "Y":
		return "KOSPI"
	case "K":
		return "KOSDAQ"
	case "N":
		return "KONEX"
	case "E":
		return "OTHER"
	}
	return ""
}

func viewer(receiptNo string) string {
	if receiptNo == "" {
		return ""
	}
	return viewerURL + receiptNo
}
