package datagokr

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakePortal serves testdata files by operation name and records the last
// query per operation. A key "op?K=V" serves only requests whose query has
// K=V, and takes precedence over "op".
type fakePortal struct {
	t     *testing.T
	files map[string]string // operation -> testdata file, or an inline XML body; "401 file" sets the status
	mu    sync.Mutex
	last  map[string]url.Values
	body  map[string]string // operation -> last request body
}

func (f *fakePortal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	op := path.Base(r.URL.Path)
	reqBody, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.last[op] = r.URL.Query()
	f.body[op] = string(reqBody)
	f.mu.Unlock()
	src := f.files[op]
	for k, v := range f.files {
		o, kv, _ := strings.Cut(k, "?")
		name, val, ok := strings.Cut(kv, "=")
		if ok && o == op && r.URL.Query().Get(name) == val {
			src = v
		}
	}
	if code, file, ok := strings.Cut(src, " "); ok && len(code) == 3 && !strings.HasPrefix(src, "<") {
		status, _ := strconv.Atoi(code)
		w.WriteHeader(status)
		src = file
	}
	if strings.HasPrefix(src, "<") { // inline body
		w.Write([]byte(src))
		return
	}
	b, err := os.ReadFile("testdata/" + src)
	if err != nil {
		f.t.Errorf("no fixture for %s", op)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Write(b)
}

func (f *fakePortal) lastBody(op string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body[op]
}

func (f *fakePortal) query(op string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[op]
}

func setup(t *testing.T, files map[string]string) (*fakePortal, *mcp.ClientSession) {
	t.Helper()
	f := &fakePortal{t: t, files: files, last: map[string]url.Values{}, body: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	p := &Provider{BaseURL: srv.URL, ODCloudURL: srv.URL, now: func() time.Time { return time.Date(2026, 10, 25, 10, 0, 0, 0, kst) }}
	h := hub.New(&mcp.Implementation{Name: "test", Version: "0"}, slog.New(slog.DiscardHandler))
	h.Add(p, func(k string) string {
		if k == envKey {
			return testKey
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

// call invokes a tool and decodes either its structured output into out or
// its error into a *hub.Error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *hub.Error {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
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

func TestListHolidays(t *testing.T) {
	f, cs := setup(t, map[string]string{"getRestDeInfo": "holidays_2026.xml", "get24DivisionsInfo": "solar_terms_2026.xml"})

	var got hub.List[Holiday]
	if e := call(t, cs, "datagokr_list_holidays", map[string]any{"year": 2026}, &got); e != nil {
		t.Fatal(e)
	}
	q := f.query("getRestDeInfo")
	if q.Get("solYear") != "2026" || q.Has("solMonth") || q.Get("numOfRows") != "100" {
		t.Errorf("query: %v", q)
	}
	if len(got.Items) != 22 || got.PageInfo.TotalCount != 22 || got.PageInfo.HasNext {
		t.Fatalf("got %d items, %+v", len(got.Items), got.PageInfo)
	}
	byDate := map[string]Holiday{}
	for _, h := range got.Items {
		byDate[h.Date] = h
	}
	for date, want := range map[string]Holiday{
		"2026-01-01": {Weekday: "Thursday", Name: "1월1일", NameEn: "New Year's Day"},
		"2026-02-17": {Weekday: "Tuesday", Name: "설날", NameEn: "Seollal (Lunar New Year)"},
		"2026-03-02": {Weekday: "Monday", Name: "대체공휴일(삼일절)", NameEn: "Substitute holiday for Independence Movement Day"},
		"2026-06-03": {Weekday: "Wednesday", Name: "전국동시지방선거", NameEn: "Election day"},
		"2026-09-25": {Weekday: "Friday", Name: "추석", NameEn: "Chuseok (Korean Thanksgiving)"},
	} {
		want.Date, want.IsHoliday = date, true
		if byDate[date] != want {
			t.Errorf("%s: got %+v, want %+v", date, byDate[date], want)
		}
	}

	got = hub.List[Holiday]{}
	if e := call(t, cs, "datagokr_list_holidays", map[string]any{"year": 2026, "month": 3, "kind": "solar_term"}, &got); e != nil {
		t.Fatal(e)
	}
	if q := f.query("get24DivisionsInfo"); q.Get("solMonth") != "03" {
		t.Errorf("month must be zero-padded: %v", q)
	}
	if h := got.Items[0]; h.Name != "소한" || h.IsHoliday || h.NameEn != "" {
		t.Errorf("solar term: %+v", h)
	}
}

func TestListHolidaysValidation(t *testing.T) {
	_, cs := setup(t, nil)
	for _, args := range []map[string]any{
		{"year": 2003},              // no data before 2004; upstream would say "success, 0 results"
		{"year": 2029},              // not published yet
		{"year": 2026, "month": 13}, // upstream would also say "success, 0 results"
		{"year": 2026, "kind": "national"},
		{"year": 2026, "page_size": 101},
	} {
		if e := call(t, cs, "datagokr_list_holidays", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%v: got %+v", args, e)
		}
	}
	if e := call(t, cs, "datagokr_list_holidays", map[string]any{"year": 2003}, nil); !strings.Contains(e.Message, "2004") {
		t.Errorf("message should give the valid range: %s", e.Message)
	}
}

func TestHolidayNameEn(t *testing.T) {
	for in, want := range map[string]string{
		"대체공휴일(어린이날)": "Substitute holiday for Children's Day",
		"대체공휴일(무언가)":  "Substitute holiday",
		"제21대 대통령선거":  "Election day",
		"알 수 없는 날":    "",
	} {
		if got := holidayNameEn(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
