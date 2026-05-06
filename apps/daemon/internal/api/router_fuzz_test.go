package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// FuzzRouter exercises NewRouter(Config{}) — the default-normalized router
// — across mutated method/path/query/body inputs. Oracle:
//
//   - Never panics. (Fuzz default; the table-recover guards httptest's own
//     preconditions so panics surface from router code, not constructor noise.)
//   - HTTP status falls in the documented [100, 599] range.
//   - Response body never exceeds responseSizeLimit.
//   - Error paths returning a body content-typed application/problem+json
//     parse as JSON and carry the schemas.Problem shape.
//   - A FUZZ_SECRET_SHIBBOLETH planted in the request never round-trips
//     verbatim into the response — confirms the router doesn't echo
//     query/body content into problem payloads.
func FuzzRouter(f *testing.F) {
	seeds := []struct {
		method string
		path   string
		query  string
		body   string
	}{
		{"GET", "/health", "", ""},
		{"GET", "/v1/health", "", ""},
		{"GET", "/v1/readiness", "", ""},
		{"GET", "/v1/version", "", ""},
		{"GET", "/v1/jobs", "", ""},
		{"GET", "/v1/jobs", "limit=10", ""},
		{"GET", "/v1/jobs", "limit=abc", ""},
		{"GET", "/v1/events/replay", "", ""},
		{"GET", "/v1/events/replay", "channel=jobs&since=42", ""},
		{"GET", "/v1/diagnostics/metrics", "", ""},
		{"GET", "/v1/diagnostics/metrics/prometheus", "", ""},
		{"POST", "/v1/auth/bootstrap/bearer", "", `{"pairing":"H-ABCDEFGHIJK"}`},
		{"POST", "/v1/auth/session/revoke", "", "{}"},
		{"DELETE", "/v1/health", "", ""},
		{"PATCH", "/v1/jobs", "", ""},
		{"PUT", "/", "", ""},
		{"OPTIONS", "/v1/health", "", ""},
		{"GET", "/", "", ""},
		{"GET", "/notfound", "", ""},
		{"GET", "/v1/../etc/passwd", "", ""},
		{"GET", "/v1/health", "%XX", ""},
		{"GET", "/v1/health/../../jobs", "", ""},
	}
	for _, s := range seeds {
		f.Add(s.method, s.path, s.query, s.body)
	}

	router := NewRouter(Config{
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})

	const (
		fuzzSecret = "FUZZ_SECRET_SHIBBOLETH_42"
		// responseSizeLimit caps the per-request response so we'd notice an
		// accidental dump of unbounded state into a problem payload. The
		// bound is generous: /v1/diagnostics/metrics/prometheus legitimately
		// emits ~100KB once telemetry counters accumulate, and the JSON
		// counterpart is similar order. 1 MiB still catches truly unbounded
		// pathological responses while leaving headroom for legitimate
		// observability blobs.
		responseSizeLimit = 1 << 20
	)

	f.Fuzz(func(t *testing.T, method string, path string, rawQuery string, body string) {
		if !validHTTPToken(method) || method == "" {
			return
		}
		if !validRequestPath(path) {
			return
		}
		if !validQueryString(rawQuery) {
			return
		}
		if len(method) > 16 || len(path) > 512 || len(rawQuery) > 1024 || len(body) > 8192 {
			return
		}
		// Plant the shibboleth in body when present, else in the query
		// string. Constructing *http.Request directly (instead of via
		// httptest.NewRequest) sidesteps url.Parse's strict pre-validation
		// — the fuzzer's job is to exercise chi's matcher, not net/url's
		// RFC compliance.
		injected := body
		if injected == "" && rawQuery == "" {
			rawQuery = "shib=" + url.QueryEscape(fuzzSecret)
		} else if injected != "" {
			injected += "\n" + fuzzSecret
		}
		req := &http.Request{
			Method:     method,
			URL:        &url.URL{Path: path, RawQuery: rawQuery},
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Host:       "127.0.0.1",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(injected)),
			RequestURI: path,
		}
		req = req.WithContext(context.Background())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		fullPath := path
		if rawQuery != "" {
			fullPath = path + "?" + rawQuery
		}

		if rec.Code < 100 || rec.Code >= 600 {
			t.Fatalf("router returned status %d outside [100,600) for %s %s", rec.Code, method, fullPath)
		}

		respBody := rec.Body.Bytes()
		if len(respBody) > responseSizeLimit {
			t.Fatalf("router returned %d-byte body for %s %s — exceeds %d cap", len(respBody), method, fullPath, responseSizeLimit)
		}

		// Problem+json contract: any response advertising application/problem+json
		// must parse as JSON and contain at least the canonical fields. JSON
		// responses (handler-driven, not problem) follow the same parse rule
		// but no further field constraint here.
		ct := rec.Header().Get("Content-Type")
		if strings.HasPrefix(ct, "application/problem+json") {
			var doc map[string]any
			if err := json.Unmarshal(respBody, &doc); err != nil {
				t.Fatalf("problem+json body did not parse for %s %s: %v\nbody=%q", method, fullPath, err, respBody)
			}
			if _, ok := doc["type"].(string); !ok {
				t.Fatalf("problem+json missing string type field for %s %s: %v", method, fullPath, doc)
			}
			if _, ok := doc["status"].(float64); !ok {
				t.Fatalf("problem+json missing numeric status field for %s %s: %v", method, fullPath, doc)
			}
		} else if strings.HasPrefix(ct, "application/json") {
			var doc any
			if err := json.Unmarshal(respBody, &doc); err != nil && len(respBody) > 0 {
				t.Fatalf("application/json body did not parse for %s %s: %v\nbody=%q", method, fullPath, err, respBody)
			}
		}

		if strings.Contains(string(respBody), fuzzSecret) {
			t.Fatalf("router echoed shibboleth verbatim into response for %s %s\nbody=%q", method, fullPath, respBody)
		}
	})
}

