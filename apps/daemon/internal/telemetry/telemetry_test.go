package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDisabledServiceRecordsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	service, err := NewService(Config{Path: path})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.RecordEvent(context.Background(), EventInput{Type: EventStageUsage})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("RecordEvent err = %v, want ErrDisabled", err)
	}
	_, err = service.SaveCrashReport(context.Background(), CrashReportInput{StackTrace: "panic"})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("SaveCrashReport err = %v, want ErrDisabled", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled service should not write records, stat err=%v", err)
	}
	status, err := service.PrivacyStatus(context.Background())
	if err != nil {
		t.Fatalf("PrivacyStatus: %v", err)
	}
	if status.Enabled || !status.LocalOnly || status.UploadConfigured {
		t.Fatalf("status = %+v", status)
	}
}

func TestCrashReportIsRedactedAndTombstoned(t *testing.T) {
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	service, err := NewService(Config{
		Enabled: true,
		Path:    filepath.Join(t.TempDir(), "records.jsonl"),
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	report, err := service.SaveCrashReport(context.Background(), CrashReportInput{
		ID:             "crash_test",
		DaemonVersion:  "1.2.3",
		DesktopVersion: "4.5.6",
		APIVersion:     "v1",
		StackTrace:     "panic: bad\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz",
		AuditTail:      []string{"user email alice@example.com", "path /home/ubuntu/secret/file"},
		Context:        map[string]string{"stage": "swarm", "account": "bob@example.com"},
	})
	if err != nil {
		t.Fatalf("SaveCrashReport: %v", err)
	}
	if strings.Contains(report.StackTrace, "abcdefghijklmnopqrstuvwxyz") || strings.Contains(strings.Join(report.AuditTail, "\n"), "/home/ubuntu") {
		t.Fatalf("report was not redacted: %+v", report)
	}
	if len(report.Redactions) == 0 {
		t.Fatalf("expected redaction traces: %+v", report)
	}
	list, err := service.ListCrashReports(context.Background())
	if err != nil {
		t.Fatalf("ListCrashReports: %v", err)
	}
	if len(list) != 1 || list[0].ID != "crash_test" {
		t.Fatalf("list = %+v", list)
	}
	if err := service.DeleteCrashReport(context.Background(), "crash_test"); err != nil {
		t.Fatalf("DeleteCrashReport: %v", err)
	}
	list, err = service.ListCrashReports(context.Background())
	if err != nil {
		t.Fatalf("ListCrashReports after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("deleted report still listed: %+v", list)
	}
}

func TestTelemetryEventsAreAggregateOnly(t *testing.T) {
	service, err := NewService(Config{
		Enabled: true,
		Path:    filepath.Join(t.TempDir(), "records.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.RecordEvent(context.Background(), EventInput{
		Type:  EventStageUsage,
		Count: 3,
		Dimensions: map[string]string{
			"stage": "planning",
		},
	})
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	_, err = service.RecordEvent(context.Background(), EventInput{
		Type: EventStageUsage,
		Dimensions: map[string]string{
			"filePath": "/home/ubuntu/project/secret.go",
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsafe RecordEvent err = %v, want ErrInvalidRequest", err)
	}
	status, err := service.PrivacyStatus(context.Background())
	if err != nil {
		t.Fatalf("PrivacyStatus: %v", err)
	}
	if status.PendingTelemetryEvents != 1 || status.PendingCrashReports != 0 {
		t.Fatalf("status = %+v", status)
	}
}
