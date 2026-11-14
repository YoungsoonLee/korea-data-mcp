package opendart

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/httpx"
	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
)

// corp is one entry of DART's company list (corpCode.xml).
type corp struct {
	Code       string
	Name       string
	EngName    string
	StockCode  string
	ModifyDate string
	norm       string // normalized Name for matching
	normEng    string
}

// corpIndex holds DART's full company list (~100k entries), downloaded on
// first use and refreshed after ttl. Every other DART API needs corp_code,
// and DART has no name-search endpoint, so this index is the lookup.
type corpIndex struct {
	mu       sync.Mutex
	corps    []corp
	byStock  map[string]int
	byCode   map[string]int
	byKey    map[string][]int // normalize.CompanyKey of Name
	loadedAt time.Time
	ttl      time.Duration
	load     func(context.Context) ([]corp, error)
	now      func() time.Time
}

func newCorpIndex(load func(context.Context) ([]corp, error), ttl time.Duration) *corpIndex {
	return &corpIndex{load: load, ttl: ttl, now: time.Now}
}

func (ix *corpIndex) get(ctx context.Context) ([]corp, map[string]int, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.getLocked(ctx)
}

func (ix *corpIndex) getLocked(ctx context.Context) ([]corp, map[string]int, error) {
	if ix.corps != nil && ix.now().Sub(ix.loadedAt) < ix.ttl {
		return ix.corps, ix.byStock, nil
	}
	corps, err := ix.load(ctx)
	if err != nil {
		if ix.corps != nil {
			return ix.corps, ix.byStock, nil // serve stale data rather than fail
		}
		return nil, nil, err
	}
	byStock, byCode, byKey := make(map[string]int), make(map[string]int), make(map[string][]int)
	for i := range corps {
		corps[i].norm = normName(corps[i].Name)
		corps[i].normEng = normName(corps[i].EngName)
		if corps[i].StockCode != "" {
			byStock[corps[i].StockCode] = i
		}
		byCode[corps[i].Code] = i
		k := normalize.CompanyKey(corps[i].Name)
		byKey[k] = append(byKey[k], i)
	}
	ix.corps, ix.byStock, ix.byCode, ix.byKey, ix.loadedAt = corps, byStock, byCode, byKey, ix.now()
	return corps, byStock, nil
}

func (ix *corpIndex) byCorpCode(ctx context.Context, code string) (*corp, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	corps, _, err := ix.getLocked(ctx)
	if err != nil {
		return nil, err
	}
	i, ok := ix.byCode[code]
	if !ok {
		return nil, nil
	}
	return &corps[i], nil
}

// named returns the companies whose name has the same normalize.CompanyKey,
// so SK플래닛 finds 에스케이플래닛.
func (ix *corpIndex) named(ctx context.Context, name string) ([]corp, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	corps, _, err := ix.getLocked(ctx)
	if err != nil {
		return nil, err
	}
	var out []corp
	for _, i := range ix.byKey[normalize.CompanyKey(name)] {
		out = append(out, corps[i])
	}
	return out, nil
}

// byName finds the one company a name in a filing refers to: the only
// company with that name, else the only one with a stock code, else the only
// one listed now. Two 삼성물산 have a stock code; the one merged in 2015 is
// delisted.
func (c *client) byName(ctx context.Context, name string) (*corp, error) {
	hits, err := c.corps.named(ctx, name)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	if len(hits) == 1 {
		return &hits[0], nil
	}
	hits = slices.DeleteFunc(hits, func(h corp) bool { return h.StockCode == "" })
	if len(hits) > 1 {
		classes, err := c.corpClasses(ctx, hits)
		if err != nil {
			return nil, err
		}
		hits = slices.DeleteFunc(hits, func(h corp) bool { return !listed(classes[h.Code]) })
	}
	if len(hits) != 1 {
		return nil, nil
	}
	return &hits[0], nil
}

func (ix *corpIndex) byStockCode(ctx context.Context, code string) (*corp, error) {
	corps, byStock, err := ix.get(ctx)
	if err != nil {
		return nil, err
	}
	if i, ok := byStock[code]; ok {
		return &corps[i], nil
	}
	return nil, nil
}

