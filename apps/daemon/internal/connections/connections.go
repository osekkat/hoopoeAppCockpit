// Package connections models the post-MVP Connection entity that will replace
// the v1 implicit single-VPS install boundary.
package connections

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion       = 1
	DefaultConnectionID = "vps_local"
	DefaultSSHPort      = 22

	LegacyProjectVPSColumn     = "vps_id"
	FutureProjectConnectionCol = "connection_id"
)

const (
	CreateConnectionsTableSQL = `CREATE TABLE IF NOT EXISTS connections (
	connection_id TEXT PRIMARY KEY,
	host TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL DEFAULT 22 CHECK (port >= 1 AND port <= 65535),
	username TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	bearer_key_ref TEXT NOT NULL DEFAULT '',
	ssh_key_ref TEXT NOT NULL DEFAULT '',
	last_connected_at TEXT NOT NULL DEFAULT '',
	schema_version INTEGER NOT NULL
)`

	AddProjectConnectionIDColumnSQL = `ALTER TABLE projects ADD COLUMN connection_id TEXT`

	BackfillProjectConnectionIDSQL = `UPDATE projects
	SET connection_id = COALESCE(NULLIF(TRIM(connection_id), ''), NULLIF(TRIM(vps_id), ''), ?)`

	CreateProjectConnectionIndexSQL = `CREATE INDEX IF NOT EXISTS projects_connection_id_idx
	ON projects(connection_id)`
)

var (
	ErrInvalidConnection      = errors.New("connections: invalid connection")
	ErrMultipleConnections    = errors.New("connections: multiple connections are not supported in v1 migration")
	ErrProjectConnectionDrift = errors.New("connections: project maps to a different connection")
	ErrDuplicateConnectionID  = errors.New("connections: duplicate connection id")
	ErrSchemaStatementMissing = errors.New("connections: schema statement missing")
)

type Status string

const (
	StatusUnconfigured     Status = "unconfigured"
	StatusSSHProbing       Status = "ssh_probing"
	StatusBootstrapping    Status = "bootstrapping"
	StatusTunnelConnecting Status = "tunnel_connecting"
	StatusAuthenticating   Status = "authenticating"
	StatusReady            Status = "ready"
	StatusDegraded         Status = "degraded"
	StatusReconnecting     Status = "reconnecting"
	StatusDisconnected     Status = "disconnected"
)

func (s Status) Valid() bool {
	switch s {
	case StatusUnconfigured,
		StatusSSHProbing,
		StatusBootstrapping,
		StatusTunnelConnecting,
		StatusAuthenticating,
		StatusReady,
		StatusDegraded,
		StatusReconnecting,
		StatusDisconnected:
		return true
	default:
		return false
	}
}

// Connection is the future top-level VPS connection record. It stores secret
// references only; raw bearer tokens and private key material stay in the
// daemon secret store.
type Connection struct {
	ConnectionID    string     `json:"connectionId"`
	Host            string     `json:"host,omitempty"`
	Port            int        `json:"port"`
	Username        string     `json:"username,omitempty"`
	Status          Status     `json:"status"`
	BearerKeyRef    string     `json:"bearerKeyRef,omitempty"`
	SSHKeyRef       string     `json:"sshKeyRef,omitempty"`
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"`
	SchemaVersion   int        `json:"schemaVersion"`
}

type ProjectRecord struct {
	ProjectID    string
	VPSID        string
	ConnectionID string
}

type ProjectBackfill struct {
	ProjectID         string
	LegacyVPSID       string
	ConnectionID      string
	AlreadyBackfilled bool
}

type MigrationPlan struct {
	SchemaVersion       int
	TargetConnection    Connection
	CreateConnection    bool
	ProjectBackfills    []ProjectBackfill
	SchemaSQLStatements []string
}

func DefaultV1Connection() Connection {
	return Connection{
		ConnectionID:  DefaultConnectionID,
		Port:          DefaultSSHPort,
		Status:        StatusDisconnected,
		SchemaVersion: SchemaVersion,
	}
}

func SchemaSQLStatements() []string {
	return []string{
		CreateConnectionsTableSQL,
		AddProjectConnectionIDColumnSQL,
		BackfillProjectConnectionIDSQL,
		CreateProjectConnectionIndexSQL,
	}
}

