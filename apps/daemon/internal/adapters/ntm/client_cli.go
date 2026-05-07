package ntm

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// client_cli.go owns the NTM CLI-transport methods on *Adapter — every
// public read/mutate operation that runs the ntm binary through the
// configured Runner and parses its stdout JSON.
//
// hp-h5yq fifth cut: split out of ntm.go to continue the size-outlier
// reduction. The transport helpers (run / runText / runRawJSON) and
// the Adapter struct itself stay in ntm.go because they're shared
// substrate. Behavior unchanged — same package, same exported
// signatures, same constants. The §18.3 golden-fixture contract
// tests keep pinning the parsed-output shapes.

func (a *Adapter) SessionsList(ctx context.Context) (SessionsResponse, error) {
	raw, err := a.runRawJSON(ctx, SessionsListArgv())
	if err != nil {
		return SessionsResponse{}, err
	}
	return ParseSessionsResponse(raw)
}

func (a *Adapter) SessionDetails(ctx context.Context, session string) (json.RawMessage, error) {
	argv, err := SessionDetailsArgv(session)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Snapshot(ctx context.Context) (Snapshot, error) {
	raw, err := a.runRawJSON(ctx, SnapshotArgv())
	if err != nil {
		return Snapshot{}, err
	}
	return ParseSnapshot(raw)
}

func (a *Adapter) Status(ctx context.Context) (Snapshot, error) {
	raw, err := a.runRawJSON(ctx, StatusArgv())
	if err != nil {
		return Snapshot{}, err
	}
	return ParseSnapshot(raw)
}

func (a *Adapter) Triage(ctx context.Context) (json.RawMessage, error) {
	return a.runRawJSON(ctx, TriageArgv())
}

func (a *Adapter) Tail(ctx context.Context, req TailRequest) (TailResponse, error) {
	argv, err := TailArgv(req)
	if err != nil {
		return TailResponse{}, err
	}
	raw, err := a.runRawJSON(ctx, argv)
	if err != nil {
		return TailResponse{}, err
	}
	return ParseTailResponse(raw)
}

func (a *Adapter) Activity(ctx context.Context, session string) (json.RawMessage, error) {
	argv, err := ActivityArgv(session)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Spawn(ctx context.Context, req SpawnRequest) (json.RawMessage, error) {
	argv, err := SpawnArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Send(ctx context.Context, req SendRequest) (json.RawMessage, error) {
	argv, err := SendArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Wait(ctx context.Context, req WaitRequest) (json.RawMessage, error) {
	argv, err := WaitArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Interrupt(ctx context.Context, req InterruptRequest) (json.RawMessage, error) {
	argv, err := InterruptArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) ApprovalsList(ctx context.Context) (json.RawMessage, error) {
	return a.runRawJSON(ctx, ApprovalsListArgv())
}

func (a *Adapter) ApprovalShow(ctx context.Context, token string) (json.RawMessage, error) {
	argv, err := ApprovalShowArgv(token)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Approve(ctx context.Context, req ApprovalRequest) (json.RawMessage, error) {
	argv, err := ApproveArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Deny(ctx context.Context, req ApprovalRequest) (json.RawMessage, error) {
	argv, err := DenyArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

// CapturePaneFallback shells out to `tmux capture-pane` directly when
// the NTM-streamed pane delta path is unavailable. Used by the
// Diagnostics 'Show raw pane' debug toggle (Guardrail 12 — never
// surfaced in default UI) and by audited forensic capture before
// agent.kill_wedged_process actions.
func (a *Adapter) CapturePaneFallback(ctx context.Context, req TmuxCaptureRequest) (PaneChunk, error) {
	argv, err := TmuxCaptureArgv(req)
	if err != nil {
		return PaneChunk{}, err
	}
	data, err := a.runText(ctx, argv)
	if err != nil {
		return PaneChunk{}, err
	}
	return PaneChunk{
		PaneID:     strings.TrimSpace(req.TargetPane),
		Offset:     0,
		Bytes:      append([]byte(nil), data...),
		Length:     len(data),
		Source:     "tmux-capture-pane:fallback-mode",
		CapturedAt: a.now().UTC().Format(time.RFC3339Nano),
	}, nil
}
