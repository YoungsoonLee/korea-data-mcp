// Package hub defines the conventions every korea-data-mcp provider follows:
// provider registration, tool naming, error shape and pagination.
// See docs/CONVENTIONS.md.
package hub

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Provider wraps one upstream service (OpenDART, Naver Search, ...).
type Provider interface {
	Info() ProviderInfo
	// Register adds the provider's tools. It is called only when every
	// RequiredEnv variable is set.
	Register(r *Registrar) error
}

type ProviderInfo struct {
	ID          string   // lowercase [a-z0-9]+, also the tool name prefix
	Title       string   // e.g. "OpenDART (전자공시)"
	Description string   // one line, shown by hub_list_providers
	RequiredEnv []string // e.g. ["OPENDART_API_KEY"]
	DocsURL     string   // where to obtain a key
}

type ProviderStatus struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools,omitempty"`
	MissingEnv  []string `json:"missing_env,omitempty"`
	DocsURL     string   `json:"docs_url,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type Hub struct {
	server    *mcp.Server
	logger    *slog.Logger
	providers []*ProviderStatus
}

func New(impl *mcp.Implementation, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Hub{server: mcp.NewServer(impl, nil), logger: logger}
	h.addBuiltinTools()
	return h
}

func (h *Hub) Server() *mcp.Server { return h.server }

// Add registers p if its required environment is present; otherwise it is
// recorded as disabled so agents can see what is missing.
func (h *Hub) Add(p Provider, getenv func(string) string) {
	info := p.Info()
	if !idPattern.MatchString(info.ID) {
		panic(fmt.Sprintf("hub: invalid provider id %q", info.ID))
	}
	st := &ProviderStatus{ID: info.ID, Title: info.Title, Description: info.Description, DocsURL: info.DocsURL}
	h.providers = append(h.providers, st)

	for _, k := range info.RequiredEnv {
		if getenv(k) == "" {
			st.MissingEnv = append(st.MissingEnv, k)
		}
	}
	if len(st.MissingEnv) > 0 {
		h.logger.Info("provider disabled", "provider", info.ID, "missing_env", st.MissingEnv)
		return
	}
	r := &Registrar{hub: h, info: info, status: st, getenv: getenv}
	if err := p.Register(r); err != nil {
		st.Error = err.Error()
		h.logger.Error("provider registration failed", "provider", info.ID, "err", err)
		return
	}
	st.Enabled = true
	h.logger.Info("provider enabled", "provider", info.ID, "tools", len(st.Tools))
}

func (h *Hub) Providers() []ProviderStatus {
	out := make([]ProviderStatus, len(h.providers))
	for i, p := range h.providers {
		out[i] = *p
	}
	return out
}

// Registrar is handed to Provider.Register.
type Registrar struct {
	hub    *Hub
	info   ProviderInfo
	status *ProviderStatus
	getenv func(string) string
}

func (r *Registrar) Env(key string) string { return r.getenv(key) }
func (r *Registrar) Logger() *slog.Logger  { return r.hub.logger.With("provider", r.info.ID) }
func (r *Registrar) ProviderID() string    { return r.info.ID }

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+$`)
	toolNamePattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)+$`)
)

// AddTool registers a typed tool after enforcing conventions:
//   - name is "<provider>_<verb>_<noun>" in snake_case
//   - annotations default to read-only, open-world
//   - any returned error is normalized to *Error tagged with the provider
func AddTool[In, Out any](r *Registrar, t *mcp.Tool, h func(ctx context.Context, in In) (Out, error)) {
	if !toolNamePattern.MatchString(t.Name) || !strings.HasPrefix(t.Name, r.info.ID+"_") {
		panic(fmt.Sprintf("hub: tool name %q must be snake_case and start with %q", t.Name, r.info.ID+"_"))
	}
	if t.Description == "" {
		panic(fmt.Sprintf("hub: tool %q needs a description", t.Name))
	}
	if t.Annotations == nil {
		openWorld := true
		t.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &openWorld}
	}

	logger := r.Logger().With("tool", t.Name)
	providerID := r.info.ID
	mcp.AddTool(r.hub.server, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		out, err := h(ctx, in)
		if err != nil {
			he := AsError(err)
			if he.Provider == "" {
				he.Provider = providerID
			}
			logger.Warn("tool error", "code", he.Code, "msg", he.Message, "dur", time.Since(start))
			var zero Out
			return nil, zero, he
		}
		logger.Debug("tool ok", "dur", time.Since(start))
		return nil, out, nil
	})
	r.status.Tools = append(r.status.Tools, t.Name)
}

type listProvidersOutput struct {
	Providers []ProviderStatus `json:"providers"`
}

func (h *Hub) addBuiltinTools() {
	mcp.AddTool(h.server, &mcp.Tool{
		Name: "hub_list_providers",
		Description: "List the Korean service providers in this hub, whether each is enabled, its tools, " +
			"and which environment variables (API keys) are missing for disabled ones.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, listProvidersOutput, error) {
		return nil, listProvidersOutput{Providers: h.Providers()}, nil
	})
}