// search ranks matches: exact code or name first, then name prefix, then
// substring, then English name; companies with a stock code (listed now or
// before), shorter names and recently updated entries first. listedOnly
// keeps those with a stock code.
func (ix *corpIndex) search(ctx context.Context, query string, listedOnly bool) ([]corp, error) {
	corps, _, err := ix.get(ctx)
	if err != nil {
		return nil, err
	}
	q := normName(query)
	upper := strings.ToUpper(strings.TrimSpace(query))

	type hit struct {
		i, score int
	}
	var hits []hit
	for i := range corps {
		c := &corps[i]
		if listedOnly && c.StockCode == "" {
			continue
		}
		score := -1
		switch {
		case c.Code == upper || c.StockCode == upper || c.norm == q:
			score = 0
		case strings.HasPrefix(c.norm, q):
			score = 1
		case strings.Contains(c.norm, q):
			score = 2
		case c.normEng != "" && strings.Contains(c.normEng, q):
			score = 3
		}
		if score >= 0 {
			hits = append(hits, hit{i, score})
		}
	}
	sort.Slice(hits, func(a, b int) bool {
		ca, cb := &corps[hits[a].i], &corps[hits[b].i]
		if hits[a].score != hits[b].score {
			return hits[a].score < hits[b].score
		}
		if (ca.StockCode != "") != (cb.StockCode != "") {
			return ca.StockCode != ""
		}
		if len(ca.Name) != len(cb.Name) {
			return len(ca.Name) < len(cb.Name)
		}
		if ca.Name != cb.Name {
			return ca.Name < cb.Name
		}
		// Of two companies with one name, the recently updated one is more
		// likely current: the delisted 삼성물산 was last updated in 2017.
		return ca.ModifyDate > cb.ModifyDate
	})
	out := make([]corp, len(hits))
	for i, h := range hits {
		out[i] = corps[h.i]
	}
	return out, nil
}

func normName(s string) string { return normalize.CompanyName(s) }

// CorpEntry is one entry of DART's company list, for the collector.
type CorpEntry struct {
	CorpCode   string
	Name       string
	EngName    string
	StockCode  string // empty for unlisted companies
	ModifyDate string // YYYY-MM-DD
}

// Companies downloads DART's full company list (corpCode.xml, ~120k
// entries). The collector diffs it daily; the tools keep their own index.
func (p *Provider) Companies(ctx context.Context, key string) ([]CorpEntry, error) {
	c := &client{
		base: p.BaseURL,
		key:  key,
		// The zip is about 3.6 MB, and from abroad OpenDART can send under
		// 20 KB/s. One retry: each attempt can take minutes.
		raw: httpx.New(httpx.WithHTTPClient(&http.Client{Timeout: cmp.Or(p.Timeout, 30*time.Minute)}),
			httpx.WithLegacyRSATLS(), httpx.WithRetries(1)),
	}
	corps, err := c.downloadCorps(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CorpEntry, len(corps))
	for i, c := range corps {
		out[i] = CorpEntry{CorpCode: c.Code, Name: c.Name, EngName: c.EngName, StockCode: c.StockCode, ModifyDate: c.ModifyDate}
	}
	return out, nil
}

type corpCodeXML struct {
	Status  string `xml:"status"`
	Message string `xml:"message"`
	List    []struct {
		CorpCode    string `xml:"corp_code"`
		CorpName    string `xml:"corp_name"`
		CorpEngName string `xml:"corp_eng_name"`
		StockCode   string `xml:"stock_code"`
		ModifyDate  string `xml:"modify_date"`
	} `xml:"list"`
}

// downloadCorps fetches corpCode.xml: a zip holding one XML file on success,
// or a plain XML status document on error.
func (c *client) downloadCorps(ctx context.Context) ([]corp, error) {
	body, err := c.raw.Get(ctx, c.base+"/corpCode.xml", url.Values{"crtfc_key": {c.key}}, nil)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(body, []byte("PK")) {
		var doc corpCodeXML
		if xml.Unmarshal(body, &doc) == nil && doc.Status != "" {
			if err := statusError(apiStatus{doc.Status, doc.Message}); err != nil {
				return nil, err
			}
		}
		return nil, hub.Errorf(hub.CodeUpstream, "OpenDART corpCode.xml: unexpected response")
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, hub.Errorf(hub.CodeUpstream, "OpenDART corpCode.xml: invalid zip").Wrap(err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, hub.Errorf(hub.CodeUpstream, "OpenDART corpCode.xml: %v", err).Wrap(err)
		}
		defer rc.Close()
		return parseCorpXML(rc)
	}
	return nil, hub.Errorf(hub.CodeUpstream, "OpenDART corpCode.xml: no XML file in zip")
}

func parseCorpXML(r io.Reader) ([]corp, error) {
	var doc corpCodeXML
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, hub.Errorf(hub.CodeUpstream, "OpenDART corpCode.xml: invalid XML").Wrap(err)
	}
	corps := make([]corp, 0, len(doc.List))
	for _, l := range doc.List {
		corps = append(corps, corp{
			Code:       strings.TrimSpace(l.CorpCode),
			Name:       strings.TrimSpace(l.CorpName),
			EngName:    strings.TrimSpace(l.CorpEngName),
			StockCode:  strings.TrimSpace(l.StockCode), // unlisted companies have " "
			ModifyDate: normalize.Date(l.ModifyDate),
		})
	}
	return corps, nil
}
