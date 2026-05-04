// Package agentmail wraps MCP Agent Mail behind typed daemon operations.
//
// The adapter talks to Agent Mail's MCP-over-HTTP tool surface and keeps
// thread, message, and reservation operations explicit. It does not inspect
// terminal output and it does not expose arbitrary tool execution to callers.
package agentmail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ToolName = "agent_mail"

const (
	CapabilityMessagesRead             = "agent_mail.messages.read"
	CapabilityMessagesSend             = "agent_mail.messages.send"
	CapabilityMessagesUrgent           = "agent_mail.messages.urgent"
	CapabilityThreadsRead              = "agent_mail.threads.read"
	CapabilityReservationsList         = "agent_mail.reservations.list"
	CapabilityReservationsRelease      = "agent_mail.reservations.release"
	CapabilityReservationsForceRelease = "agent_mail.reservations.force_release"
	CapabilityInboxSubscribe           = "agent_mail.inbox.subscribe"
	CapabilityThreadConventionEnforced = "agent_mail.threads.bead_id_convention"
	CapabilityForceReleaseNotification = "agent_mail.reservations.force_release_notice"
)

var (
	ErrInvalidRequest = errors.New("agentmail: invalid request")
	ErrHTTPStatus     = errors.New("agentmail: http status")
	ErrMCPError       = errors.New("agentmail: mcp error")
	ErrDecode         = errors.New("agentmail: decode response")
)

type AuditSink interface {
	RecordAgentMailAction(context.Context, AuditEvent) error
}

type AuditFunc func(context.Context, AuditEvent) error

func (f AuditFunc) RecordAgentMailAction(ctx context.Context, event AuditEvent) error {
	return f(ctx, event)
}

type AuditEvent struct {
	Action        string
	ProjectKey    string
	AgentName     string
	TargetAgent   string
	ThreadID      string
	ReservationID int
	Reason        string
	Result        string
	Error         string
	At            time.Time
}

type Message struct {
	ID          int      `json:"id"`
	ProjectID   int      `json:"project_id,omitempty"`
	ThreadID    string   `json:"thread_id,omitempty"`
	Subject     string   `json:"subject"`
	BodyMD      string   `json:"body_md,omitempty"`
	Importance  string   `json:"importance"`
	AckRequired bool     `json:"ack_required"`
	CreatedTS   string   `json:"created_ts"`
	From        string   `json:"from"`
	To          []string `json:"to,omitempty"`
	CC          []string `json:"cc,omitempty"`
	BCC         []string `json:"bcc,omitempty"`
	Kind        string   `json:"kind,omitempty"`
}

type Delivery struct {
	Project string  `json:"project"`
	Payload Message `json:"payload"`
}

type SendMessageRequest struct {
	ProjectKey      string
	SenderName      string
	To              []string
	CC              []string
	BCC             []string
	Subject         string
	BodyMD          string
	Importance      string
	AckRequired     bool
	ThreadID        string
	Topic           string
	Broadcast       bool
	AttachmentPaths []string
	ConvertImages   *bool
}

type SendMessageResponse struct {
	Deliveries []Delivery `json:"deliveries"`
	Count      int        `json:"count"`
}

type FetchInboxRequest struct {
	ProjectKey    string
	AgentName     string
	Limit         int
	UrgentOnly    bool
	IncludeBodies bool
	SinceTS       string
	Topic         string
}

type SearchMessagesRequest struct {
	ProjectKey string
	Query      string
	Limit      int
}

type ThreadSummaryRequest struct {
	ProjectKey      string
	ThreadID        string
	IncludeExamples bool
	LLMMode         bool
	LLMModel        string
	PerThreadLimit  int
}

type ListThreadsRequest struct {
	ProjectKey string
	Query      string
	Limit      int
}

type Thread struct {
	ID            string   `json:"id"`
	Subject       string   `json:"subject,omitempty"`
	LastMessageID int      `json:"lastMessageId,omitempty"`
	LastMessageAt string   `json:"lastMessageAt,omitempty"`
	Participants  []string `json:"participants,omitempty"`
}

type ThreadSummary struct {
	ThreadID string         `json:"thread_id"`
	Summary  map[string]any `json:"summary"`
	Examples []Message      `json:"examples,omitempty"`
}

type Reservation struct {
	ID          int    `json:"id"`
	PathPattern string `json:"path_pattern"`
	Exclusive   bool   `json:"exclusive"`
	Reason      string `json:"reason"`
	CreatedTS   string `json:"created_ts,omitempty"`
	ExpiresTS   string `json:"expires_ts"`
	ReleasedTS  string `json:"released_ts,omitempty"`
	Agent       string `json:"agent,omitempty"`
}

type ReservationConflict struct {
	Path    string   `json:"path"`
	Holders []string `json:"holders"`
}

type ReservePathsRequest struct {
	ProjectKey string
	AgentName  string
	Paths      []string
	TTLSeconds int
	Exclusive  bool
	Reason     string
}

type ReservePathsResponse struct {
	Granted   []Reservation         `json:"granted"`
	Conflicts []ReservationConflict `json:"conflicts"`
}

type ListReservationsRequest struct {
	Project    string
	ActiveOnly bool
	Limit      int
}

type ReleaseReservationsRequest struct {
	ProjectKey         string
	AgentName          string
	Paths              []string
	FileReservationIDs []int
}

type ReleaseReservationsResponse struct {
	Released   int    `json:"released"`
	ReleasedAt string `json:"released_at"`
}

type ForceReleaseReservationRequest struct {
	ProjectKey        string
	AgentName         string
	FileReservationID int
	Note              string
}

type ForceReleaseReservationResponse struct {
	Released        int            `json:"released"`
	ReleasedAt      string         `json:"released_at"`
	AlreadyReleased bool           `json:"already_released,omitempty"`
	Reservation     map[string]any `json:"reservation,omitempty"`
}

func BeadThreadID(beadID string) (string, error) {
	beadID = strings.TrimSpace(beadID)
	if beadID == "" {
		return "", fmt.Errorf("%w: empty bead id", ErrInvalidRequest)
	}
	if strings.ContainsAny(beadID, " \t\r\n/\\") {
		return "", fmt.Errorf("%w: unsafe bead id %q", ErrInvalidRequest, beadID)
	}
	if strings.HasPrefix(beadID, "br-") {
		return beadID, nil
	}
	return "br-" + beadID, nil
}

func ValidateBeadThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("%w: empty thread id", ErrInvalidRequest)
	}
	if !strings.HasPrefix(threadID, "br-") {
		return fmt.Errorf("%w: bead thread id %q must start with br-", ErrInvalidRequest, threadID)
	}
	if strings.ContainsAny(threadID, " \t\r\n/\\") {
		return fmt.Errorf("%w: unsafe thread id %q", ErrInvalidRequest, threadID)
	}
	return nil
}
