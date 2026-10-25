package httpx

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

func TestRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct{ OK bool }
	err := New(WithBackoff(time.Millisecond)).GetJSON(context.Background(), srv.URL, nil, nil, &out)
	if err != nil || !out.OK || calls.Load() != 3 {
		t.Fatalf("err=%v out=%v calls=%d", err, out, calls.Load())
	}
}

func TestStatusMappingAndKeyRedaction(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   hub.Code
		calls  int32
	}{
		{http.StatusUnauthorized, hub.CodeUnauthenticated, 1},
		{http.StatusNotFound, hub.CodeNotFound, 1},
		{http.StatusTooManyRequests, hub.CodeRateLimited, 3},
		{http.StatusInternalServerError, hub.CodeUpstream, 1},
	} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(tc.status)
		}))
		_, err := New(WithBackoff(time.Millisecond)).Get(context.Background(), srv.URL+"/api",
			url.Values{"crtfc_key": {"SECRET"}}, nil)
		srv.Close()

		he := hub.AsError(err)
		if he.Code != tc.code || calls.Load() != tc.calls {
			t.Errorf("%d: code=%s calls=%d", tc.status, he.Code, calls.Load())
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%d: API key leaked in error: %s", tc.status, err)
		}
	}
}

func TestNetworkErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused
	_, err := New(WithRetries(0)).Get(context.Background(), srv.URL, url.Values{"serviceKey": {"SECRET"}}, nil)
	if he := hub.AsError(err); he.Code != hub.CodeUnavailable || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("got %v", err)
	}
}

func TestCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(r.URL.Query().Get("q")))
	}))
	defer srv.Close()

	c := New(WithCache(time.Minute, 10))
	for _, q := range []string{"a", "a", "b"} {
		b, err := c.Get(context.Background(), srv.URL, url.Values{"q": {q}}, nil)
		if err != nil || string(b) != q {
			t.Fatalf("q=%s got %q err=%v", q, b, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", calls.Load())
	}
}

func TestCacheCheckSkipsErrorBodies(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"status":"020"}`)) // error reported inside HTTP 200
	}))
	defer srv.Close()

	c := New(WithCache(time.Minute, 10), WithCacheCheck(func(b []byte) bool {
		return !strings.Contains(string(b), `"020"`)
	}))
	for range 2 {
		if _, err := c.Get(context.Background(), srv.URL, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("error body must not be cached, got %d upstream calls", calls.Load())
	}
}

func TestLegacyRSATLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{
		MaxVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256},
	}
	srv.StartTLS()
	defer srv.Close()

	if _, err := New(WithHTTPClient(srv.Client()), WithRetries(0)).Get(context.Background(), srv.URL, nil, nil); err == nil {
		t.Fatal("default client should fail the handshake with an RSA-only server")
	}
	c := New(WithHTTPClient(srv.Client()), WithLegacyRSATLS(), WithRetries(0))
	b, err := c.Get(context.Background(), srv.URL, nil, nil)
	if err != nil || string(b) != "ok" {
		t.Fatalf("got %q, %v", b, err)
	}
}
