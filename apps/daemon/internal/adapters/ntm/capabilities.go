package ntm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

// capabilities.go owns the NTM capability probe + classification
// helpers that build a capabilities.ToolReport from CLI/HTTP probes.
//
// hp-h5yq fourth cut: split out of ntm.go. The Probe receiver method
// is the public entry point (still on *Adapter); statusForError /
// missingCapabilities / blockPolicyCapabilities / probeTail /
// rawReportsFailure / versionAtLeast / probeLive are the helpers it
// composes. Keeping them grouped in this file makes the
// "what does Probe assert?" question answerable in one place.
//
// Behavior unchanged — same package, same exported signatures, same
// constants. The §18.3 golden-fixture contract tests (hp-jyw, hp-k4c,
// hp-nar, hp-a3r, hp-gc2) keep pinning Probe's outputs.

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolNTM,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities:  missingCapabilities("not probed"),
	}
	versionText, err := a.runText(ctx, VersionArgv())
	if err != nil {
		state := statusForError(err)
		for capID, cap := range report.Capabilities {
			cap.Status = state
			cap.Notes = err.Error()
			report.Capabilities[capID] = cap
		}
		blockPolicyCapabilities(report)
		return report, nil
	}
	version := ParseVersion(string(versionText))
	report.Version = version
	report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusOK}
	if !versionAtLeast(version, 1, 5) {
		note := fmt.Sprintf("%v: observed %q, min-compatible is 1.5", ErrUnsupportedVersion, version)
		for capID, cap := range report.Capabilities {
			cap.Status = capabilities.StatusMissing
			cap.Notes = note
			report.Capabilities[capID] = cap
		}
		blockPolicyCapabilities(report)
		return report, nil
	}
	blockPolicyCapabilities(report)
	report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "http", Notes: "ntm serve not configured"}
	report.Capabilities[CapabilityServeSSE] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "sse", Notes: "ntm serve not configured"}
	report.Capabilities[CapabilityServeWS] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "ws", Notes: "ntm serve not configured"}
	if a.LiveBaseURL != "" {
		if err := a.probeLive(ctx); err == nil {
			report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "http"}
			report.Capabilities[CapabilityServeSSE] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "sse", Notes: "stream probe deferred until subscription"}
			report.Capabilities[CapabilityServeWS] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "ws", Notes: "stream probe deferred until subscription"}
			report.Capabilities[CapabilityPanesStream] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "ws,sse", Notes: "ntm serve live stream available; websocket preferred"}
		} else {
			report.Capabilities[CapabilityServeREST] = capabilities.Capability{Status: capabilities.StatusDegraded, Transport: "http", Notes: err.Error()}
		}
	}

	if _, err := a.SessionsList(ctx); err != nil {
		report.Capabilities[CapabilitySessionsList] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilitySessionsList] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	snapshot, err := a.Snapshot(ctx)
	if err != nil {
		report.Capabilities[CapabilityRobotSnapshot] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotSnapshot] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
		if len(snapshot.Sessions) > 0 {
			tailCap := probeTail(ctx, a, snapshot.Sessions[0].SessionID())
			report.Capabilities[CapabilityRobotTail] = tailCap
			if tailCap.Status == capabilities.StatusOK && report.Capabilities[CapabilityPanesStream].Status != capabilities.StatusOK {
				report.Capabilities[CapabilityPanesStream] = capabilities.Capability{
					Status:    capabilities.StatusOK,
					Transport: "poll",
					Fallback:  CapabilityRobotTail,
					Notes:     "live stream unavailable; using bounded ntm.robot.tail polling",
				}
			}
		} else {
			report.Capabilities[CapabilityRobotTail] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "no active sessions to tail"}
		}
	}
	if _, err := a.Status(ctx); err != nil {
		report.Capabilities[CapabilityRobotStatus] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotStatus] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	if _, err := a.Triage(ctx); err != nil {
		report.Capabilities[CapabilityRobotTriage] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityRobotTriage] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	report.Capabilities[CapabilityRobotActivity] = capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "requires active session"}
	if raw, err := a.ApprovalsList(ctx); err != nil {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else if note, failed := rawReportsFailure(raw); failed {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: note, Transport: "stdio"}
	} else {
		report.Capabilities[CapabilityApprovalsList] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	return report, nil
}

