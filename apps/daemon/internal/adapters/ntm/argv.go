package ntm

import (
	"fmt"
	"strconv"
	"strings"
)

// argv.go owns the NTM CLI argv builders.
//
// hp-h5yq: ntm.go was 1444 lines mixing argv builders, action-intent
// constructors, transport plumbing (CLI / REST / SSE / WS), parsers,
// and capability probes. A single change in any of those concerns
// forced editing one oversized file, raising regression risk and
// making focused review harder. Splitting `*Argv` builders into this
// narrow file is the first step of the bead's "split without behavior
// change" plan; signatures are unchanged so existing callers and
// golden-fixture contract tests continue to pin them.

func VersionArgv() []string {
	return []string{ToolName, "version"}
}

func SessionsListArgv() []string {
	return []string{ToolName, "sessions", "list", "--json"}
}

func SessionDetailsArgv(session string) ([]string, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	return []string{ToolName, "sessions", "show", session, "--json"}, nil
}

func SnapshotArgv() []string {
	return []string{ToolName, "--robot-snapshot"}
}

func StatusArgv() []string {
	return []string{ToolName, "--robot-status"}
}

func TriageArgv() []string {
	return []string{ToolName, "--robot-triage"}
}

func ActivityArgv(session string) ([]string, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	return []string{ToolName, "--robot-activity=" + session}, nil
}

func TailArgv(req TailRequest) ([]string, error) {
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "--robot-tail=" + session}
	if req.Lines > 0 {
		argv = append(argv, "--lines="+strconv.Itoa(req.Lines))
	}
	if len(req.Panes) > 0 {
		argv = append(argv, "--panes="+strings.Join(trimNonEmpty(req.Panes), ","))
	}
	if req.All {
		argv = append(argv, "--all")
	}
	return argv, nil
}

func SpawnArgv(req SpawnRequest) ([]string, error) {
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "--robot-spawn=" + session}
	if req.Claude > 0 {
		argv = append(argv, "--spawn-cc="+strconv.Itoa(req.Claude))
	}
	if req.Codex > 0 {
		argv = append(argv, "--spawn-cod="+strconv.Itoa(req.Codex))
	}
	if req.Gemini > 0 {
		argv = append(argv, "--spawn-gmi="+strconv.Itoa(req.Gemini))
	}
	if req.Wait {
		argv = append(argv, "--spawn-wait")
	}
	if req.DryRun {
		argv = append(argv, "--dry-run")
	}
	return argv, nil
}

func SendArgv(req SendRequest) ([]string, error) {
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, fmt.Errorf("%w: message is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "--robot-send=" + session, "--msg=" + msg}
	if len(req.Panes) > 0 {
		argv = append(argv, "--panes="+strings.Join(trimNonEmpty(req.Panes), ","))
	}
	if req.Type != "" {
		argv = append(argv, "--type="+strings.TrimSpace(req.Type))
	}
	if req.All {
		argv = append(argv, "--all")
	}
	if req.Track {
		argv = append(argv, "--track")
	}
	if req.DryRun {
		argv = append(argv, "--dry-run")
	}
	return argv, nil
}

func WaitArgv(req WaitRequest) ([]string, error) {
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "--robot-wait=" + session}
	if req.Timeout != "" {
		argv = append(argv, "--timeout="+strings.TrimSpace(req.Timeout))
	}
	if req.Condition != "" {
		argv = append(argv, "--condition="+strings.TrimSpace(req.Condition))
	}
	return argv, nil
}

func InterruptArgv(req InterruptRequest) ([]string, error) {
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return nil, fmt.Errorf("%w: session is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "--robot-interrupt=" + session}
	if req.Message != "" {
		argv = append(argv, "--interrupt-msg="+strings.TrimSpace(req.Message))
	}
	if len(req.Panes) > 0 {
		argv = append(argv, "--panes="+strings.Join(trimNonEmpty(req.Panes), ","))
	}
	if req.All {
		argv = append(argv, "--all")
	}
	if req.DryRun {
		argv = append(argv, "--dry-run")
	}
	return argv, nil
}

func ApprovalsListArgv() []string {
	return []string{ToolName, "approve", "list", "--json"}
}

func ApprovalShowArgv(token string) ([]string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: approval token is required", ErrInvalidRequest)
	}
	return []string{ToolName, "approve", "show", token, "--json"}, nil
}

func ApproveArgv(req ApprovalRequest) ([]string, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return nil, fmt.Errorf("%w: approval token is required", ErrInvalidRequest)
	}
	return []string{ToolName, "approve", token, "--json"}, nil
}

func DenyArgv(req ApprovalRequest) ([]string, error) {
	token := strings.TrimSpace(req.Token)
	reason := strings.TrimSpace(req.Reason)
	if token == "" {
		return nil, fmt.Errorf("%w: approval token is required", ErrInvalidRequest)
	}
	if reason == "" {
		return nil, fmt.Errorf("%w: denial reason is required", ErrInvalidRequest)
	}
	return []string{ToolName, "approve", "deny", token, "--reason", reason, "--json"}, nil
}

func TmuxCaptureArgv(req TmuxCaptureRequest) ([]string, error) {
	target := strings.TrimSpace(req.TargetPane)
	if target == "" {
		return nil, fmt.Errorf("%w: tmux target pane is required", ErrInvalidRequest)
	}
	argv := []string{"tmux", "capture-pane", "-p", "-t", target}
	if req.JoinWrapped {
		argv = append(argv, "-J")
	}
	if req.StartLine != 0 {
		argv = append(argv, "-S", strconv.Itoa(req.StartLine))
	}
	if req.EndLine != 0 {
		argv = append(argv, "-E", strconv.Itoa(req.EndLine))
	}
	return argv, nil
}
