package planning

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
	"sync"
)

var ErrPromptNotFound = errors.New("planning: prompt not found")

type MapPromptSource map[string]Prompt

func (m MapPromptSource) LoadPrompt(ctx context.Context, id string) (Prompt, error) {
	if err := ctx.Err(); err != nil {
		return Prompt{}, err
	}
	prompt, ok := m[id]
	if !ok {
		return Prompt{}, fmt.Errorf("%w: %s", ErrPromptNotFound, id)
	}
	if prompt.ID == "" {
		prompt.ID = id
	}
	return prompt, nil
}

type FilePromptSource struct {
	Root string

	mu       sync.Mutex
	manifest *promptManifest
	baseDir  string
}

type promptManifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Prompts       []promptManifestEntry `json:"prompts"`
}

type promptManifestEntry struct {
	ID      string   `json:"id"`
	Version int      `json:"version"`
	Path    string   `json:"path"`
	Hash    string   `json:"hash"`
	Owner   string   `json:"owner"`
	Applies []string `json:"appliesToPipelineVersions"`
}

func (s *FilePromptSource) LoadPrompt(ctx context.Context, id string) (Prompt, error) {
	if err := ctx.Err(); err != nil {
		return Prompt{}, err
	}
	manifest, base, err := s.loadManifest(ctx)
	if err != nil {
		return Prompt{}, err
	}
	for _, entry := range manifest.Prompts {
		if entry.ID != id {
			continue
		}
		path := filepath.Join(base, filepath.FromSlash(entry.Path))
		body, err := os.ReadFile(path)
		if err != nil {
			return Prompt{}, err
		}
		hash := "sha256:" + digestBytes(body)
		if strings.TrimSpace(entry.Hash) != "" && entry.Hash != hash {
			return Prompt{}, fmt.Errorf("%w: prompt %s hash mismatch", ErrInvalidRequest, id)
		}
		return Prompt{
			ID:      entry.ID,
			Version: entry.Version,
			Hash:    hash,
			Body:    string(body),
		}, nil
	}
	return Prompt{}, fmt.Errorf("%w: %s", ErrPromptNotFound, id)
}

func (s *FilePromptSource) loadManifest(ctx context.Context) (promptManifest, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest != nil {
		return *s.manifest, s.baseDir, nil
	}
	if err := ctx.Err(); err != nil {
		return promptManifest{}, "", err
	}
	base, err := promptBaseDir(s.Root)
	if err != nil {
		return promptManifest{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		return promptManifest{}, "", err
	}
	var manifest promptManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return promptManifest{}, "", err
	}
	if manifest.SchemaVersion != 1 {
		return promptManifest{}, "", fmt.Errorf("%w: unsupported prompt manifest schema %d", ErrInvalidRequest, manifest.SchemaVersion)
	}
	copied := manifest
	s.manifest = &copied
	s.baseDir = base
	return copied, base, nil
}

func promptBaseDir(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: prompt root is required", ErrInvalidRequest)
	}
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err == nil {
		return root, nil
	}
	candidate := filepath.Join(root, "packages", "planning-prompts")
	if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("%w: planning prompt manifest not found under %s", ErrInvalidRequest, root)
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
