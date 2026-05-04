package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	joblog "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs/log"
	daemonmetrics "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/metrics"
)

const logFinalHeader = "X-Log-Final"
const logNextOffsetHeader = "X-Log-Next-Offset"
const logTotalBytesHeader = "X-Log-Total-Bytes"

func (s *server) handleJobLogChunk(w http.ResponseWriter, r *http.Request) {
	offset, err := parseNonNegativeInt64Query(r, "offset", 0)
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid offset", err.Error())
		return
	}
	maxBytes, err := parseLogMaxBytes(r)
	if err != nil {
		s.writeProblem(w, http.StatusBadRequest, "invalid maxBytes", err.Error())
		return
	}

	jobID := chi.URLParam(r, "jobId")
	start := time.Now()
	chunk, err := s.jobs.ReadLog(r.Context(), jobID, offset, maxBytes)
	_ = s.metrics.ObserveDuration(daemonmetrics.MetricLogFetchDurationSeconds, daemonmetrics.Labels{
		"result": logFetchResult(err),
		"final":  strconv.FormatBool(chunk.Final),
	}, time.Since(start))
	if err != nil {
		s.writeLogJobError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	setLogHeader(w.Header(), "Content-Range", logContentRange(chunk))
	setLogHeader(w.Header(), logTotalBytesHeader, strconv.FormatInt(chunk.TotalBytes, 10))
	setLogHeader(w.Header(), logFinalHeader, strconv.FormatBool(chunk.Final))
	setLogHeader(w.Header(), logNextOffsetHeader, strconv.FormatInt(chunk.NextOffset, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(chunk.Data)
}

func logFetchResult(err error) string {
	if err == nil {
		return "ok"
	}
	return "error"
}

func parseLogMaxBytes(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("maxBytes")
	if raw == "" {
		raw = r.URL.Query().Get("limit")
	}
	if raw == "" {
		return joblog.DefaultReadLimit, nil
	}
	return parseNonNegativeInt64(raw)
}

func parseNonNegativeInt64Query(r *http.Request, name string, fallback int64) (int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	return parseNonNegativeInt64(raw)
}

func parseNonNegativeInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%d is negative", value)
	}
	return value, nil
}

func logContentRange(chunk jobs.LogChunk) string {
	if len(chunk.Data) == 0 {
		return fmt.Sprintf("bytes */%d", chunk.TotalBytes)
	}
	return fmt.Sprintf("bytes %d-%d/%d", chunk.Offset, chunk.NextOffset-1, chunk.TotalBytes)
}

func setLogHeader(header http.Header, key string, value string) {
	header.Set(key, strings.NewReplacer("\r", "", "\n", "").Replace(value))
}

func (s *server) writeLogJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrJobsReaderUnavailable):
		s.writeProblemCode(w, http.StatusServiceUnavailable, "jobs.registry_unavailable", "job registry unavailable", err.Error())
	case errors.Is(err, jobs.ErrNotFound):
		s.writeProblem(w, http.StatusNotFound, "job not found", err.Error())
	case errors.Is(err, jobs.ErrInvalidRequest):
		s.writeProblem(w, http.StatusBadRequest, "invalid job log request", err.Error())
	default:
		s.writeProblem(w, http.StatusInternalServerError, "job log failed", err.Error())
	}
}