// probeLive issues a GET /health against the configured live base URL
// to confirm the ntm-serve sidecar is reachable. A 2xx response means
// the live transport family (REST + SSE + WS) is at least up; the
// stream-specific probes deferred until subscription.
func (a *Adapter) probeLive(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.liveURL("/health"), nil)
	if err != nil {
		return err
	}
	a.addAuth(req)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// statusForError classifies a runner error into the capabilities
// status vocabulary. The §18.3 contract dictates: exit 127 (command
// not found) maps to Missing; exit 124 (timeout) and decode failures
// map to Degraded; "executable not found" syscall errors are Missing.
// Anything else collapses to Degraded so a probe never silently
// returns Healthy on an error path.
func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		switch commandErr.result.ExitCode {
		case 124:
			return capabilities.StatusDegraded
		case 127:
			return capabilities.StatusMissing
		default:
			return capabilities.StatusDegraded
		}
	}
	if errors.Is(err, ErrOutputTooLarge) || strings.Contains(err.Error(), "decode JSON") {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

// missingCapabilities returns a fresh capability map with every
// declared NTM capability set to StatusMissing + the supplied note.
// Used as the Probe baseline before any specific capability probe
// runs.
func missingCapabilities(note string) map[string]capabilities.Capability {
	caps := map[string]capabilities.Capability{}
	for _, capID := range []string{
		CapabilityPresent,
		CapabilitySessionsList,
		CapabilitySessionsSpawn,
		CapabilitySessionsTerminate,
		CapabilitySessionsAttach,
		CapabilityPanesStream,
		CapabilityServeREST,
		CapabilityServeSSE,
		CapabilityServeWS,
		CapabilityRobotSnapshot,
		CapabilityRobotStatus,
		CapabilityRobotTail,
		CapabilityRobotTriage,
		CapabilityRobotActivity,
		CapabilityApprovalsList,
		CapabilityApprovalsApprove,
		CapabilityApprovalsDeny,
		CapabilitySwarmHalt,
		CapabilitySpawn,
		CapabilitySendMarchingOrders,
		CapabilityPaneKill,
	} {
		caps[capID] = capabilities.Capability{Status: capabilities.StatusMissing, Notes: note}
	}
	return caps
}

// blockPolicyCapabilities marks every mutating NTM capability as
// StatusBlockedByPolicy in the report, regardless of whether the CLI
// itself supports them. Mutation goes through the daemon's
// ActionPlan/job policy gate (§8.3.1), not through direct adapter
// calls.
func blockPolicyCapabilities(report *capabilities.ToolReport) {
	for _, capID := range []string{
		CapabilitySessionsSpawn,
		CapabilitySessionsTerminate,
		CapabilitySessionsAttach,
		CapabilityApprovalsApprove,
		CapabilityApprovalsDeny,
		CapabilitySwarmHalt,
		CapabilitySpawn,
		CapabilitySendMarchingOrders,
		CapabilityPaneKill,
	} {
		report.Capabilities[capID] = capabilities.Capability{
			Status: capabilities.StatusBlockedByPolicy,
			Notes:  "mutating NTM operation; executable only through daemon ActionPlan/job policy",
		}
	}
}

// probeTail issues a 1-line ntm.robot.tail to verify the bounded-poll
// fallback is available when the live stream isn't. Untested when no
// session is provided; Missing/Degraded inherit from statusForError.
func probeTail(ctx context.Context, adapter *Adapter, session string) capabilities.Capability {
	if strings.TrimSpace(session) == "" {
		return capabilities.Capability{Status: capabilities.StatusUntested, Transport: "stdio", Notes: "no session id available"}
	}
	if _, err := adapter.Tail(ctx, TailRequest{Session: session, Lines: 1}); err != nil {
		return capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	}
	return capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
}

// rawReportsFailure parses the {success, error} envelope NTM uses for
// JSON command outputs. Returns (note, true) when the payload reports
// success=false or non-empty error, so capability probes can mark the
// related capability Degraded rather than OK.
func rawReportsFailure(raw json.RawMessage) (string, bool) {
	var response struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", false
	}
	if response.Success != nil && !*response.Success {
		note := strings.TrimSpace(response.Error)
		if note == "" {
			note = "response reported success=false"
		}
		return note, true
	}
	if strings.TrimSpace(response.Error) != "" {
		return strings.TrimSpace(response.Error), true
	}
	return "", false
}

// versionAtLeast checks whether the parsed NTM version satisfies the
// minimum (major, minor) the daemon supports. Returns false on parse
// failure so capability gating is conservative.
func versionAtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	gotMajor, err := strconv.Atoi(strings.TrimLeft(parts[0], "v"))
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}
