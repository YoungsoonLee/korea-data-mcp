// Package httpx is the shared upstream HTTP client: timeouts, retries with
// backoff, response caching, and mapping of HTTP failures to hub.Error.
// Error messages never include query strings, since those often carry API keys
// (OpenDART crtfc_key, data.go.kr serviceKey).
package httpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/cache"
	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

const maxBodyBytes = 10 << 20

type Client struct {
	http       *http.Client
	userAgent  string
	maxRetries int
	backoff    time.Duration
	cache      *cache.Cache[[]byte]
	cacheTTL   time.Duration
	cacheable  func(body []byte) bool
	errorBody  func(status int, body []byte) error
}

type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.http = hc } }
func WithRetries(n int) Option              { return func(c *Client) { c.maxRetries = n } }
func WithBackoff(d time.Duration) Option    { return func(c *Client) { c.backoff = d } }

// WithCache caches successful GET response bodies by full URL for ttl.
func WithCache(ttl time.Duration, maxEntries int) Option {
	return func(c *Client) {
		c.cache = cache.New[[]byte](maxEntries)
		c.cacheTTL = ttl
	}
}

// WithCacheCheck limits caching to bodies for which ok returns true. Use it
// for APIs that report errors inside HTTP 200 responses (OpenDART status,
// data.go.kr resultCode), so a quota or maintenance error is not cached.
func WithCacheCheck(ok func(body []byte) bool) Option {
	return func(c *Client) { c.cacheable = ok }
}

// WithErrorBody lets a provider classify non-2xx responses from their body.
// If decode returns nil, the generic status code mapping applies. data.go.kr,
// for example, answers an unregistered key with HTTP 403 and a body saying
// so, which is UNAUTHENTICATED rather than PERMISSION_DENIED.
func WithErrorBody(decode func(status int, body []byte) error) Option {
	return func(c *Client) { c.errorBody = decode }
}

// WithLegacyRSATLS additionally offers TLS 1.2 cipher suites with RSA key
// exchange, which Go stopped offering by default in 1.22. Some Korean public
// servers (OpenDART) negotiate only TLS_RSA_WITH_AES_128_GCM_SHA256 and fail
// with "tls: handshake failure" otherwise. Apply it after WithHTTPClient.
func WithLegacyRSATLS() Option {
	return func(c *Client) {
		hc := *c.http
		t, ok := hc.Transport.(*http.Transport)
		if !ok || t == nil {
			t = http.DefaultTransport.(*http.Transport)
		}
		hc.Transport = withLegacyRSA(t)
		c.http = &hc
	}
}

func withLegacyRSA(t *http.Transport) *http.Transport {
	t = t.Clone()
	cfg := t.TLSClientConfig
	if cfg == nil {
		cfg = &tls.Config{}
	}
	cfg = cfg.Clone()
	var suites []uint16
	for _, s := range tls.CipherSuites() {
		suites = append(suites, s.ID)
	}
	cfg.CipherSuites = append(suites,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	)
	t.TLSClientConfig = cfg
	return t
}

func New(opts ...Option) *Client {
	c := &Client{
		http:       &http.Client{Timeout: 15 * time.Second},
		userAgent:  "korea-data-mcp (+https://github.com/YoungsoonLee/korea-data-mcp)",
		maxRetries: 2,
		backoff:    500 * time.Millisecond,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Get fetches rawURL with query appended and returns the body.
func (c *Client) Get(ctx context.Context, rawURL string, query url.Values, header http.Header) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, hub.Errorf(hub.CodeInternal, "bad url").Wrap(err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	key := u.String()
	if c.cache != nil {
		if b, ok := c.cache.Get(key); ok {
			return b, nil
		}
	}
	body, err := c.do(ctx, http.MethodGet, u, header, nil)
	if err != nil {
		return nil, err
	}
	if c.cache != nil && (c.cacheable == nil || c.cacheable(body)) {
		c.cache.Set(key, body, c.cacheTTL)
	}
	return body, nil
}

func (c *Client) GetJSON(ctx context.Context, rawURL string, query url.Values, header http.Header, out any) error {
	body, err := c.Get(ctx, rawURL, query, header)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return hub.Errorf(hub.CodeUpstream, "invalid JSON from upstream").Wrap(err)
	}
	return nil
}

// PostJSON sends in as a JSON body to rawURL (with query appended) and
// decodes the JSON response into out. POST responses are never cached.
func (c *Client) PostJSON(ctx context.Context, rawURL string, query url.Values, in, out any) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return hub.Errorf(hub.CodeInternal, "bad url").Wrap(err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	reqBody, err := json.Marshal(in)
	if err != nil {
		return hub.Errorf(hub.CodeInternal, "encode request").Wrap(err)
	}
	header := http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}}
	body, err := c.do(ctx, http.MethodPost, u, header, reqBody)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return hub.Errorf(hub.CodeUpstream, "invalid JSON from upstream").Wrap(err)
	}
	return nil
}

func (c *Client) GetXML(ctx context.Context, rawURL string, query url.Values, header http.Header, out any) error {
	body, err := c.Get(ctx, rawURL, query, header)
	if err != nil {
		return err
	}
	if err := xml.Unmarshal(body, out); err != nil {
		return hub.Errorf(hub.CodeUpstream, "invalid XML from upstream").Wrap(err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method string, u *url.URL, header http.Header, body []byte) ([]byte, error) {
	safe := u.Scheme + "://" + u.Host + u.Path
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.retryDelay(attempt, lastErr)); err != nil {
				return nil, hub.AsError(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, hub.Errorf(hub.CodeInternal, "build request").Wrap(err)
		}
		for k, vs := range header {
			req.Header[k] = vs
		}
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, hub.AsError(ctx.Err())
			}
			// Strip *url.Error, whose message contains the full URL.
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			lastErr = hub.Errorf(hub.CodeUnavailable, "request to %s failed: %v", safe, err)
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = hub.Errorf(hub.CodeUnavailable, "reading response from %s: %v", safe, readErr)
			continue
		}
		if resp.StatusCode < 300 {
			return b, nil
		}
		err = statusError(resp, safe)
		if c.errorBody != nil {
			if e := c.errorBody(resp.StatusCode, b); e != nil {
				err = e
			}
		}
		if !hub.AsError(err).Retryable {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// retryAfterError carries a 429's Retry-After delay alongside the hub error.
type retryAfterError struct {
	err   *hub.Error
	after time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

func (c *Client) retryDelay(attempt int, lastErr error) time.Duration {
	var ra *retryAfterError
	if errors.As(lastErr, &ra) && ra.after > 0 && ra.after <= 10*time.Second {
		return ra.after
	}
	return c.backoff << (attempt - 1)
}

func statusError(resp *http.Response, safe string) error {
	msg := fmt.Sprintf("%s returned HTTP %d", safe, resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return hub.Errorf(hub.CodeInvalidArgument, "%s", msg)
	case http.StatusUnauthorized:
		return hub.Errorf(hub.CodeUnauthenticated, "%s", msg).WithHint("check the provider's API key environment variable")
	case http.StatusForbidden:
		return hub.Errorf(hub.CodePermission, "%s", msg)
	case http.StatusNotFound:
		return hub.Errorf(hub.CodeNotFound, "%s", msg)
	case http.StatusTooManyRequests:
		e := &retryAfterError{err: hub.Errorf(hub.CodeRateLimited, "%s", msg)}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.after = time.Duration(s) * time.Second
		}
		return e
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return hub.Errorf(hub.CodeUnavailable, "%s", msg)
	}
	return hub.Errorf(hub.CodeUpstream, "%s", msg)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
