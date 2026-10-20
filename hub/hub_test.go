package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Query string `json:"query" jsonschema:"text to echo"`
	PageParams
}

type echoProvider struct{}

func (echoProvider) Info() ProviderInfo {
	return ProviderInfo{ID: "echo", Title: "Echo", RequiredEnv: []string{"ECHO_KEY"}}
}

func (echoProvider) Register(r *Registrar) error {
	AddTool(r, &mcp.Tool{Name: "echo_search_items", Description: "echo"},
		func(_ context.Context, in echoInput) (List[string], error) {
			if in.Query == "" {
				return List[string]{}, Errorf(CodeInvalidArgument, "query is required").WithHint("pass a non-empty query")
			}
			page, size, err := in.Resolve(10, 50)
			if err != nil {
				return List[string]{}, err
			}
			return List[string]{Items: []string{in.Query + ":" + r.Env("ECHO_KEY")}, PageInfo: NewPageInfo(page, size, 25)}, nil
		})
	return nil
}

func connect(t *testing.T, h *Hub) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := h.Server().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func newHub(env map[string]string) *Hub {
	h := New(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	h.Add(echoProvider{}, func(k string) string { return env[k] })
	return h
}

func callJSON(t *testing.T, cs *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(*mcp.TextContent).Text
}

func TestToolSuccessAndPagination(t *testing.T) {
	cs := connect(t, newHub(map[string]string{"ECHO_KEY": "k"}))
	res, _ := callJSON(t, cs, "echo_search_items", map[string]any{"query": "삼성전자", "page": 2})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out List[string]
	json.Unmarshal(b, &out)
	if out.Items[0] != "삼성전자:k" || out.PageInfo != (PageInfo{Page: 2, PageSize: 10, TotalCount: 25, HasNext: true}) {
		t.Fatalf("got %+v", out)
	}
}

func TestToolErrorIsStructured(t *testing.T) {
	cs := connect(t, newHub(map[string]string{"ECHO_KEY": "k"}))
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"query": ""}, "query is required"},
		{map[string]any{"query": "x", "page_size": 500}, "page_size must be between 1 and 50"},
	} {
		res, text := callJSON(t, cs, "echo_search_items", tc.args)
		if !res.IsError {
			t.Fatalf("%v: expected error", tc.args)
		}
		var e Error
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			t.Fatalf("error content is not JSON: %q", text)
		}
		if e.Code != CodeInvalidArgument || e.Provider != "echo" || !strings.Contains(e.Message, tc.want) {
			t.Fatalf("got %+v", e)
		}
	}
}

func TestMissingEnvDisablesProvider(t *testing.T) {
	h := newHub(nil)
	cs := connect(t, h)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "hub_list_providers" {
		t.Fatalf("expected only builtin tool, got %d tools", len(tools.Tools))
	}
	_, text := callJSON(t, cs, "hub_list_providers", map[string]any{})
	if !strings.Contains(text, `"missing_env":["ECHO_KEY"]`) || !strings.Contains(text, `"enabled":false`) {
		t.Fatalf("got %s", text)
	}
}

func TestToolNameConvention(t *testing.T) {
	h := New(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	r := &Registrar{hub: h, info: ProviderInfo{ID: "echo"}, status: &ProviderStatus{}}
	for _, name := range []string{"search", "other_search", "echo_Search", "echo-search"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: expected panic", name)
				}
			}()
			AddTool(r, &mcp.Tool{Name: name, Description: "d"}, func(context.Context, struct{}) (struct{}, error) {
				return struct{}{}, nil
			})
		}()
	}
}

func TestAsError(t *testing.T) {
	if e := AsError(context.DeadlineExceeded); e.Code != CodeUnavailable || !e.Retryable {
		t.Errorf("deadline: %+v", e)
	}
	if e := AsError(errors.New("boom")); e.Code != CodeInternal || e.Retryable {
		t.Errorf("plain: %+v", e)
	}
	orig := Errorf(CodeNotFound, "x")
	if AsError(errors.Join(orig)) != orig {
		t.Error("wrapped *Error not preserved")
	}
}
