package agentmail

import (
	"context"
	"errors"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func CapabilityIDs() []string {
	return []string{
		CapabilityMessagesRead,
		CapabilityMessagesSend,
		CapabilityMessagesUrgent,
		CapabilityThreadsRead,
		CapabilityReservationsList,
		CapabilityReservationsRelease,
		CapabilityReservationsForceRelease,
		CapabilityInboxSubscribe,
		CapabilityThreadConventionEnforced,
		CapabilityForceReleaseNotification,
	}
}

func StaticReport(now func() time.Time) *capabilities.ToolReport {
	if now == nil {
		now = time.Now
	}
	caps := make(map[string]capabilities.Capability, len(CapabilityIDs()))
	for _, id := range CapabilityIDs() {
		caps[id] = capabilities.Capability{
			Status:    capabilities.StatusOK,
			Transport: "mcp_http",
		}
	}
	caps[CapabilityThreadConventionEnforced] = capabilities.Capability{
		Status:    capabilities.StatusOK,
		Transport: "daemon",
		Notes:     "bead thread ids are normalized to br-<beadId>",
	}
	caps[CapabilityForceReleaseNotification] = capabilities.Capability{
		Status:    capabilities.StatusOK,
		Transport: "mcp_http",
		Notes:     "force-release requests always pass notify_previous=true and require an operator note",
	}
	return &capabilities.ToolReport{
		Tool:          capabilities.ToolAgentMail,
		Source:        "mcp_http",
		LastCheckedAt: now().UTC().Format(time.RFC3339Nano),
		Capabilities:  caps,
	}
}

func Probe(ctx context.Context, client *Client, projectKey string, agentName string, now func() time.Time) *capabilities.ToolReport {
	report := StaticReport(now)
	if client == nil || projectKey == "" || agentName == "" {
		markAll(report, capabilities.StatusUntested, "probe skipped: client, project, or agent not configured")
		return report
	}
	_, err := client.FetchInbox(ctx, FetchInboxRequest{
		ProjectKey: projectKey,
		AgentName:  agentName,
		Limit:      1,
	})
	if err == nil {
		return report
	}
	status := capabilities.StatusDegraded
	if errors.Is(err, ErrHTTPStatus) || errors.Is(err, ErrMCPError) {
		status = capabilities.StatusDegraded
	}
	markAll(report, status, err.Error())
	return report
}

func markAll(report *capabilities.ToolReport, status capabilities.CapabilityStatus, note string) {
	for id, cap := range report.Capabilities {
		cap.Status = status
		cap.Notes = note
		report.Capabilities[id] = cap
	}
}
