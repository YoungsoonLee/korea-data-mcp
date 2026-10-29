// Package lawgokr provides tools for Korean statutes from the National Law
// Information Center's open API (국가법령정보 공동활용, open.law.go.kr).
//
// Quirks handled here, observed on the live API (docs/QUIRKS.md lists them all):
//
//   - The "current law" (target=law) text includes amendments that are not
//     in effect yet, labeled with today's effective date: 근로기준법 shows
//     제44조의4, which takes effect on 2027-01-01. Text is therefore always
//     fetched by effective date (target=eflaw) for the version in force.
//   - efYd only accepts a version's exact effective date; any other date
//     returns an empty document. The version is picked from the law's
//     history (LID) instead.
//   - Errors are HTTP 200 with a different document per case: <Response>
//     for a bad OC or unregistered IP (indistinguishable), <Law> with plain
//     text for an unknown law.
//   - Every link in a response contains the OC. Links are never passed on;
//     tools return public law.go.kr URLs instead.
//   - JSON returns a single result as an object instead of an array, so
//     every request asks for XML.
package lawgokr

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/httpx"
	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

const (
	defaultBaseURL = "https://www.law.go.kr/DRF"
	publicURL      = "https://www.law.go.kr/법령/"
	envKey         = "LAWGOKR_OC"
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
		ID:    "lawgokr",
		Title: "국가법령정보 (law.go.kr)",
		Description: "Korean statutes (법령) from the Ministry of Government Legislation: search by name, abbreviation or text, " +
			"read the text in force on any date, article by article, and search and read court rulings (판례). " +
			"Requires an OC value from open.law.go.kr and this machine's public IP registered there.",
		RequiredEnv: []string{envKey},
		DocsURL:     "https://open.law.go.kr",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	c := &client{
		base: p.BaseURL,
		oc:   r.Env(envKey),
		// Statutes change at most daily; errors arrive as HTTP 200, so only
		// normal responses are cached.
		http: httpx.New(httpx.WithCache(10*time.Minute, 500), httpx.WithCacheCheck(func(b []byte) bool { return apiError(b) == nil })),
		now:  p.now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	registerLaws(r, c)
	registerPrecedents(r, c)
	return nil
}

type client struct {
	base string
	oc   string
	http *httpx.Client
	now  func() time.Time
}

// get calls lawSearch or lawService and decodes the XML into out.
func (c *client) get(ctx context.Context, endpoint string, q url.Values, out any) error {
	q.Set("OC", c.oc)
	q.Set("type", "XML")
	body, err := c.http.Get(ctx, c.base+"/"+endpoint+".do", q, nil)
	if err != nil {
		return err
	}
	if err := apiError(body); err != nil {
		return err
	}
	if err := xml.Unmarshal(body, out); err != nil {
		return hub.Errorf(hub.CodeUpstream, "invalid XML from law.go.kr").Wrap(err)
	}
	return nil
}

// apiError recognizes the error documents law.go.kr returns with HTTP 200.
func apiError(body []byte) error {
	root := rootElement(body)
	switch root {
	case "Response": // <Response><result>…</result><msg>…</msg></Response>
		var doc struct {
			Result string `xml:"result"`
			Msg    string `xml:"msg"`
		}
		xml.Unmarshal(body, &doc)
		if strings.Contains(doc.Result, "사용자 정보 검증") {
			return hub.Errorf(hub.CodeUnauthenticated, "law.go.kr rejected the OC: %s", doc.Result).
				WithHint(envKey + " must be the API key (OC) shown under 마이페이지 > API인증키관리 on open.law.go.kr, " +
					"and this machine's public IP must be registered there; law.go.kr reports both problems the same way")
		}
		return hub.Errorf(hub.CodeUpstream, "law.go.kr: %s %s", doc.Result, doc.Msg)
	case "Law": // <Law>일치하는 법령이 없습니다. …</Law>
		var msg string
		xml.Unmarshal(body, &msg)
		return hub.Errorf(hub.CodeNotFound, "law.go.kr: %s", strings.Join(strings.Fields(msg), " "))
	case "":
		return hub.Errorf(hub.CodeUpstream, "law.go.kr returned a response that is not XML")
	}
	return nil
}

func rootElement(body []byte) string {
	d := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

// date turns YYYYMMDD into YYYY-MM-DD; anything else becomes "".
func date(s string) string {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return ""
	}
	if _, err := strconv.Atoi(s); err != nil {
		return ""
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:]
}

// lawURL is the public page of a law, which never contains the OC.
func lawURL(name string, article string) string {
	u := publicURL + strings.ReplaceAll(name, " ", "")
	if article != "" {
		u += "/" + article
	}
	return u
}
