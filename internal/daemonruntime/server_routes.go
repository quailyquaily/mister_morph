package daemonruntime

import (
	"net/http"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
)

// routeRegistration is the immutable registration snapshot shared by endpoint
// domains. Mutable state stays inside the domain that owns it.
type routeRegistration struct {
	mux             *http.ServeMux
	options         RoutesOptions
	mode            string
	paths           runtimepaths.Paths
	statePaths      runtimeStatePaths
	fileCacheLimits filecache.Limits
	startedAt       time.Time
	authToken       string
	instanceID      string
	settingsReader  agentsettings.Reader
}

func (routes *routeRegistration) register() {
	routes.registerSystemRoutes()
	routes.registerStateRoutes()
	routes.registerApprovalRoutes()
	routes.registerTaskRoutes()
	routes.registerWorkspaceRoutes()
}
