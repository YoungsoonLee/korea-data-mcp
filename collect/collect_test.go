package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeDART stands in for the opendart provider: same tool names, inputs and
// output shapes, data from memory.
type fakeDART struct {
	filings []disclosure
	reports map[string][]report // corp_code + "/" + kind
	errs    map[string]hub.Code // corp_code -> error to return
	calls   map[string]int
}

type disclosure struct {
	CorpCode   string `json:"corp_code"`
	ReportName string `json:"report_name"`
}

type report struct {
	Date      string `json:"date"`
	Reporter  string `json:"reporter"`
	ReceiptNo string `json:"receipt_no"`
	Percent   float64
}

type searchIn struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Type      string `json:"type"`
	hub.PageParams
}

type ownershipIn struct {
	CorpCode string `json:"corp_code"`
	Kind     string `json:"kind"`
	hub.PageParams
}

type ownershipOut struct {
	Reports  []report     `json:"reports"`
	PageInfo hub.PageInfo `json:"page_info"`
}

func page[T any](items []T, p hub.PageParams) ([]T, hub.PageInfo) {
	from := min((p.Page-1)*p.PageSize, len(items))
	to := min(from+p.PageSize, len(items))
	return append([]T{}, items[from:to]...), hub.NewPageInfo(p.Page, p.PageSize, len(items))
}

func (f *fakeDART) Info() hub.ProviderInfo { return hub.ProviderInfo{ID: "opendart", Title: "fake"} }

func (f *fakeDART) Register(r *hub.Registrar) error {
	hub.AddTool(r, &mcp.Tool{Name: "opendart_search_disclosures", Description: "fake"},
		func(_ context.Context, in searchIn) (hub.List[disclosure], error) {
			f.calls["search"]++
			if in.Type != "shareholding" || in.StartDate != "2026-11-03" || in.EndDate != "2026-11-09" {
				return hub.List[disclosure]{}, hub.Errorf(hub.CodeInvalidArgument, "unexpected input %+v", in)
			}
			items, pi := page(f.filings, in.PageParams)
			return hub.List[disclosure]{Items: items, PageInfo: pi}, nil
		})
	hub.AddTool(r, &mcp.Tool{Name: "opendart_get_ownership_reports", Description: "fake"},
		func(_ context.Context, in ownershipIn) (ownershipOut, error) {
			f.calls[in.CorpCode+"/"+in.Kind]++
			if code, ok := f.errs[in.CorpCode]; ok {
				return ownershipOut{}, hub.Errorf(code, "fake %s", code)
			}
			items, pi := page(f.reports[in.CorpCode+"/"+in.Kind], in.PageParams)
			return ownershipOut{Reports: items, PageInfo: pi}, nil
		})
	return nil
}

func reports(n int, prefix string) []report {
	out := make([]report, n)
	for i := range out {
		out[i] = report{Date: "2026-11-06", Reporter: "보고자", ReceiptNo: fmt.Sprintf("%s%06d", prefix, i), Percent: 5.1}
	}
	return out
}

