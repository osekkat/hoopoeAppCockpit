package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDaemonUpgradeRouteMountsConfiguredHandler(t *testing.T) {
	calls := 0
	router := NewRouter(Config{
		Upgrade: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/v1/bootstrap/daemon/upgrade" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
		}),
	})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/v1/bootstrap/daemon/upgrade", strings.NewReader("{}"))
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s status = %d body=%s", method, rec.Code, rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
}
