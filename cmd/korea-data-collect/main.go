// Command korea-data-collect stores what OpenDART forgets: changes to the
// company list, and ownership reports that the API stops returning after
// about two years or replaces with a correction. Run it once a day:
//
//	korea-data-collect -db korea-data.db daily
//	korea-data-collect -db korea-data.db -limit 2000 backfill
//
// daily syncs the company list and collects every company with an ownership
// filing in the last days. backfill collects listed companies not collected
// yet, limit at a time (about 3 to 4 OpenDART calls each, against a quota of
// 20,000 a day).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/collect"
	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/internal/envfile"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
	"github.com/YoungsoonLee/korea-data-mcp/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	dbPath := flag.String("db", "korea-data.db", "SQLite database file, created if missing")
	envFile := flag.String("env-file", "", "read OPENDART_API_KEY from a .env file (process environment takes precedence)")
	days := flag.Int("days", 7, "daily: look back this many days for ownership filings")
	limit := flag.Int("limit", 1000, "backfill: companies to collect in this run")
	timeout := flag.Duration("timeout", 30*time.Minute, "per OpenDART request; from abroad large responses take minutes")
	verbose := flag.Bool("v", false, "debug logging")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: korea-data-collect [flags] daily|backfill")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "daily" && flag.Arg(0) != "backfill") {
		flag.Usage()
		os.Exit(2)
	}
	mode := flag.Arg(0)

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	fail := func(msg string, err error) {
		logger.Error(msg, "err", err)
		os.Exit(1)
	}

	var fileEnv map[string]string
	if *envFile != "" {
		var err error
		if fileEnv, err = envfile.Load(*envFile); err != nil {
			fail("cannot read env file", err)
		}
	}
	getenv := envfile.Lookup(fileEnv)
	key := getenv("OPENDART_API_KEY")
	if key == "" {
		fail("OPENDART_API_KEY is not set", nil)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		fail("cannot open database", err)
	}
	defer st.Close()

	// The tools run in this process; quiet their per-call logs.
	toolLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: max(level, slog.LevelWarn)}))
	h := hub.New(&mcp.Implementation{Name: "korea-data-mcp", Version: "collect"}, toolLog)
	dart := opendart.New()
	dart.Timeout = *timeout
	h.Add(dart, getenv)
	session, err := collect.Connect(ctx, h.Server())
	if err != nil {
		fail("cannot start tools", err)
	}
	defer session.Close()

	c := &collect.Collector{
		Store: st,
		Tools: session,
		Companies: func(ctx context.Context) ([]store.Corp, error) {
			list, err := dart.Companies(ctx, key)
			if err != nil {
				return nil, err
			}
			out := make([]store.Corp, len(list))
			for i, e := range list {
				out[i] = store.Corp{CorpCode: e.CorpCode, Name: e.Name, EngName: e.EngName, StockCode: e.StockCode, ModifyDate: e.ModifyDate}
			}
			return out, nil
		},
		Now:    time.Now,
		Logger: logger,
	}

	runID, err := st.StartRun(ctx, mode, time.Now())
	if err != nil {
		fail("cannot record run", err)
	}
	var stats *collect.Stats
	switch mode {
	case "daily":
		stats, err = c.Daily(ctx, *days)
	case "backfill":
		stats, err = c.Backfill(ctx, *limit)
	}
	status := "ok"
	switch {
	case errors.Is(err, collect.ErrStopped):
		status = "partial"
	case err != nil && stats.Corps > 0:
		status = "partial"
	case err != nil:
		status = "failed"
	}
	// Record the run even when interrupted.
	if ferr := st.FinishRun(context.WithoutCancel(ctx), runID, status, stats, err, time.Now()); ferr != nil {
		logger.Error("cannot record run", "err", ferr)
	}
	logger.Info("run finished", "mode", mode, "status", status, "corps", stats.Corps, "tool_calls", stats.ToolCalls,
		"major_holders_added", stats.ReportsAdded[collect.MajorHolders], "insiders_added", stats.ReportsAdded[collect.Insiders],
		"company_events", stats.Events, "errors", stats.Errors)
	if err != nil {
		fail("run ended with errors", err)
	}
}
