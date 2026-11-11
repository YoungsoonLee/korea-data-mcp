// Package collect fills the history store from OpenDART. It calls the same
// MCP tools an agent does, in process, so every quirk the tools handle is
// handled in the stored data too. See docs/ENTITY_DESIGN.md.
package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/store"
)

// kst is Korea Standard Time; DART dates are KST dates.
var kst = time.FixedZone("KST", 9*60*60)

// Ownership report kinds, as opendart_get_ownership_reports names them.
const (
	MajorHolders = "major_holders"
	Insiders     = "insiders"
)

// Caller calls an MCP tool and decodes its structured output into out.
// Tool errors come back as *hub.Error.
type Caller interface {
	Call(ctx context.Context, name string, args, out any) error
}

type Collector struct {
	Store *store.Store
	Tools Caller
	// Companies downloads DART's company list (corpCode.xml).
	Companies func(context.Context) ([]store.Corp, error)
	Now       func() time.Time
	Logger    *slog.Logger
}

// Stats summarizes a run; it is stored with the run.
type Stats struct {
	Companies    int            `json:"companies,omitempty"`
	Events       map[string]int `json:"events,omitempty"`
	Filings      int            `json:"filings,omitempty"`
	Corps        int            `json:"corps"`
	ToolCalls    int            `json:"tool_calls"`
	ReportsAdded map[string]int `json:"reports_added"`
	Errors       int            `json:"errors"`
}

func newStats() *Stats {
	return &Stats{Events: map[string]int{}, ReportsAdded: map[string]int{}}
}

// ErrStopped wraps an error after which the run cannot continue: the daily
// quota is used up or the key is rejected. Stats up to that point are valid.
var ErrStopped = errors.New("collect: stopped")

func (c *Collector) today() time.Time { return c.Now().In(kst) }

// SyncCompanies downloads the company list and records changes since the
// last sync.
func (c *Collector) SyncCompanies(ctx context.Context, st *Stats) error {
	corps, err := c.Companies(ctx)
	if err != nil {
		return fmt.Errorf("download company list: %w", err)
	}
	events, err := c.Store.SyncCorps(ctx, corps, c.today().Format(time.DateOnly))
	if err != nil {
		return err
	}
	st.Companies = len(corps)
	for _, e := range events {
		st.Events[e.Type]++
	}
	c.Logger.Info("company list synced", "companies", len(corps), "events", len(events))
	return nil
}

// Daily syncs the company list, then collects ownership reports for every
// company with an ownership filing in the last days days. Filings are looked
// up again for several days because a receipt number's date can be earlier
// than its receipt date (20261106000480 was received 2026-11-09).
func (c *Collector) Daily(ctx context.Context, days int) (*Stats, error) {
	st := newStats()
	var errs []error
	if err := c.SyncCompanies(ctx, st); err != nil {
		// The download is large and sometimes fails; the reports do not
		// depend on it, so collect them anyway.
		c.Logger.Error("company list not synced", "err", err)
		st.Errors++
		errs = append(errs, err)
	}

	todo, err := c.ownershipFilings(ctx, days, st)
	if err != nil {
		return st, errors.Join(append(errs, err)...)
	}
	for _, code := range slices.Sorted(maps.Keys(todo)) {
		for _, kind := range []string{MajorHolders, Insiders} {
			if !todo[code][kind] {
				continue
			}
			if err := c.collect(ctx, code, kind, st); err != nil {
				return st, errors.Join(append(errs, err)...)
			}
		}
		st.Corps++
	}
	return st, errors.Join(errs...)
}

// Backfill collects both kinds of ownership reports for up to limit listed
// companies, starting with those never collected. Run it on several days
// to cover every listed company within the daily quota.
func (c *Collector) Backfill(ctx context.Context, limit int) (*Stats, error) {
	st := newStats()
	n, err := c.Store.CorpCount(ctx)
	if err != nil {
		return st, err
	}
	if n == 0 {
		if err := c.SyncCompanies(ctx, st); err != nil {
			return st, err
		}
	}
	codes, err := c.Store.ListedCorps(ctx, MajorHolders, limit)
	if err != nil {
		return st, err
	}
	for i, code := range codes {
		for _, kind := range []string{MajorHolders, Insiders} {
			if err := c.collect(ctx, code, kind, st); err != nil {
				return st, err
			}
		}
		st.Corps++
		if (i+1)%100 == 0 {
			c.Logger.Info("backfill progress", "corps", i+1, "of", len(codes), "tool_calls", st.ToolCalls)
		}
	}
	return st, nil
}

