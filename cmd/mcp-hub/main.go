// Command mcp-hub serves all configured Korean service providers as one MCP
// server, over stdio (default) or streamable HTTP (-http :8080).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/providers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set by release builds (-ldflags "-X main.version=..."); go install
// builds take it from the module version instead.
var version = "dev"

func main() {
	httpAddr := flag.String("http", "", "serve streamable HTTP on this address instead of stdio")
	only := flag.String("providers", "", "comma-separated provider IDs to enable (default: all with keys set)")
	envFile := flag.String("env-file", "", "read API keys from a .env file (process environment takes precedence)")
	verbose := flag.Bool("v", false, "debug logging")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
	if *showVersion {
		fmt.Println("mcp-hub", version)
		return
	}

	// stdout is the MCP stdio channel, so logs must go to stderr.
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	var fileEnv map[string]string
	if *envFile != "" {
		var err error
		if fileEnv, err = loadEnvFile(*envFile); err != nil {
			logger.Error("cannot read env file", "err", err)
			os.Exit(1)
		}
	}
	getenv := envLookup(fileEnv)

	h := hub.New(&mcp.Implementation{
		Name:    "mcp-hub",
		Title:   "MCP Hub for Korean services",
		Version: version,
	}, logger)

	var allow []string
	if *only != "" {
		allow = strings.Split(*only, ",")
	}
	for _, p := range providers.All() {
		if allow == nil || slices.Contains(allow, p.Info().ID) {
			h.Add(p, getenv)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr == "" {
		if err := h.Server().Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
			logger.Error("server stopped", "err", err)
			os.Exit(1)
		}
		return
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.Server() }, nil)
	srv := &http.Server{Addr: *httpAddr, Handler: handler}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	logger.Info("listening", "addr", *httpAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
