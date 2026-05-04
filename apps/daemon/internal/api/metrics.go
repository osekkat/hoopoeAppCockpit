package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

func (s *server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Snapshot())
}

func (s *server) handleMetricsPrometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.metrics.PrometheusText()))
}

func metricRoute(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		if pattern := routeContext.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unknown"
}

func countInFlightJobs(jobList []jobs.Job) int {
	count := 0
	for _, job := range jobList {
		switch job.Status {
		case jobs.StatusQueued, jobs.StatusRunning, jobs.StatusWaitingApproval, jobs.StatusCanceling:
			count++
		}
	}
	return count
}