// ownershipFilings returns the companies with ownership filings in the last
// days days, and which report kinds they filed.
func (c *Collector) ownershipFilings(ctx context.Context, days int, st *Stats) (map[string]map[string]bool, error) {
	end := c.today()
	start := end.AddDate(0, 0, -(days - 1))
	todo := map[string]map[string]bool{}
	for page := 1; ; page++ {
		var out hub.List[struct {
			CorpCode   string `json:"corp_code"`
			ReportName string `json:"report_name"`
		}]
		err := c.call(ctx, "opendart_search_disclosures", map[string]any{
			"start_date": start.Format(time.DateOnly), "end_date": end.Format(time.DateOnly),
			"type": "shareholding", "page": page, "page_size": 100,
		}, &out, st)
		if err != nil {
			return nil, err
		}
		for _, d := range out.Items {
			kind := reportKind(d.ReportName)
			if kind == "" {
				continue
			}
			st.Filings++
			if todo[d.CorpCode] == nil {
				todo[d.CorpCode] = map[string]bool{}
			}
			todo[d.CorpCode][kind] = true
		}
		if !out.PageInfo.HasNext {
			break
		}
	}
	c.Logger.Info("ownership filings found", "from", start.Format(time.DateOnly), "to", end.Format(time.DateOnly),
		"filings", st.Filings, "corps", len(todo))
	return todo, nil
}

// reportKind maps a disclosure title to the report kind that stores it.
// Trading plans (거래계획보고서) and proxy solicitations are also 지분공시
// but are not ownership reports.
func reportKind(title string) string {
	switch {
	case strings.Contains(title, "대량보유상황보고서"):
		return MajorHolders
	case strings.Contains(title, "소유상황보고서"):
		return Insiders
	}
	return ""
}

type reportRow struct {
	Date      string `json:"date"`
	Reporter  string `json:"reporter"`
	ReceiptNo string `json:"receipt_no"`
}

// collect stores every ownership report of kind that OpenDART returns for
// a company. Errors other than a stopped run are logged and skipped.
func (c *Collector) collect(ctx context.Context, code, kind string, st *Stats) error {
	var reports []store.Report
	for page := 1; ; page++ {
		var out struct {
			Reports  []json.RawMessage `json:"reports"`
			PageInfo hub.PageInfo      `json:"page_info"`
		}
		err := c.call(ctx, "opendart_get_ownership_reports", map[string]any{
			"corp_code": code, "kind": kind, "page": page, "page_size": 100,
		}, &out, st)
		if errors.Is(err, ErrStopped) {
			return err
		}
		if err != nil {
			c.Logger.Warn("ownership reports skipped", "corp_code", code, "kind", kind, "err", err)
			st.Errors++
			return nil
		}
		for _, raw := range out.Reports {
			var r reportRow
			if err := json.Unmarshal(raw, &r); err != nil || r.ReceiptNo == "" {
				continue
			}
			reports = append(reports, store.Report{
				Kind: kind, ReceiptNo: r.ReceiptNo, CorpCode: code, Reporter: r.Reporter, FiledOn: r.Date, Body: raw,
			})
		}
		if !out.PageInfo.HasNext {
			break
		}
	}
	added, err := c.Store.AddReports(ctx, code, kind, reports, c.Now())
	if err != nil {
		return err
	}
	st.ReportsAdded[kind] += added
	return nil
}

// call calls a tool, turning quota and key errors into ErrStopped.
func (c *Collector) call(ctx context.Context, name string, args, out any, st *Stats) error {
	st.ToolCalls++
	err := c.Tools.Call(ctx, name, args, out)
	var he *hub.Error
	if errors.As(err, &he) && (he.Code == hub.CodeRateLimited || he.Code == hub.CodeUnauthenticated) {
		return fmt.Errorf("%w: %s", ErrStopped, he.Message)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrStopped, ctx.Err())
	}
	return err
}
