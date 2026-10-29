package datagokr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

const testKey = "TEST-KEY"

// holiday mirrors a SpcdeInfoService item, enough to exercise the envelope.
type holiday struct {
	DateName  string `xml:"dateName"`
	IsHoliday string `xml:"isHoliday"`
	Locdate   string `xml:"locdate"`
}

// serve answers every request with testdata/name and the given status, and
// counts upstream calls.
func serve(t *testing.T, status int, name string) (*client, *atomic.Int32) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("serviceKey") != testKey {
			t.Errorf("serviceKey not sent")
		}
		w.WriteHeader(status)
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return newClient(srv.URL, testKey), &calls
}

func get(c *client) ([]holiday, int, error) {
	return getList[holiday](context.Background(), c, "/B090041/openapi/service/SpcdeInfoService/getRestDeInfo", url.Values{}, 1, 10)
}

func TestGetList(t *testing.T) {
	for _, tc := range []struct {
		file  string
		want  []string
		total int
	}{
		{"holidays_many.xml", []string{"개천절", "대체공휴일(개천절)", "한글날"}, 3},
		{"holidays_single.xml", []string{"기독탄신일"}, 1}, // a lone object in JSON
		{"holidays_none.xml", nil, 0},                 // "items": "" in JSON
		{"apt_trades_none.xml", nil, 0},               // success code "000" instead of "00"
	} {
		c, _ := serve(t, http.StatusOK, tc.file)
		items, total, err := get(c)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		var names []string
		for _, it := range items {
			names = append(names, it.DateName)
		}
		if items == nil || strings.Join(names, ",") != strings.Join(tc.want, ",") || total != tc.total {
			t.Errorf("%s: got %v (nil=%v), total %d", tc.file, names, items == nil, total)
		}
	}
}

func TestServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		status   int
		file     string
		code     hub.Code
		upstream string
	}{
		// HTTP 403 means "key not registered", not a permission problem.
		{http.StatusForbidden, "error_key_not_registered.xml", hub.CodeUnauthenticated, "30"},
		{http.StatusForbidden, "error_key_not_registered.json", hub.CodeUnauthenticated, "30"},
		// The same error document can also arrive with HTTP 200.
		{http.StatusOK, "error_http_200.json", hub.CodeUnavailable, "04"},
	} {
		c, _ := serve(t, tc.status, tc.file)
		_, _, err := get(c)
		e := hub.AsError(err)
		if e.Code != tc.code || e.UpstreamCode != tc.upstream || strings.Contains(e.Message, testKey) {
			t.Errorf("%s: got %+v", tc.file, e)
		}
		if tc.upstream == "30" && !strings.Contains(e.Hint, "Decoding key") {
			t.Errorf("%s: hint should explain the key: %q", tc.file, e.Hint)
		}
	}
}

func TestResultCodeInsideNormalResponse(t *testing.T) {
	for code, want := range map[string]hub.Code{
		"22":  hub.CodeRateLimited,
		"10":  hub.CodeInvalidArgument,
		"030": hub.CodeUnauthenticated,
		"77":  hub.CodeUpstream,
	} {
		_, _, err := decodeList[holiday]([]byte(`<response><header><resultCode>` + code + `</resultCode><resultMsg>m</resultMsg></header><body/></response>`))
		if e := hub.AsError(err); e.Code != want || e.UpstreamCode != code {
			t.Errorf("%s: got %+v", code, e)
		}
	}
	for _, code := range []string{"03", "003"} { // NODATA_ERROR is an empty result
		items, _, err := decodeList[holiday]([]byte(`<response><header><resultCode>` + code + `</resultCode></header><body/></response>`))
		if err != nil || items == nil || len(items) != 0 {
			t.Errorf("%s: got %v, %v", code, items, err)
		}
	}
	if _, _, err := decodeList[holiday]([]byte(`<response><body/></response>`)); hub.AsError(err).Code != hub.CodeUpstream {
		t.Errorf("missing result code must be an error, got %v", err)
	}
}

func TestOnlyNormalResponsesAreCached(t *testing.T) {
	c, calls := serve(t, http.StatusOK, "holidays_many.xml")
	get(c)
	get(c)
	if n := calls.Load(); n != 1 {
		t.Errorf("normal response: %d upstream calls, want 1", n)
	}
	c, calls = serve(t, http.StatusOK, "error_http_200.json")
	get(c)
	get(c)
	if n := calls.Load(); n != 2 {
		t.Errorf("error inside HTTP 200 must not be cached: %d upstream calls", n)
	}
}
