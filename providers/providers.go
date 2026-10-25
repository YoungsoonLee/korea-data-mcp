// Package providers lists every provider compiled into the hub.
package providers

import (
	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/YoungsoonLee/mcp-hub/providers/opendart"
)

// All returns the providers in registration order. Add new providers here.
func All() []hub.Provider {
	return []hub.Provider{
		opendart.New(),
	}
}
