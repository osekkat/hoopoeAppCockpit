package beadflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrInvalidEvidence = errors.New("beadflow: invalid evidence entry")
	ErrUnsafePath      = errors.New("beadflow: unsafe path")
)

type EvidenceLedger struct {
	Root string
	Now  func() time.Time
}

type ArtifactStore struct {
	Root string
}

type WrittenArtifact struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

func (s ArtifactStore) WriteConversionArtifacts(ctx context.Context, plan ConversionPlan) ([]WrittenArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !safeSegment(plan.PlanID) {
		return nil, fmt.Errorf("%w: plan id", ErrUnsafePath)
	}
	payloads := map[string]any{
		"traceability":    plan.Traceability,
		"quality_report":  plan.Quality,
		"polish_plan":     plan.Polish,
		"bv_graph_health": plan.Graph,
	}
	paths := BeadflowArtifactPaths(plan.PlanID)
	written := make([]WrittenArtifact, 0, len(paths))
	for _, artifact := range paths {
		payload, ok := payloads[artifact.Kind]
		if !ok {
			return nil, fmt.Errorf("beadflow: no payload for artifact kind %q", artifact.Kind)
		}
		bytes, err := s.writeJSON(ctx, artifact.Path, payload)
		if err != nil {
			return nil, err
		}
		written = append(written, WrittenArtifact{
			Kind:  artifact.Kind,
			Path:  artifact.Path,
			Bytes: bytes,
		})
	}
	return written, nil
}

func (s ArtifactStore) writeJSON(ctx context.Context, rel string, payload any) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	abs, err := resolveUnderRoot(s.Root, rel)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return 0, err
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".tmp-"+filepath.Base(abs)+"-*")
	if err != nil {
		return 0, err
	}
	if _, err := tmp.Write(encoded); err != nil {
		return 0, closeWithError(tmp, err)
	}
	if err := tmp.Sync(); err != nil {
		return 0, closeWithError(tmp, err)
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		return 0, err
	}
	return len(encoded), nil
}

func (l EvidenceLedger) Append(ctx context.Context, planID string, entry EvidenceEntry) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !safeSegment(planID) {
		return "", fmt.Errorf("%w: plan id", ErrUnsafePath)
	}
	if strings.TrimSpace(l.Root) == "" {
		return "", fmt.Errorf("%w: empty root", ErrUnsafePath)
	}
	if strings.TrimSpace(entry.BeadID) == "" {
		return "", fmt.Errorf("%w: bead id required", ErrInvalidEvidence)
	}
	if entry.Kind == "" {
		return "", fmt.Errorf("%w: kind required", ErrInvalidEvidence)
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}
	entry.SchemaVersion = EvidenceSchemaVersion
	entry.PlanID = planID
	if entry.Time.IsZero() {
		entry.Time = now().UTC()
	}
	rel := filepath.Join(".hoopoe", "plans", planID, "implementation-evidence.jsonl")
	cleanAbs, err := resolveUnderRoot(l.Root, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cleanAbs), 0o700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(cleanAbs, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return "", closeWithError(file, err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return "", closeWithError(file, err)
	}
	if err := file.Sync(); err != nil {
		return "", closeWithError(file, err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func resolveUnderRoot(root, rel string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w: empty root", ErrUnsafePath)
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", ErrUnsafePath
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanAbs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
		return "", ErrUnsafePath
	}
	return cleanAbs, nil
}

func closeWithError(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
