package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// store.go owns the projects-package SQLite-backed persistence layer:
// the SQLStore type and its CRUD methods, idempotency record helpers,
// and the row-scanning machinery.
//
// hp-j6zr first cut: split out of projects.go (1220 lines mixing
// store, service orchestration, .hoopoe initialization, RU discovery,
// agent contract generation, and JSON/file helpers). Behavior is
// unchanged — same package, same exported names, same SQL. The
// idempotency contract test (hp-jsi/hp-cjmc) and existing project-
// service integration tests keep pinning behavior across the cut.

// StoreProject is the daemon-side persistence shape for a project
// registry row. It mirrors the SQL columns 1:1 and is converted to
// the wire-shape `schemas.Project` by toSchemaProject (in projects.go).
type StoreProject struct {
	ID                    string
	Slug                  string
	Name                  string
	VPSID                 string
	RootPath              string
	OriginRemote          string
	Branch                string
	LifecycleState        schemas.ProjectLifecycleState
	AgentsManifestPresent bool
	HoopoeInitialized     bool
	ToolDetectionDone     bool
	DesktopMirrorPath     string
	ImportedAt            time.Time
	LastActivityAt        time.Time
	Tools                 ToolEnvironment
	SchemaVersion         int
}

// SQLStore implements the project registry persistence layer over
// SQLite. The schema is bootstrapped lazily by ensureSchema on first
// construction; idempotency keys live in a sibling table so retries
// of POST /v1/projects collapse to the same record.
type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(ctx context.Context, db *sql.DB) (*SQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil db", ErrInvalidRequest)
	}
	store := &SQLStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLStore) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			vps_id TEXT NOT NULL,
			root_path TEXT NOT NULL UNIQUE,
			origin_remote TEXT NOT NULL,
			branch TEXT NOT NULL,
			lifecycle_state TEXT NOT NULL,
			agents_manifest_present INTEGER NOT NULL,
			hoopoe_initialized INTEGER NOT NULL,
			tool_detection_done INTEGER NOT NULL,
			desktop_mirror_path TEXT NOT NULL DEFAULT '',
			imported_at TEXT NOT NULL,
			last_activity_at TEXT NOT NULL,
			tools_json TEXT NOT NULL,
			schema_version INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS project_idempotency (
			key TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL,
			project_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("projects: ensure schema: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) List(ctx context.Context) ([]StoreProject, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects ORDER BY imported_at DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("projects: list: %w", err)
	}
	defer rows.Close()
	var out []StoreProject
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projects: list rows: %w", err)
	}
	return out, nil
}

func (s *SQLStore) Get(ctx context.Context, id string) (StoreProject, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE id = ?`, strings.TrimSpace(id))
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, ErrNotFound
	}
	return project, err
}

func (s *SQLStore) FindByRoot(ctx context.Context, root string) (StoreProject, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE root_path = ?`, root)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, false, nil
	}
	if err != nil {
		return StoreProject{}, false, err
	}
	return project, true, nil
}

