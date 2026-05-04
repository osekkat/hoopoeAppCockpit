package checkpoints

import (
	"context"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

type AuditAppender interface {
	Append(audit.Entry) (audit.Entry, []redaction.TraceEvent, error)
}

type AuditWriterSink struct {
	Writer AuditAppender
}

func (s AuditWriterSink) RecordCheckpointTransition(_ context.Context, event AuditEvent) error {
	if s.Writer == nil {
		return nil
	}
	result := audit.ResultSuccess
	if event.ToStatus == StatusFailed {
		result = audit.ResultFailure
	}
	entry := audit.Entry{
		ProjectID: stringOr(event.ProjectID, audit.GlobalProjectID),
		Actor:     auditActor(event.Actor),
		Action:    event.Action,
		Reason:    event.Reason,
		Result:    result,
		Data: map[string]any{
			"runId":        event.RunID,
			"stepId":       event.StepID,
			"fromStatus":   event.FromStatus,
			"toStatus":     event.ToStatus,
			"evidenceRefs": event.EvidenceRefs,
		},
	}
	for key, value := range event.Data {
		entry.Data[key] = value
	}
	_, _, err := s.Writer.Append(entry)
	return err
}

func auditActor(actor schemas.Actor) audit.Actor {
	id := ""
	if actor.Id != nil {
		id = *actor.Id
	}
	switch actor.Kind {
	case schemas.ActorKindAgent:
		return audit.Actor{Kind: audit.ActorAgent, ID: id}
	case schemas.ActorKindScheduler:
		return audit.Actor{Kind: audit.ActorTendingJob, ID: id}
	case schemas.ActorKindUser:
		return audit.Actor{Kind: audit.ActorUser, ID: id}
	default:
		return audit.Actor{Kind: audit.ActorSystem, ID: stringOr(id, "onboarding")}
	}
}

func stringOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
