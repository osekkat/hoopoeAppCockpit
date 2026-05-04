package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrInvalidArtifact      = errors.New("cli: invalid artifact")
	ErrUnsafeArtifactPath   = errors.New("cli: unsafe artifact path")
	ErrUnsupportedArtifact  = errors.New("cli: unsupported artifact kind")
	ErrArtifactStoreMissing = errors.New("cli: artifact store missing")
)

type ArtifactStore interface {
	Write(ctx context.Context, artifact Artifact) (ArtifactRef, error)
}

type FileArtifactStore struct {
	Root string
}

func (s FileArtifactStore) Write(ctx context.Context, artifact Artifact) (ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	if strings.TrimSpace(s.Root) == "" {
		return ArtifactRef{}, fmt.Errorf("%w: empty root", ErrInvalidArtifact)
	}
	rel, err := artifactRelativePath(artifact)
	if err != nil {
		return ArtifactRef{}, err
	}
	abs := filepath.Join(s.Root, rel)
	cleanRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return ArtifactRef{}, err
	}
	cleanAbs, err := filepath.Abs(abs)
	if err != nil {
		return ArtifactRef{}, err
	}
	if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
		return ArtifactRef{}, ErrUnsafeArtifactPath
	}
	if err := os.MkdirAll(filepath.Dir(cleanAbs), 0o700); err != nil {
		return ArtifactRef{}, fmt.Errorf("cli: mkdir artifact parent: %w", err)
	}
	if err := os.WriteFile(cleanAbs, artifact.Content, 0o600); err != nil {
		return ArtifactRef{}, fmt.Errorf("cli: write artifact: %w", err)
	}
	return ArtifactRef{
		Kind:    artifact.Kind,
		Path:    filepath.ToSlash(rel),
		SHA256:  digestBytes(artifact.Content),
		Size:    int64(len(artifact.Content)),
		Written: true,
	}, nil
}

func artifactRelativePath(artifact Artifact) (string, error) {
	if !safeSegment(artifact.PlanID) {
		return "", fmt.Errorf("%w: plan id", ErrUnsafeArtifactPath)
	}
	switch artifact.Kind {
	case ArtifactCandidateMarkdown:
		if !safeSegment(artifact.CandidateSlug) {
			return "", fmt.Errorf("%w: candidate slug", ErrUnsafeArtifactPath)
		}
		return filepath.Join(".hoopoe", "plans", artifact.PlanID, "candidates", artifact.CandidateSlug+".md"), nil
	case ArtifactStdout:
		if !safeSegment(artifact.CandidateSlug) {
			return "", fmt.Errorf("%w: candidate slug", ErrUnsafeArtifactPath)
		}
		return filepath.Join(".hoopoe", "plans", artifact.PlanID, "artifacts", artifact.CandidateSlug+".stdout.txt"), nil
	case ArtifactStderr:
		if !safeSegment(artifact.CandidateSlug) {
			return "", fmt.Errorf("%w: candidate slug", ErrUnsafeArtifactPath)
		}
		return filepath.Join(".hoopoe", "plans", artifact.PlanID, "artifacts", artifact.CandidateSlug+".stderr.txt"), nil
	case ArtifactContextManifest:
		if !safeSegment(artifact.CandidateSlug) {
			return "", fmt.Errorf("%w: candidate slug", ErrUnsafeArtifactPath)
		}
		return filepath.Join(".hoopoe", "plans", artifact.PlanID, "artifacts", artifact.CandidateSlug+".context.json"), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedArtifact, artifact.Kind)
	}
}

func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
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
	return !strings.Contains(s, "..")
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func digestJSON(v any) ([]byte, string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return b, digestBytes(b), nil
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		now = time.Now
	}
	return now().UTC()
}
