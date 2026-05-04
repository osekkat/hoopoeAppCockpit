// Package git owns daemon Git event payload contracts for the VPS project
// working tree and origin sync loop.
package git

import (
	"strings"
	"time"
)

const (
	EventVPSCommitCreated = "vps_commit_created"
	EventVPSPushCompleted = "vps_push_completed"
	EventVPSPushComplete  = EventVPSPushCompleted
	EventOriginUpdated    = "origin_updated"

	OriginUpdateSourceVPSPush      = "vps_push"
	OriginUpdateSourceExternalPush = "external_push"

	defaultCommitMessageLimit = 240
)

type CommitCreatedPayload struct {
	ProjectID    string    `json:"projectId"`
	CommitSHA    string    `json:"commitSha"`
	Branch       string    `json:"branch"`
	ParentSHA    string    `json:"parentSha,omitempty"`
	AuthorName   string    `json:"authorName,omitempty"`
	AuthorEmail  string    `json:"authorEmail,omitempty"`
	Message      string    `json:"message"`
	FilesChanged int       `json:"filesChanged"`
	Time         time.Time `json:"time"`
}

type PushCompletedPayload struct {
	ProjectID     string    `json:"projectId"`
	Branch        string    `json:"branch"`
	CommitsPushed []string  `json:"commitsPushed"`
	Remote        string    `json:"remote"`
	Time          time.Time `json:"time"`
	DurationMs    int64     `json:"durationMs"`
	OK            bool      `json:"ok"`
	Reason        string    `json:"reason,omitempty"`
}

type RefUpdate struct {
	Name   string `json:"name"`
	OldSHA string `json:"oldSha,omitempty"`
	NewSHA string `json:"newSha"`
}

type OriginUpdatedPayload struct {
	ProjectID string      `json:"projectId"`
	Refs      []RefUpdate `json:"refs"`
	Source    string      `json:"source"`
	Time      time.Time   `json:"time"`
}

type RefState struct {
	Name string
	SHA  string
}

func ProjectChannel(projectID string) string {
	token := safeProjectToken(projectID)
	if token == "" {
		return "project:unknown"
	}
	return "project:" + token
}

func TruncateCommitMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= defaultCommitMessageLimit {
		return message
	}
	return strings.TrimSpace(message[:defaultCommitMessageLimit]) + "..."
}

func safeProjectToken(projectID string) string {
	projectID = strings.TrimSpace(projectID)
	var b strings.Builder
	for _, r := range projectID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
