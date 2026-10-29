package datagokr

import (
	"os"
	"strings"
	"testing"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

func TestCheckBusinessStatus(t *testing.T) {
	f, cs := setup(t, map[string]string{"status": "nts_status_mixed.json"})

	var got hub.List[BusinessStatus]
	if e := call(t, cs, "datagokr_check_business_status", map[string]any{"business_numbers": []string{"124-81-00998", "1234567890"}}, &got); e != nil {
		t.Fatal(e)
	}
	// Hyphens are removed before sending: with them the NTS reports even a
	// real number as not registered (see testdata/nts_status_hyphenated.json).
	if b := f.lastBody("status"); b != `{"b_no":["1248100998","1234567890"]}` {
		t.Errorf("request body: %s", b)
	}
	if f.query("status").Get("serviceKey") != testKey {
		t.Error("serviceKey not sent")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items: %+v", got.Items)
	}
	samsung, unknown := got.Items[0], got.Items[1]
	want := BusinessStatus{BizRegNo: "1248100998", Registered: true, Status: "active", StatusKo: "계속사업자", TaxType: "general_vat", TaxTypeKo: "부가가치세 일반과세자"}
	if samsung != want {
		t.Errorf("got %+v, want %+v", samsung, want)
	}
	// The NTS puts "국세청에 등록되지 않은 사업자등록번호입니다." in tax_type.
	if unknown != (BusinessStatus{BizRegNo: "1234567890"}) {
		t.Errorf("unregistered: %+v", unknown)
	}
}

func TestCheckBusinessStatusErrors(t *testing.T) {
	_, cs := setup(t, map[string]string{"status": "401 nts_error_bad_key.json"})
	e := call(t, cs, "datagokr_check_business_status", map[string]any{"business_numbers": []string{"1248100998"}}, nil)
	if e == nil || e.Code != hub.CodeUnauthenticated || e.UpstreamCode != "-4" || !strings.Contains(e.Hint, "활용신청") {
		t.Errorf("bad key: %+v", e)
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = "1248100998"
	}
	for _, args := range []map[string]any{
		{"business_numbers": []string{}},
		{"business_numbers": many},
		{"business_numbers": []string{"1248100998", "12345"}},
	} {
		if e := call(t, cs, "datagokr_check_business_status", args, nil); e == nil || e.Code != hub.CodeInvalidArgument {
			t.Errorf("%d numbers: got %+v", len(args["business_numbers"].([]string)), e)
		}
	}
	if e := call(t, cs, "datagokr_check_business_status", map[string]any{"business_numbers": []string{"12345"}}, nil); !strings.Contains(e.Message, "12345") {
		t.Errorf("message should name the bad number: %s", e.Message)
	}
}

func TestODCloudError(t *testing.T) {
	for _, tc := range []struct {
		status int
		file   string
		code   hub.Code
	}{
		{401, "nts_error_bad_key.json", hub.CodeUnauthenticated},
		{413, "nts_error_too_large.json", hub.CodeInvalidArgument},
	} {
		b, err := os.ReadFile("testdata/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if e := hub.AsError(odcloudError(tc.status, b)); e.Code != tc.code {
			t.Errorf("%s: got %+v", tc.file, e)
		}
	}
	if odcloudError(504, []byte("<html>504 Gateway Time-out</html>")) != nil {
		t.Error("non-JSON bodies fall back to the status mapping (504 is retried)")
	}
}
