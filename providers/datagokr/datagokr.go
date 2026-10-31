package datagokr

import (
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// kst is Korea Standard Time. A fixed zone avoids depending on tzdata.
var kst = time.FixedZone("KST", 9*60*60)

type Provider struct {
	BaseURL    string           // apis.data.go.kr; overridden in tests
	ODCloudURL string           // api.odcloud.kr; overridden in tests
	now        func() time.Time // overridden in tests
}

func New() *Provider { return &Provider{BaseURL: defaultBaseURL, ODCloudURL: defaultODCloudURL} }

func (p *Provider) Info() hub.ProviderInfo {
	return hub.ProviderInfo{
		ID:    "datagokr",
		Title: "공공데이터포털 (data.go.kr)",
		Description: "Korean government open data from the Public Data Portal: public holidays and special days, " +
			"the Financial Services Commission's corporation registry, National Tax Service business status, " +
			"apartment sale and rental prices, and daily stock prices. " +
			"Each API must be applied for (활용신청) separately on data.go.kr with the same service key.",
		RequiredEnv: []string{envKey},
		DocsURL:     "https://www.data.go.kr",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	c := newClient(p.BaseURL, r.Env(envKey))
	if p.ODCloudURL != "" {
		c.odcloudBase = p.ODCloudURL
	}
	c.now = p.now
	if c.now == nil {
		c.now = time.Now
	}
	registerHolidays(r, c)
	registerCorporations(r, c)
	registerBusiness(r, c)
	registerApartments(r, c)
	registerRents(r, c)
	registerStocks(r, c)
	return nil
}
