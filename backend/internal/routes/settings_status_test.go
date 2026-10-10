package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// The status route must win over the generic /settings/{key} route.
func TestSettingsStatusRouteBeatsGenericKeyRoute(t *testing.T) {
	r := mux.NewRouter()
	registerSettingsStatusRoute(r)
	r.HandleFunc("/settings/{key}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Setting not found", http.StatusNotFound)
	}).Methods(http.MethodGet)

	SetSettingsStatusHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(func() { settingsStatusHandler = atomicValueReset() })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("want status handler, got %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/other_key", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("generic key route should still serve other keys, got %d", rec.Code)
	}
}

func TestSettingsStatusUnavailableBeforeWiring(t *testing.T) {
	settingsStatusHandler = atomicValueReset()
	rec := httptest.NewRecorder()
	serveSettingsStatus(rec, httptest.NewRequest(http.MethodGet, "/settings/status", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 before the handler is installed, got %d", rec.Code)
	}
}

// Guards the ordering inside SetupAdminRoutes itself (the real regression).
func TestSetupAdminRoutesRegistersStatusBeforeGenericKey(t *testing.T) {
	src, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	status := strings.Index(s, "registerSettingsStatusRoute(adminRouter)")
	generic := strings.Index(s, `"/settings/{key}"`)
	if status < 0 || generic < 0 || status > generic {
		t.Fatalf("registerSettingsStatusRoute must be called before the /settings/{key} routes (status=%d generic=%d)", status, generic)
	}
}
