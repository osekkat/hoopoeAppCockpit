package ntm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"nhooyr.io/websocket"
)

// client_live.go owns the NTM live-transport methods on *Adapter —
// every operation that goes through the ntm-serve sidecar (REST GETs,
// SSE event stream, WebSocket event stream) plus the URL/auth/HTTP
// helpers those methods share.
//
// hp-h5yq sixth cut: split out of ntm.go, completing the bead's
// "split into argv.go, intents.go, client_cli.go, client_live.go,
// parsers.go, capabilities.go" plan. Behavior unchanged — same
// package, same exported signatures, same constants. liveURL /
// httpClient / addAuth are still callable from capabilities.go's
// probeLive (same package). Same-package access also keeps SSE
// scanning + WebSocket dialing internal to this file.

func (a *Adapter) LiveSessions(ctx context.Context) (json.RawMessage, error) {
	return a.liveGETAny(ctx, "/v1/sessions", "/api/sessions")
}

func (a *Adapter) LiveSessionDetails(ctx context.Context, sessionID string) (json.RawMessage, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidRequest)
	}
	escaped := url.PathEscape(sessionID)
	return a.liveGETAny(ctx, "/v1/sessions/"+escaped, "/api/sessions/"+escaped)
}

func (a *Adapter) LivePaneState(ctx context.Context, paneID string) (json.RawMessage, error) {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return nil, fmt.Errorf("%w: pane id is required", ErrInvalidRequest)
	}
	escaped := url.PathEscape(paneID)
	return a.liveGETAny(ctx, "/v1/panes/"+escaped+"/state", "/api/panes/"+escaped+"/state")
}

// ReadSSE subscribes to NTM's Server-Sent-Events transport and invokes
// handle for every decoded EventEnvelope. The connection is bound to
// ctx; cancellation closes the stream.
func (a *Adapter) ReadSSE(ctx context.Context, path string, handle func(EventEnvelope) error) error {
	if handle == nil {
		return fmt.Errorf("%w: event handler is required", ErrInvalidRequest)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL(path), nil)
	if err != nil {
		return err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrLiveUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: SSE status %d", ErrLiveUnavailable, resp.StatusCode)
	}
	return parseSSE(resp.Body, handle)
}

// ReadWebSocket subscribes to NTM's WebSocket transport and invokes
// handle for every decoded EventEnvelope. The connection closes
// cleanly on ctx cancellation; transport-level errors propagate.
func (a *Adapter) ReadWebSocket(ctx context.Context, path string, handle func(EventEnvelope) error) error {
	if handle == nil {
		return fmt.Errorf("%w: event handler is required", ErrInvalidRequest)
	}
	header := http.Header{}
	if a.LiveToken != "" {
		header.Set("Authorization", "Bearer "+a.LiveToken)
	}
	conn, _, err := websocket.Dial(ctx, wsURL(a.liveURL(path)), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return fmt.Errorf("%w: websocket dial: %w", ErrLiveUnavailable, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		var event EventEnvelope
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("ntm: decode websocket event: %w", err)
		}
		if err := handle(event); err != nil {
			return err
		}
	}
}

func (a *Adapter) liveGET(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL(path), nil)
	if err != nil {
		return nil, err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrLiveUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: GET %s status %d", ErrLiveUnavailable, path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(a.maxStdoutBytes()+1)))
	if err != nil {
		return nil, fmt.Errorf("ntm: read live response: %w", err)
	}
	if max := a.maxStdoutBytes(); max > 0 && len(data) > max {
		return nil, outputTooLargeError{limit: max, got: len(data), argv: []string{"GET", path}}
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("ntm: decode live JSON: %w", err)
	}
	return raw, nil
}

// liveGETAny tries each path in order and returns the first
// successful response. Used by adapters that have to support both the
// /v1/* and /api/* prefixes during the ntm-serve URL migration.
func (a *Adapter) liveGETAny(ctx context.Context, paths ...string) (json.RawMessage, error) {
	var lastErr error
	for _, path := range paths {
		raw, err := a.liveGET(ctx, path)
		if err == nil {
			return raw, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no live paths configured", ErrLiveUnavailable)
	}
	return nil, lastErr
}

// parseSSE reads an SSE stream line by line, accumulating data: lines
// into a single payload that flushes on a blank-line delimiter. Used
// by ReadSSE; isolated as a pure func so SSE-handling tests don't
// need a live transport.
func parseSSE(reader io.Reader, handle func(EventEnvelope) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		var event EventEnvelope
		if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
			return fmt.Errorf("ntm: decode SSE event: %w", err)
		}
		data.Reset()
		return handle(event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

// liveURL composes a full URL from the configured LiveBaseURL and a
// relative path, defaulting to the standard ntm-serve loopback when
// no base is configured.
func (a *Adapter) liveURL(path string) string {
	base := strings.TrimRight(a.LiveBaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:7337"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (a *Adapter) httpClient() HTTPDoer {
	if a != nil && a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *Adapter) addAuth(req *http.Request) {
	if a != nil && a.LiveToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.LiveToken)
	}
}

// wsURL converts an http(s):// URL to its ws(s):// equivalent.
func wsURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}