func BackfillConnectionID(vpsID string) string {
	id := strings.TrimSpace(vpsID)
	if id == "" {
		return DefaultConnectionID
	}
	return id
}

func PlanSingleVPSMigration(projects []ProjectRecord, existing []Connection) (MigrationPlan, error) {
	target, create, err := targetConnection(existing)
	if err != nil {
		return MigrationPlan{}, err
	}

	backfills := make([]ProjectBackfill, 0, len(projects))
	for _, project := range projects {
		legacyID := BackfillConnectionID(project.VPSID)
		connectionID := strings.TrimSpace(project.ConnectionID)
		if connectionID == "" {
			connectionID = legacyID
		}
		if connectionID != target.ConnectionID || legacyID != target.ConnectionID {
			return MigrationPlan{}, fmt.Errorf("%w: project %s maps legacy=%q connection=%q target=%q",
				ErrProjectConnectionDrift, project.ProjectID, legacyID, connectionID, target.ConnectionID)
		}
		backfills = append(backfills, ProjectBackfill{
			ProjectID:         strings.TrimSpace(project.ProjectID),
			LegacyVPSID:       legacyID,
			ConnectionID:      connectionID,
			AlreadyBackfilled: strings.TrimSpace(project.ConnectionID) == connectionID,
		})
	}

	statements := SchemaSQLStatements()
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			return MigrationPlan{}, ErrSchemaStatementMissing
		}
	}
	return MigrationPlan{
		SchemaVersion:       SchemaVersion,
		TargetConnection:    target,
		CreateConnection:    create,
		ProjectBackfills:    backfills,
		SchemaSQLStatements: statements,
	}, nil
}

func (c Connection) Normalized() Connection {
	out := c
	out.ConnectionID = strings.TrimSpace(out.ConnectionID)
	out.Host = strings.TrimSpace(out.Host)
	out.Username = strings.TrimSpace(out.Username)
	out.BearerKeyRef = strings.TrimSpace(out.BearerKeyRef)
	out.SSHKeyRef = strings.TrimSpace(out.SSHKeyRef)
	if out.Port == 0 {
		out.Port = DefaultSSHPort
	}
	if out.SchemaVersion == 0 {
		out.SchemaVersion = SchemaVersion
	}
	if out.Status == "" {
		out.Status = StatusDisconnected
	}
	return out
}

func (c Connection) Validate() error {
	c = c.Normalized()
	if c.ConnectionID == "" {
		return fmt.Errorf("%w: connection id is required", ErrInvalidConnection)
	}
	if strings.ContainsAny(c.ConnectionID, " \t\r\n/\\") {
		return fmt.Errorf("%w: connection id %q is not route-safe", ErrInvalidConnection, c.ConnectionID)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("%w: port %d outside 1..65535", ErrInvalidConnection, c.Port)
	}
	if !c.Status.Valid() {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidConnection, c.Status)
	}
	if strings.Contains(c.Host, "://") {
		return fmt.Errorf("%w: host must not include a URL scheme", ErrInvalidConnection)
	}
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema version %d", ErrInvalidConnection, c.SchemaVersion)
	}
	return nil
}

func targetConnection(existing []Connection) (Connection, bool, error) {
	if len(existing) == 0 {
		return DefaultV1Connection(), true, nil
	}

	seen := map[string]struct{}{}
	var target Connection
	for _, connection := range existing {
		connection = connection.Normalized()
		if err := connection.Validate(); err != nil {
			return Connection{}, false, err
		}
		if _, ok := seen[connection.ConnectionID]; ok {
			return Connection{}, false, fmt.Errorf("%w: %s", ErrDuplicateConnectionID, connection.ConnectionID)
		}
		seen[connection.ConnectionID] = struct{}{}
		if target.ConnectionID == "" {
			target = connection
			continue
		}
		return Connection{}, false, ErrMultipleConnections
	}
	if target.ConnectionID != DefaultConnectionID {
		return Connection{}, false, fmt.Errorf("%w: %s", ErrMultipleConnections, target.ConnectionID)
	}
	return target, false, nil
}
