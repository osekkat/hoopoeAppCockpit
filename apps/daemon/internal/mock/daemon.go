// Package mock boots the daemon against deterministic Flywheel fixtures.
package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/fixtures"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/inventory"
	daemonmetrics "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/metrics"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

type Config struct {
	Scenario    string
	FixtureRoot string
	Build       api.BuildInfo
	Now         func() time.Time
}

type Daemon struct {
	Scenario     *fixtures.Phase0Scenario
	Events       *api.EventHub
	Jobs         *JobReader
	Capabilities *capabilities.Registry
	Inventory    *inventory.Service
	Metrics      *daemonmetrics.Registry
	Audit        *audit.Writer
	Build        api.BuildInfo
	Now          func() time.Time
}

func NewDaemon(cfg Config) (*Daemon, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	build := cfg.Build
	if build.Version == "" {
		build.Version = "0.0.0"
	}
	if build.Commit == "" {
		build.Commit = "mock"
	}
	if build.BuildDate == "" {
		build.BuildDate = "mock"
	}
	if build.APIVersion == "" {
		build.APIVersion = "v1"
	}

	scenario, err := fixtures.LoadPhase0Scenario(cfg.FixtureRoot, cfg.Scenario)
	if err != nil {
		return nil, err
	}

	registry := capabilities.New(build.APIVersion)
	registry.SetClock(now)
	registry.SetFixturesVersion(scenario.Manifest.FixturesVersion)
	reports, err := scenario.CapabilityReports()
	if err != nil {
		return nil, err
	}
	for _, report := range reports {
		if err := registry.SetReport(report); err != nil {
			return nil, err
		}
	}

	events := api.NewEventHub(api.EventHubConfig{
		Now:      now,
		Redactor: redaction.New(redaction.Config{Now: now}),
	})
	auditWriter, err := audit.NewWriter(audit.Config{
		Writer: io.Discard,
		Now:    now,
	})
	if err != nil {
		return nil, err
	}
	metricsRegistry := daemonmetrics.NewRegistry(daemonmetrics.Config{
		Now:                   now,
		IncludeDefaultTargets: true,
	})
	_ = metricsRegistry.SetGauge(daemonmetrics.MetricInFlightJobs, nil, 0)
	_ = metricsRegistry.SetGauge(daemonmetrics.MetricJobCancellationOrphans, nil, 0)
	events.Publish(api.PublishInput{
		Channel: "_system",
		Type:    "mock.scenario.loaded",
		Actor:   map[string]any{"kind": "system", "id": "mock-flywheel"},
		Data: map[string]any{
			"scenario":        scenario.Manifest.Scenario,
			"fixturesVersion": scenario.Manifest.FixturesVersion,
			"adapterCount":    len(scenario.Manifest.Adapters),
		},
	})
	_, _, _ = auditWriter.Append(audit.Entry{
		ProjectID:     "mock-flywheel-project",
		Action:        "mock.scenario.loaded",
		Actor:         audit.Actor{Kind: audit.ActorSystem, ID: "mock-flywheel"},
		Result:        audit.ResultSuccess,
		Reason:        "fixture scenario loaded",
		CorrelationID: "mock-" + scenario.Manifest.Scenario,
		Data: map[string]any{
			"scenario":        scenario.Manifest.Scenario,
			"fixturesVersion": scenario.Manifest.FixturesVersion,
		},
	})
	for _, adapter := range scenario.Manifest.Adapters {
		capture, err := scenario.Adapter(adapter)
		if err != nil {
			return nil, err
		}
		events.Publish(api.PublishInput{
			Channel: "_system",
			Type:    "mock.adapter.loaded",
			Actor:   map[string]any{"kind": "system", "id": "mock-flywheel"},
			Data: map[string]any{
				"tool":    capture.Tool,
				"present": capture.Present,
				"source":  "fixture",
			},
		})
		_, _, _ = auditWriter.Append(audit.Entry{
			ProjectID:     "mock-flywheel-project",
			Action:        "mock.adapter.loaded",
			Actor:         audit.Actor{Kind: audit.ActorAdapter, ID: adapter},
			Result:        audit.ResultSuccess,
			Reason:        "adapter fixture loaded",
			CorrelationID: "mock-" + scenario.Manifest.Scenario,
			Data: map[string]any{
				"tool":    capture.Tool,
				"present": capture.Present,
			},
		})
	}

	return &Daemon{
		Scenario:     scenario,
		Events:       events,
		Jobs:         NewJobReader(scenario, now),
		Capabilities: registry,
		Inventory: inventory.NewService(inventory.Config{
			Registry: registry,
			Now:      now,
		}),
		Metrics: metricsRegistry,
		Audit:   auditWriter,
		Build:   build,
		Now:     now,
	}, nil
}

func (d *Daemon) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/v1/capabilities", d.Capabilities.HandleCapabilities)
	r.Get("/v1/compatibility", d.Capabilities.HandleCompatibility(capabilities.StaticCompatibilityComposer{
		MinDesktopVersion: "0.0.0",
		EventSchemaVersions: map[string]int{
			"_system": 1,
			"project": 1,
			"swarm":   1,
		},
		Migration: capabilities.MigrationState{
			SchemaVersion: 1,
			AppliedAt:     d.Now().UTC().Format(time.RFC3339),
			Pending:       []string{},
			Phase:         capabilities.MigrationIdle,
		},
	}))
	r.Get("/v1/mock/scenario", d.handleScenario)
	r.Get("/v1/mock/adapters/{tool}", d.handleAdapter)
	r.Mount("/", api.NewRouter(api.Config{
		Build:        d.Build,
		Events:       d.Events,
		Jobs:         d.Jobs,
		Audit:        d.Audit,
		Capabilities: d.Capabilities,
		Inventory:    d.Inventory,
		Metrics:      d.Metrics,
		Now:          d.Now,
	}))
	return r
}

func (d *Daemon) handleScenario(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion": 1,
		"mock":          true,
		"manifest":      d.Scenario.Manifest,
	})
}

func (d *Daemon) handleAdapter(w http.ResponseWriter, r *http.Request) {
	tool := chi.URLParam(r, "tool")
	capture, err := d.Scenario.Adapter(tool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, capture)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