// validHTTPToken matches RFC 7230 token (method names allow only token chars).
// httptest.NewRequest is lenient but chi's matcher is not — we filter so the
// fuzz inputs reach the router's actual code path rather than tripping on
// pre-router validation.
func validHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z',
			c >= 'a' && c <= 'z',
			c >= '0' && c <= '9',
			c == '!', c == '#', c == '$', c == '%', c == '&',
			c == '\'', c == '*', c == '+', c == '-', c == '.',
			c == '^', c == '_', c == '`', c == '|', c == '~':
		default:
			return false
		}
	}
	return true
}

// validQueryString rejects characters that httptest.NewRequest's URL parser
// treats as request-line delimiters (whitespace splits "/path? HTTP/1.0"
// into a malformed-version panic). Real HTTP clients percent-encode these,
// but the fuzzer produces raw bytes — we keep the assertions on chi's
// matcher rather than on httptest's preconditions.
func validQueryString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
		if c == ' ' || c == '#' {
			return false
		}
	}
	return true
}

func validRequestPath(s string) bool {
	if s == "" || s[0] != '/' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
		// httptest.NewRequest passes the URL through url.Parse; reject the
		// fragment delimiter and bare spaces so the parser doesn't gobble
		// the rest of the path.
		if c == ' ' || c == '#' {
			return false
		}
	}
	// Reject paths with an embedded scheme — httptest treats those as
	// absolute URLs and would route to a different host/port construct.
	if strings.HasPrefix(s, "//") {
		return false
	}
	return true
}

// Compile-time guard: chi.Router exposes ServeHTTP via http.Handler, but we
// also check NewRouter still returns one so a future refactor can't
// silently break this fuzzer.
var _ http.Handler = NewRouter(Config{Now: func() time.Time { return time.Time{} }})
