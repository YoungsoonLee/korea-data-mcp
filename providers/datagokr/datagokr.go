package datagokr

import (
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
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
		ID:    "datagokr",
		Title: "공공데이터포털 (data.go.kr)",
		Description: "Korean government open data from the Public Data Portal: public holidays and special days, " +
			"and the Financial Services Commission's corporation registry. " +
			"Each API must be applied for (활용신청) separately on data.go.kr with the same service key.",
		RequiredEnv: []string{envKey},
		DocsURL:     "https://www.data.go.kr",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	c := newClient(p.BaseURL, r.Env(envKey))
	c.now = p.now
	if c.now == nil {
		c.now = time.Now
	}
	registerHolidays(r, c)
	registerCorporations(r, c)
	return nil
}
