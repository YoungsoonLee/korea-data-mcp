// Package providers lists every provider compiled into the hub.
package providers

import (
	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/providers/datagokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/lawgokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
)

// All returns the providers in registration order. Add new providers here.
func All() []hub.Provider {
	return []hub.Provider{
		opendart.New(),
		datagokr.New(),
		lawgokr.New(),
	}
}
