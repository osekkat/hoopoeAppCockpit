package api

import (
	"net/http"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
)

const bindSafetyReportPath = "/v1/security/bind-safety"

func WithBindSafetyReport(next http.Handler, report security.BindReport) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	mux.HandleFunc(bindSafetyReportPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.Handle("/", next)
	return mux
}