func (s *SQLStore) Create(ctx context.Context, project StoreProject, idempotencyKey, requestHash string) (StoreProject, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: begin create: %w", err)
	}
	defer tx.Rollback()

	if idempotencyKey != "" {
		existing, err := lookupIdempotency(ctx, tx, idempotencyKey)
		if err != nil {
			return StoreProject{}, err
		}
		if existing != nil {
			if existing.requestHash != requestHash {
				return StoreProject{}, ErrIdempotencyConflict
			}
			out, err := getTx(ctx, tx, existing.projectID)
			if err != nil {
				return StoreProject{}, err
			}
			return out, tx.Commit()
		}
	}

	if existing, ok, err := findByRootTx(ctx, tx, project.RootPath); err != nil {
		return StoreProject{}, err
	} else if ok {
		if idempotencyKey != "" {
			if err := insertIdempotency(ctx, tx, idempotencyKey, requestHash, existing.ID, project.LastActivityAt); err != nil {
				return StoreProject{}, err
			}
		}
		return existing, tx.Commit()
	}

	toolsJSON, err := json.Marshal(project.Tools)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: marshal tools: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, slug, name, vps_id, root_path, origin_remote, branch, lifecycle_state,
		agents_manifest_present, hoopoe_initialized, tool_detection_done, desktop_mirror_path,
		imported_at, last_activity_at, tools_json, schema_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		project.ID, project.Slug, project.Name, project.VPSID, project.RootPath, project.OriginRemote,
		project.Branch, string(project.LifecycleState), boolInt(project.AgentsManifestPresent),
		boolInt(project.HoopoeInitialized), boolInt(project.ToolDetectionDone), project.DesktopMirrorPath,
		project.ImportedAt.UTC().Format(time.RFC3339Nano), project.LastActivityAt.UTC().Format(time.RFC3339Nano),
		string(toolsJSON), project.SchemaVersion,
	); err != nil {
		return StoreProject{}, fmt.Errorf("projects: insert: %w", err)
	}
	if idempotencyKey != "" {
		if err := insertIdempotency(ctx, tx, idempotencyKey, requestHash, project.ID, project.LastActivityAt); err != nil {
			return StoreProject{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return StoreProject{}, fmt.Errorf("projects: commit create: %w", err)
	}
	return project, nil
}

func (s *SQLStore) UpdateToolEnvironment(ctx context.Context, id string, tools ToolEnvironment, at time.Time) (StoreProject, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return StoreProject{}, fmt.Errorf("%w: project id is required", ErrInvalidRequest)
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: marshal tools: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE projects
		SET agents_manifest_present = ?,
			hoopoe_initialized = ?,
			tool_detection_done = ?,
			last_activity_at = ?,
			tools_json = ?
		WHERE id = ?`,
		boolInt(tools.AgentsMDRelative != nil),
		boolInt(tools.HasHoopoeDir),
		1,
		at.UTC().Format(time.RFC3339Nano),
		string(toolsJSON),
		id,
	)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: update tools: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: update tools rows affected: %w", err)
	}
	if affected == 0 {
		return StoreProject{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// idempotencyRecord is the bind shape for the project_idempotency
// table — used by Create to short-circuit retries to the same project.
type idempotencyRecord struct {
	requestHash string
	projectID   string
}

func lookupIdempotency(ctx context.Context, tx *sql.Tx, key string) (*idempotencyRecord, error) {
	var rec idempotencyRecord
	err := tx.QueryRowContext(ctx, `SELECT request_hash, project_id FROM project_idempotency WHERE key = ?`, key).Scan(&rec.requestHash, &rec.projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("projects: lookup idempotency: %w", err)
	}
	return &rec, nil
}

func insertIdempotency(ctx context.Context, tx *sql.Tx, key, requestHash, projectID string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_idempotency (key, request_hash, project_id, created_at)
		VALUES (?, ?, ?, ?)`, key, requestHash, projectID, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("projects: insert idempotency: %w", err)
	}
	return nil
}

func getTx(ctx context.Context, tx *sql.Tx, id string) (StoreProject, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE id = ?`, id)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, ErrNotFound
	}
	return project, err
}

func findByRootTx(ctx context.Context, tx *sql.Tx, root string) (StoreProject, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE root_path = ?`, root)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, false, nil
	}
	if err != nil {
		return StoreProject{}, false, err
	}
	return project, true, nil
}

// scanner abstracts the row-scanning surface so scanProject can serve
// both *sql.Row (single row) and *sql.Rows (iteration) call sites.
type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (StoreProject, error) {
	var project StoreProject
	var lifecycle string
	var importedAt string
	var lastActivityAt string
	var toolsJSON string
	var agents int
	var hoopoe int
	var tools int
	if err := row.Scan(&project.ID, &project.Slug, &project.Name, &project.VPSID, &project.RootPath,
		&project.OriginRemote, &project.Branch, &lifecycle, &agents, &hoopoe, &tools,
		&project.DesktopMirrorPath, &importedAt, &lastActivityAt, &toolsJSON, &project.SchemaVersion); err != nil {
		return StoreProject{}, err
	}
	project.LifecycleState = schemas.ProjectLifecycleState(lifecycle)
	project.AgentsManifestPresent = agents == 1
	project.HoopoeInitialized = hoopoe == 1
	project.ToolDetectionDone = tools == 1
	var err error
	project.ImportedAt, err = time.Parse(time.RFC3339Nano, importedAt)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: parse imported_at: %w", err)
	}
	project.LastActivityAt, err = time.Parse(time.RFC3339Nano, lastActivityAt)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: parse last_activity_at: %w", err)
	}
	if err := json.Unmarshal([]byte(toolsJSON), &project.Tools); err != nil {
		return StoreProject{}, fmt.Errorf("projects: decode tools: %w", err)
	}
	return project, nil
}
