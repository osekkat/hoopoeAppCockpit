package api

import (
	"net/http"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security"
)

const bindSafetyReportPath = "/v1/security/bind-safety"

func WithBindSafetyReport(next http.Handler, report security.BindReport) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeProblemCode(w, http.StatusNotFound, "route.not_found", "route not found", "no route matched "+r.URL.Path)
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc(bindSafetyReportPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeProblemCode(
				w,
				http.StatusMethodNotAllowed,
				"route.method_not_allowed",
				"method not allowed",
				"GET is the only supported method for "+bindSafetyReportPath,
			)
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.Handle("/", next)
	return mux
}
