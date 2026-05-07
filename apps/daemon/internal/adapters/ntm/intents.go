package ntm

import (
	"fmt"
	"strings"
)

// intents.go owns the NTM ActionIntent type and its constructors.
//
// hp-h5yq second cut: ActionIntent + the 5 intent constructors
// (SendMarchingOrdersIntent, SwarmHaltIntent, SessionTerminateIntent,
// SessionAttachIntent, ApprovalDecisionIntent) split out of ntm.go to
// continue the size-outlier reduction. Behavior unchanged — same
// package, same exported signatures, same constants.
//
// ActionIntent is the typed envelope §8.3.1 hands to the daemon's
// action executor: a CapabilityID plus Target/Args plus Pre/Post
// conditions. The daemon (not the model) is the executor; intents are
// declarative descriptions of "what would mutate" before policy +
// idempotency + approval gates.

// ActionIntent is the typed mutation request §8.3.1 hands to the
// action executor. Kind names the high-level operation; CapabilityID
// names the capability gate the executor must verify before
// proceeding; Target identifies the operand; Args carries the payload;
// Pre/Postconditions document the gating + verification contract for
// audit + restartability.
type ActionIntent struct {
	Kind           string         `json:"kind"`
	CapabilityID   string         `json:"capabilityId"`
	Target         map[string]any `json:"target"`
	Args           map[string]any `json:"args"`
	Preconditions  []string       `json:"preconditions"`
	Postconditions []string       `json:"postconditions"`
}

func SendMarchingOrdersIntent(session, agentID, message string) (ActionIntent, error) {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(agentID) == "" {
		return ActionIntent{}, fmt.Errorf("%w: session and agent id are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(message) == "" {
		return ActionIntent{}, fmt.Errorf("%w: message is required", ErrInvalidRequest)
	}
	return ActionIntent{
		Kind:         ActionSendMarchingOrders,
		CapabilityID: CapabilitySendMarchingOrders,
		Target: map[string]any{
			"session": session,
			"agentId": agentID,
		},
		Args: map[string]any{
			"message": strings.TrimSpace(message),
		},
		Preconditions: []string{
			"ntm.robot.status reports the target agent pane exists",
			"action executor policy permits agent messaging",
		},
		Postconditions: []string{
			"ntm.robot.tail or Agent Mail activity shows the message was delivered",
		},
	}, nil
}

func SwarmHaltIntent(session, reason string) (ActionIntent, error) {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(reason) == "" {
		return ActionIntent{}, fmt.Errorf("%w: session and reason are required", ErrInvalidRequest)
	}
	return ActionIntent{
		Kind:         ActionSwarmHalt,
		CapabilityID: CapabilitySwarmHalt,
		Target: map[string]any{
			"session": strings.TrimSpace(session),
		},
		Args: map[string]any{
			"reason": strings.TrimSpace(reason),
		},
		Preconditions: []string{
			"watch-safety-thresholds or user approval requested swarm halt",
			"ntm.robot.status reports the session exists",
		},
		Postconditions: []string{
			"ntm.robot.status reports no agent panes running for the session",
		},
	}, nil
}

func SessionTerminateIntent(session, reason string) (ActionIntent, error) {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(reason) == "" {
		return ActionIntent{}, fmt.Errorf("%w: session and reason are required", ErrInvalidRequest)
	}
	return ActionIntent{
		Kind:         ActionSessionTerminate,
		CapabilityID: CapabilitySessionsTerminate,
		Target: map[string]any{
			"session": strings.TrimSpace(session),
		},
		Args: map[string]any{
			"reason": strings.TrimSpace(reason),
		},
		Preconditions: []string{
			"ntm.robot.status reports the session exists",
			"action executor policy permits session termination",
		},
		Postconditions: []string{
			"ntm.sessions.list no longer reports the terminated session as active",
		},
	}, nil
}

func SessionAttachIntent(session, reason string) (ActionIntent, error) {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(reason) == "" {
		return ActionIntent{}, fmt.Errorf("%w: session and reason are required", ErrInvalidRequest)
	}
	return ActionIntent{
		Kind:         ActionSessionAttach,
		CapabilityID: CapabilitySessionsAttach,
		Target: map[string]any{
			"session": strings.TrimSpace(session),
		},
		Args: map[string]any{
			"reason": strings.TrimSpace(reason),
		},
		Preconditions: []string{
			"Diagnostics requested an explicit audited raw pane view",
			"action executor policy permits tmux attach for forensics",
		},
		Postconditions: []string{
			"audit log records the attach request and target session",
		},
	}, nil
}

func ApprovalDecisionIntent(req ApprovalRequest, approve bool) (ActionIntent, error) {
	if strings.TrimSpace(req.Token) == "" {
		return ActionIntent{}, fmt.Errorf("%w: approval token is required", ErrInvalidRequest)
	}
	if !approve && strings.TrimSpace(req.Reason) == "" {
		return ActionIntent{}, fmt.Errorf("%w: denial reason is required", ErrInvalidRequest)
	}
	kind := ActionApprovalApprove
	capID := CapabilityApprovalsApprove
	if !approve {
		kind = ActionApprovalDeny
		capID = CapabilityApprovalsDeny
	}
	return ActionIntent{
		Kind:         kind,
		CapabilityID: capID,
		Target: map[string]any{
			"token": strings.TrimSpace(req.Token),
		},
		Args: map[string]any{
			"reason": strings.TrimSpace(req.Reason),
		},
		Preconditions: []string{
			"ntm.approvals.list reports the approval token is pending",
			"operator policy permits the requested approval decision",
		},
		Postconditions: []string{
			"ntm.approvals.list no longer reports the token as pending",
		},
	}, nil
}
