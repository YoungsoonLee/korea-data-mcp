package lawgokr

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testOC = "TEST-OC"

// fakeLaw serves testdata files. A route is "lawSearch?K=V&K2=V2": the
// endpoint and query values that must match; the most specific match wins.
type fakeLaw struct {
	t      *testing.T
	routes map[string]string
	mu     sync.Mutex
	last   map[string]url.Values // endpoint -> last query
}

func (f *fakeLaw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ep := strings.TrimSuffix(path.Base(r.URL.Path), ".do")
	q := r.URL.Query()
	f.mu.Lock()
	f.last[ep] = q
	f.mu.Unlock()
	best, bestN := "", -1
	for route, file := range f.routes {
		e, cond, _ := strings.Cut(route, "?")
		want, _ := url.ParseQuery(cond)
		ok := e == ep
		for k := range want {
			ok = ok && q.Get(k) == want.Get(k)
		}
		if ok && len(want) > bestN {
			best, bestN = file, len(want)
		}
	}
	if best == "" {
		f.t.Errorf("no route for %s %v", ep, q)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	b, err := os.ReadFile("testdata/" + best)
	if err != nil {
		f.t.Fatal(err)
	}
	w.Write(b)
}

func (f *fakeLaw) query(ep string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[ep]
}

func setup(t *testing.T, routes map[string]string) (*fakeLaw, *mcp.ClientSession) {
	t.Helper()
	f := &fakeLaw{t: t, routes: routes, last: map[string]url.Values{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	p := &Provider{BaseURL: srv.URL, now: func() time.Time { return time.Date(2026, 10, 28, 10, 0, 0, 0, kst) }}
	h := hub.New(&mcp.Implementation{Name: "test", Version: "0"}, slog.New(slog.DiscardHandler))
	h.Add(p, func(k string) string {
		if k == envKey {
			return testOC
		}
		return ""
	})
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := h.Server().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return f, cs
}

// call returns the tool error, if any, and decodes a successful result into out.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *hub.Error {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		var e hub.Error
		text := res.Content[0].(*mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			t.Fatalf("%s: error is not a hub.Error: %s", name, text)
		}
		return &e
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	return nil
}
