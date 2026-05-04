package modelcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ManifestStore struct {
	ProjectRoot string
}

type ManifestRef struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Written bool   `json:"written"`
}

func (s ManifestStore) Write(ctx context.Context, manifest Manifest) (ManifestRef, error) {
	if err := ctx.Err(); err != nil {
		return ManifestRef{}, err
	}
	root := strings.TrimSpace(s.ProjectRoot)
	if root == "" {
		return ManifestRef{}, fmt.Errorf("%w: project root is required", ErrUnsafeManifestRef)
	}
	if strings.TrimSpace(manifest.ManifestID) == "" {
		manifest.ManifestID = manifestID(manifest)
	}
	if !safeSegment(manifest.ManifestID) || !safeSegment(string(manifest.Stage)) {
		return ManifestRef{}, fmt.Errorf("%w: manifest id or stage", ErrUnsafeManifestRef)
	}
	rel := filepath.Join(".hoopoe", "context-manifests", string(manifest.Stage), manifest.ManifestID+".json")
	abs := filepath.Join(root, rel)
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return ManifestRef{}, err
	}
	cleanAbs, err := filepath.Abs(abs)
	if err != nil {
		return ManifestRef{}, err
	}
	if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
		return ManifestRef{}, ErrUnsafeManifestRef
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ManifestRef{}, err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(cleanAbs), 0o700); err != nil {
		return ManifestRef{}, fmt.Errorf("modelcontext: mkdir manifest parent: %w", err)
	}
	if err := os.WriteFile(cleanAbs, body, 0o600); err != nil {
		return ManifestRef{}, fmt.Errorf("modelcontext: write manifest: %w", err)
	}
	return ManifestRef{
		Path:    filepath.ToSlash(rel),
		SHA256:  digestBytes(body),
		Size:    int64(len(body)),
		Written: true,
	}, nil
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
