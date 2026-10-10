package routes

import (
	"net/http"
	"sync/atomic"

	"github.com/gorilla/mux"
)

// settingsStatusHandler is the real GET /api/admin/settings/status handler. It
// needs services (certificates, binary manager, cloud providers) that are only
// built in main.go after SetupRoutes, so main.go plugs it in later via
// SetSettingsStatusHandler. The route itself must be registered inside
// SetupAdminRoutes, before the generic /settings/{key} route: gorilla/mux
// matches in registration order, and registering it later made "status" be
// treated as a setting key ("Setting not found", 404).
var settingsStatusHandler atomic.Value // http.Handler

// SetSettingsStatusHandler installs the handler served at /api/admin/settings/status.
func SetSettingsStatusHandler(h http.Handler) {
	settingsStatusHandler.Store(h)
}

func registerSettingsStatusRoute(adminRouter *mux.Router) {
	adminRouter.HandleFunc("/settings/status", serveSettingsStatus).Methods(http.MethodGet, http.MethodOptions)
}

func serveSettingsStatus(w http.ResponseWriter, r *http.Request) {
	h, _ := settingsStatusHandler.Load().(http.Handler)
	if h == nil {
		http.Error(w, "settings status not available", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// atomicValueReset returns an empty atomic.Value (tests only need a way to clear it).
func atomicValueReset() atomic.Value { return atomic.Value{} }
