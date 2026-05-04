package connections

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultV1ConnectionIsSafeAndValid(t *testing.T) {
	t.Parallel()

	connection := DefaultV1Connection()
	if err := connection.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if connection.ConnectionID != DefaultConnectionID {
		t.Fatalf("connection id = %q, want %q", connection.ConnectionID, DefaultConnectionID)
	}
	if connection.Port != DefaultSSHPort || connection.Status != StatusDisconnected {
		t.Fatalf("connection = %+v", connection)
	}
	if connection.BearerKeyRef != "" || connection.SSHKeyRef != "" {
		t.Fatalf("default connection should not contain secret refs: %+v", connection)
	}
}

func TestBackfillConnectionIDKeepsLegacyVPSID(t *testing.T) {
	t.Parallel()

	if got := BackfillConnectionID("  vps_local  "); got != DefaultConnectionID {
		t.Fatalf("got %q, want %q", got, DefaultConnectionID)
	}
	if got := BackfillConnectionID(""); got != DefaultConnectionID {
		t.Fatalf("blank got %q, want %q", got, DefaultConnectionID)
	}
	if got := BackfillConnectionID("team-vps"); got != "team-vps" {
		t.Fatalf("custom got %q, want team-vps", got)
	}
}

func TestPlanSingleVPSMigrationBackfillsProjects(t *testing.T) {
	t.Parallel()

	plan, err := PlanSingleVPSMigration([]ProjectRecord{
		{ProjectID: "proj_1", VPSID: ""},
		{ProjectID: "proj_2", VPSID: "vps_local"},
		{ProjectID: "proj_3", VPSID: " vps_local ", ConnectionID: "vps_local"},
	}, nil)
	if err != nil {
		t.Fatalf("PlanSingleVPSMigration: %v", err)
	}
	if !plan.CreateConnection {
		t.Fatal("expected migration to create the implicit v1 connection")
	}
	if plan.TargetConnection.ConnectionID != DefaultConnectionID {
		t.Fatalf("target = %+v", plan.TargetConnection)
	}
	if len(plan.ProjectBackfills) != 3 {
		t.Fatalf("backfills = %d, want 3", len(plan.ProjectBackfills))
	}
	for _, backfill := range plan.ProjectBackfills {
		if backfill.ConnectionID != DefaultConnectionID || backfill.LegacyVPSID != DefaultConnectionID {
			t.Fatalf("backfill = %+v", backfill)
		}
	}
	if !plan.ProjectBackfills[2].AlreadyBackfilled {
		t.Fatalf("expected existing connection id to be marked already backfilled: %+v", plan.ProjectBackfills[2])
	}
}

func TestPlanSingleVPSMigrationIsIdempotentWithExistingConnection(t *testing.T) {
	t.Parallel()

	existing := []Connection{{
		ConnectionID:  DefaultConnectionID,
		Host:          "build.example.net",
		Port:          DefaultSSHPort,
		Username:      "ubuntu",
		Status:        StatusReady,
		BearerKeyRef:  "secret://connections/vps_local/bearer",
		SSHKeyRef:     "ssh://keychain/acfs",
		SchemaVersion: SchemaVersion,
	}}
	plan, err := PlanSingleVPSMigration([]ProjectRecord{
		{ProjectID: "proj_1", VPSID: DefaultConnectionID, ConnectionID: DefaultConnectionID},
	}, existing)
	if err != nil {
		t.Fatalf("PlanSingleVPSMigration: %v", err)
	}
	if plan.CreateConnection {
		t.Fatal("existing connection should make creation idempotent")
	}
	if plan.TargetConnection.Host != "build.example.net" || plan.TargetConnection.Status != StatusReady {
		t.Fatalf("target = %+v", plan.TargetConnection)
	}
	if len(plan.ProjectBackfills) != 1 || !plan.ProjectBackfills[0].AlreadyBackfilled {
		t.Fatalf("backfills = %+v", plan.ProjectBackfills)
	}
}

func TestPlanSingleVPSMigrationRejectsMultiVPSDrift(t *testing.T) {
	t.Parallel()

	_, err := PlanSingleVPSMigration([]ProjectRecord{
		{ProjectID: "proj_1", VPSID: DefaultConnectionID},
		{ProjectID: "proj_2", VPSID: "other-vps"},
	}, nil)
	if !errors.Is(err, ErrProjectConnectionDrift) {
		t.Fatalf("err = %v, want ErrProjectConnectionDrift", err)
	}

	_, err = PlanSingleVPSMigration(nil, []Connection{
		{ConnectionID: DefaultConnectionID, Port: 22, Status: StatusReady, SchemaVersion: SchemaVersion},
		{ConnectionID: "other-vps", Port: 22, Status: StatusReady, SchemaVersion: SchemaVersion},
	})
	if !errors.Is(err, ErrMultipleConnections) {
		t.Fatalf("err = %v, want ErrMultipleConnections", err)
	}
}

func TestConnectionValidationRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]Connection{
		"blank id":    {Port: 22, Status: StatusReady, SchemaVersion: SchemaVersion},
		"bad id":      {ConnectionID: "bad id", Port: 22, Status: StatusReady, SchemaVersion: SchemaVersion},
		"bad port":    {ConnectionID: DefaultConnectionID, Port: 70000, Status: StatusReady, SchemaVersion: SchemaVersion},
		"bad status":  {ConnectionID: DefaultConnectionID, Port: 22, Status: "running", SchemaVersion: SchemaVersion},
		"url host":    {ConnectionID: DefaultConnectionID, Host: "ssh://example.test", Port: 22, Status: StatusReady, SchemaVersion: SchemaVersion},
		"bad version": {ConnectionID: DefaultConnectionID, Port: 22, Status: StatusReady, SchemaVersion: 99},
	}
	for name, connection := range tests {
		name := name
		connection := connection
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := connection.Validate(); !errors.Is(err, ErrInvalidConnection) {
				t.Fatalf("err = %v, want ErrInvalidConnection", err)
			}
		})
	}
}

func TestSchemaStatementsPreserveAdditiveMigrationShape(t *testing.T) {
	t.Parallel()

	statements := strings.Join(SchemaSQLStatements(), "\n")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS connections",
		"connection_id TEXT PRIMARY KEY",
		"bearer_key_ref TEXT",
		"ssh_key_ref TEXT",
		"ALTER TABLE projects ADD COLUMN connection_id TEXT",
		"UPDATE projects",
		"CREATE INDEX IF NOT EXISTS projects_connection_id_idx",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("schema statements missing %q:\n%s", want, statements)
		}
	}
	if strings.Contains(statements, "bearer_key TEXT") {
		t.Fatalf("schema should store bearer key refs only:\n%s", statements)
	}
}
