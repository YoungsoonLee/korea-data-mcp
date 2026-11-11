package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Session calls the tools of an MCP server running in the same process.
type Session struct {
	cs *mcp.ClientSession
}

// Connect connects to srv over an in-memory transport.
func Connect(ctx context.Context, srv *mcp.Server) (*Session, error) {
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		return nil, err
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "korea-data-collect", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	return &Session{cs: cs}, nil
}

func (s *Session) Close() error { return s.cs.Close() }

func (s *Session) Call(ctx context.Context, name string, args, out any) error {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err
	}
	if res.IsError {
		var text string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				text += t.Text
			}
		}
		// hub tools put a JSON *hub.Error in the text.
		var he hub.Error
		if json.Unmarshal([]byte(text), &he) == nil && he.Code != "" {
			return &he
		}
		return errors.New(text)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%s: decode output: %w", name, err)
	}
	return nil
}
