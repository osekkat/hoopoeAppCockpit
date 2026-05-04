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
	abs := filepath.Join(l.Root, rel)
	cleanRoot, err := filepath.Abs(l.Root)
	if err != nil {
		return "", err
	}
	cleanAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
		return "", ErrUnsafePath
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
