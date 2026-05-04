package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Registry struct {
	adapters map[Harness]*Adapter
}

func NewRegistry(adapters ...*Adapter) (*Registry, error) {
	r := &Registry{adapters: map[Harness]*Adapter{}}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		harness := adapter.Config.Harness
		if harness == "" {
			return nil, fmt.Errorf("%w: empty harness", ErrInvalidRequest)
		}
		if _, exists := r.adapters[harness]; exists {
			return nil, fmt.Errorf("%w: duplicate harness %s", ErrInvalidRequest, harness)
		}
		r.adapters[harness] = adapter
	}
	return r, nil
}

func NewDefaultRegistry(exec Executor, artifacts ArtifactStore, accounts AccountRouter, audit AuditSink) *Registry {
	adapters := make([]*Adapter, 0, 3)
	for _, config := range DefaultHarnessConfigs() {
		adapter := NewAdapter(config, exec)
		adapter.Artifacts = artifacts
		adapter.AccountRouter = accounts
		adapter.Audit = audit
		adapters = append(adapters, adapter)
	}
	registry, err := NewRegistry(adapters...)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *Registry) Harnesses() []Harness {
	if r == nil {
		return nil
	}
	out := make([]Harness, 0, len(r.adapters))
	for harness := range r.adapters {
		out = append(out, harness)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (r *Registry) Adapter(harness Harness) (*Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.adapters[harness]
	return adapter, ok
}

func (r *Registry) Supports(harness Harness, model string) bool {
	adapter, ok := r.Adapter(harness)
	return ok && adapter.Supports(model)
}

func (r *Registry) Run(ctx context.Context, harness Harness, req RunRequest, onChunk func(StreamChunk) error) (RunResult, error) {
	adapter, ok := r.Adapter(harness)
	if !ok {
		return RunResult{}, fmt.Errorf("%w: unknown harness %s", ErrInvalidRequest, harness)
	}
	return adapter.Run(ctx, req, onChunk)
}

func DefaultHarnessConfigs() []HarnessConfig {
	return []HarnessConfig{
		{
			Harness:         HarnessClaudeCode,
			Binary:          "claude",
			Args:            []string{"--print", "--input-format", "text", "--output-format", "stream-json", "--include-partial-messages"},
			ModelFlag:       "--model",
			SupportedModels: []string{"opus", "sonnet", "claude-opus", "claude-sonnet"},
			DefaultTimeout:  30 * time.Minute,
			MaxOutputBytes:  32 << 20,
		},
		{
			Harness:         HarnessCodexCLI,
			Binary:          "codex",
			Args:            []string{"exec", "--json"},
			StdinArg:        "-",
			ModelFlag:       "--model",
			SupportedModels: []string{"gpt-5", "gpt-5.1", "gpt-5.2", "gpt-5.3", "gpt-5.4", "gpt-5.5", "codex"},
			DefaultTimeout:  30 * time.Minute,
			MaxOutputBytes:  32 << 20,
		},
		{
			Harness:         HarnessGeminiCLI,
			Binary:          "gemini",
			Args:            []string{"--prompt", "", "--output-format", "stream-json"},
			ModelFlag:       "--model",
			SupportedModels: []string{"gemini", "gemini-pro", "gemini-3", "gemini-3-pro"},
			DefaultTimeout:  30 * time.Minute,
			MaxOutputBytes:  32 << 20,
		},
	}
}

func NormalizeCandidateSlug(model string, harness Harness) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model != "" {
		return sanitizeSlug(model)
	}
	return sanitizeSlug(strings.ReplaceAll(string(harness), "_", "-"))
}

func sanitizeSlug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "candidate"
	}
	return out
}
