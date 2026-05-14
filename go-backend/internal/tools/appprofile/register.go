package appprofile

import (
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/profiles"
	"github.com/auto-developer-orchestrator/backend/internal/tools/desktop"
)

// RegisterAll creates the app_interact and app_profile tools and appends them
// to the tools slice. Only registers if a DesktopProvider is available
// (these tools need sandbox desktop access to execute actions).
func RegisterAll(tools []core.Tool, store *profiles.Store, provider desktop.DesktopProvider, sandboxID func() string) []core.Tool {
	if provider == nil || store == nil {
		return tools
	}

	interact := NewInteractTool(store, provider, sandboxID)
	profile := NewProfileTool(store, interact)

	return append(tools,
		interact,
		profile,
	)
}
