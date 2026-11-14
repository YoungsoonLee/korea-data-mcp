package opendart

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// DART's company list keeps a stock code after delisting: on 2026-11-11 it
// gave 4,013 companies one while KRX traded 2,876 (삼성물산 000830, merged in
// 2015, still has its code). Only company.json's corp_cls tells whether a
// company is listed now, one call per company, so classes are cached.

const classTTL = 24 * time.Hour

type classEntry struct {
	cls string
	at  time.Time
}

type classCache struct {
	mu sync.Mutex
	m  map[string]classEntry
}

// corpClass returns a company's corp_cls: Y, K or N when listed, E otherwise.
func (c *client) corpClass(ctx context.Context, code string) (string, error) {
	c.classes.mu.Lock()
	e, ok := c.classes.m[code]
	c.classes.mu.Unlock()
	if ok && c.now().Sub(e.at) < classTTL {
		return e.cls, nil
	}
	var r companyResponse
	if err := c.getJSON(ctx, "company.json", url.Values{"corp_code": {code}}, &r); err != nil {
		return "", err
	}
	c.rememberClass(code, r.CorpCls)
	return r.CorpCls, nil
}

func (c *client) rememberClass(code, cls string) {
	c.classes.mu.Lock()
	defer c.classes.mu.Unlock()
	if c.classes.m == nil {
		c.classes.m = map[string]classEntry{}
	}
	c.classes.m[code] = classEntry{cls, c.now()}
}

// corpClasses looks up the classes of companies with a stock code, four at a
// time; the others are unlisted without a call.
func (c *client) corpClasses(ctx context.Context, corps []corp) (map[string]string, error) {
	out := make(map[string]string, len(corps))
	classes := make([]string, len(corps))
	errs := make([]error, len(corps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, co := range corps {
		if co.StockCode == "" {
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			cls, err := c.corpClass(ctx, co.Code)
			if err != nil && hub.AsError(err).Code != hub.CodeNotFound {
				errs[i] = err
			}
			classes[i] = cls
		})
	}
	wg.Wait()
	for i, co := range corps {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out[co.Code] = classes[i]
	}
	return out, nil
}

func listed(cls string) bool { return cls == "Y" || cls == "K" || cls == "N" }