func setup(t *testing.T, f *fakeDART, corps []store.Corp) (*Collector, *store.Store) {
	t.Helper()
	ctx := context.Background()
	f.calls = map[string]int{}
	h := hub.New(&mcp.Implementation{Name: "t", Version: "0"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.Add(f, func(string) string { return "" })
	sess, err := Connect(ctx, h.Server())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Collector{
		Store:     st,
		Tools:     sess,
		Companies: func(context.Context) ([]store.Corp, error) { return corps, nil },
		Now:       func() time.Time { return time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC) }, // 10:00 KST
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, st
}

var corps = []store.Corp{
	{CorpCode: "00663669", Name: "폴라리스우노", StockCode: "114630"},
	{CorpCode: "01956751", Name: "래블업", StockCode: "0147B0"},
	{CorpCode: "00223762", Name: "KT지니뮤직", StockCode: "043610"},
	{CorpCode: "09999999", Name: "비상장"},
}

func TestDaily(t *testing.T) {
	ctx := context.Background()
	f := &fakeDART{
		reports: map[string][]report{
			"00663669/major_holders": reports(150, "2026"), // two pages
			"00663669/insiders":      reports(2, "2025"),
			"00223762/insiders":      reports(3, "2024"),
		},
		errs: map[string]hub.Code{"01956751": hub.CodeNotFound},
	}
	for range 120 { // two pages of filings
		f.filings = append(f.filings, disclosure{CorpCode: "00663669", ReportName: "[기재정정]주식등의대량보유상황보고서(일반)"})
	}
	f.filings = append(f.filings,
		disclosure{CorpCode: "00663669", ReportName: "임원ㆍ주요주주특정증권등소유상황보고서"},
		disclosure{CorpCode: "00223762", ReportName: "임원ㆍ주요주주특정증권등소유상황보고서"},
		disclosure{CorpCode: "00223762", ReportName: "임원ㆍ주요주주특정증권등거래계획보고서"}, // not a holding report
		disclosure{CorpCode: "09999999", ReportName: "의결권대리행사권유참고서류"},
		disclosure{CorpCode: "01956751", ReportName: "주식등의대량보유상황보고서(약식)"},
	)
	c, st := setup(t, f, corps)

	stats, err := c.Daily(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Companies != 4 || stats.Filings != 123 || stats.Corps != 3 || stats.Errors != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if stats.ReportsAdded[MajorHolders] != 150 || stats.ReportsAdded[Insiders] != 5 {
		t.Errorf("added = %v", stats.ReportsAdded)
	}
	if f.calls["00223762/major_holders"] != 0 || f.calls["09999999/insiders"] != 0 {
		t.Errorf("collected kinds not filed: %v", f.calls)
	}
	if f.calls["00663669/major_holders"] != 2 || f.calls["search"] != 2 {
		t.Errorf("paging: %v", f.calls)
	}
	if n, _ := st.ReportCount(ctx, "00663669", MajorHolders); n != 150 {
		t.Errorf("stored = %d", n)
	}

	// Running again adds nothing.
	stats, err = c.Daily(ctx, 7)
	if err != nil || stats.ReportsAdded[MajorHolders] != 0 || stats.ReportsAdded[Insiders] != 0 {
		t.Errorf("second run: %+v, %v", stats, err)
	}
}

func TestDailyStopsAtQuota(t *testing.T) {
	f := &fakeDART{
		filings: []disclosure{
			{CorpCode: "00223762", ReportName: "임원ㆍ주요주주특정증권등소유상황보고서"},
			{CorpCode: "00663669", ReportName: "주식등의대량보유상황보고서(일반)"},
		},
		reports: map[string][]report{"00663669/major_holders": reports(1, "2026")},
		errs:    map[string]hub.Code{"00223762": hub.CodeRateLimited},
	}
	c, _ := setup(t, f, corps)
	stats, err := c.Daily(context.Background(), 7)
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("err = %v, want ErrStopped", err)
	}
	if f.calls["00663669/major_holders"] != 0 || stats.Corps != 0 {
		t.Errorf("kept going after the quota: %v %+v", f.calls, stats)
	}
}

func TestDailyWithoutCompanyList(t *testing.T) {
	f := &fakeDART{
		filings: []disclosure{{CorpCode: "00663669", ReportName: "주식등의대량보유상황보고서(일반)"}},
		reports: map[string][]report{"00663669/major_holders": reports(1, "2026")},
	}
	c, _ := setup(t, f, nil)
	c.Companies = func(context.Context) ([]store.Corp, error) { return nil, errors.New("download timed out") }
	stats, err := c.Daily(context.Background(), 7)
	if err == nil || errors.Is(err, ErrStopped) {
		t.Errorf("err = %v, want the download error", err)
	}
	if stats.ReportsAdded[MajorHolders] != 1 {
		t.Errorf("reports not collected without the company list: %+v", stats)
	}
}

func TestBackfill(t *testing.T) {
	ctx := context.Background()
	f := &fakeDART{reports: map[string][]report{
		"00223762/major_holders": reports(2, "2026"),
		"00223762/insiders":      reports(4, "2025"),
		"00663669/major_holders": reports(1, "2024"),
	}}
	c, st := setup(t, f, corps)

	stats, err := c.Backfill(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Listed companies by corp_code; the unlisted one is skipped.
	if stats.Corps != 2 || stats.ReportsAdded[MajorHolders] != 3 || stats.ReportsAdded[Insiders] != 4 {
		t.Errorf("stats = %+v", stats)
	}
	stats, err = c.Backfill(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Corps != 2 || f.calls["01956751/major_holders"] != 1 || f.calls["09999999/major_holders"] != 0 {
		t.Errorf("second run should start with 래블업: %+v %v", stats, f.calls)
	}
	if n, _ := st.ReportCount(ctx, "", Insiders); n != 4 {
		t.Errorf("insiders = %d", n)
	}
}

func TestReportKind(t *testing.T) {
	for title, want := range map[string]string{
		"주식등의대량보유상황보고서(일반)":         MajorHolders,
		"[기재정정]주식등의대량보유상황보고서(약식)":   MajorHolders,
		"임원ㆍ주요주주특정증권등소유상황보고서":       Insiders,
		"[기재정정]임원ㆍ주요주주특정증권등소유상황보고서": Insiders,
		"임원ㆍ주요주주특정증권등거래계획보고서":       "",
		"의결권대리행사권유참고서류":             "",
	} {
		if got := reportKind(title); got != want {
			t.Errorf("reportKind(%q) = %q, want %q", title, got, want)
		}
	}
}
