// Package cli wraps subscription-backed agent CLIs behind typed daemon calls.
//
// The package is intentionally narrow: callers choose one of the supported
// harnesses, provide a prompt, and receive streamed stdout/stderr chunks plus
// a final result. It does not expose arbitrary shell execution and it does not
// import or configure direct provider SDKs.
package cli

import (
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/modelcontext"
)

type Harness string

const (
	HarnessClaudeCode Harness = "claude_code"
	HarnessCodexCLI   Harness = "codex_cli"
	HarnessGeminiCLI  Harness = "gemini_cli"
)

type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

type StreamChunk struct {
	Harness Harness
	Stream  Stream
	Data    []byte
	At      time.Time
}

type RunRequest struct {
	Prompt         string
	Model          string
	AccountID      string
	MaxTokens      int
	Timeout        time.Duration
	WorkDir        string
	Env            []string
	PlanID         string
	CandidateSlug  string
	Context        ContextManifest
	ContextStage   modelcontext.Stage
	ContextPolicy  *modelcontext.Policy
	ContextSources []modelcontext.Source
}

type RunResult struct {
	Harness        Harness
	Model          string
	AccountID      string
	StartedAt      time.Time
	CompletedAt    time.Time
	ExitCode       int
	Stdout         []byte
	Stderr         []byte
	Artifacts      []ArtifactRef
	InputSHA256    string
	StdoutSHA256   string
	StderrSHA256   string
	ManifestSHA256 string
	Error          string
}

type ContextManifest struct {
	Harness         Harness       `json:"harness"`
	Model           string        `json:"model,omitempty"`
	AccountID       string        `json:"accountId,omitempty"`
	PolicyRule      string        `json:"policyRule"`
	SourceRefs      []string      `json:"sourceRefs,omitempty"`
	InputArtifacts  []ArtifactRef `json:"inputArtifacts,omitempty"`
	Redactions      []string      `json:"redactions,omitempty"`
	MaxContextBytes int64         `json:"maxContextBytes,omitempty"`
	InputSHA256     string        `json:"inputSha256"`
	GeneratedAt     time.Time     `json:"generatedAt"`
}

type HarnessConfig struct {
	Harness         Harness
	Binary          string
	Args            []string
	StdinArg        string
	ModelFlag       string
	MaxTokensFlag   string
	SupportedModels []string
	DefaultTimeout  time.Duration
	MaxOutputBytes  int64
}

type CommandSpec struct {
	Binary         string
	Args           []string
	Stdin          []byte
	Dir            string
	Env            []string
	Timeout        time.Duration
	MaxOutputBytes int64
}

type CommandResult struct {
	ExitCode    int
	Stdout      []byte
	Stderr      []byte
	StartedAt   time.Time
	CompletedAt time.Time
}

type ArtifactKind string

const (
	ArtifactCandidateMarkdown ArtifactKind = "candidate_markdown"
	ArtifactStdout            ArtifactKind = "stdout"
	ArtifactStderr            ArtifactKind = "stderr"
	ArtifactContextManifest   ArtifactKind = "context_manifest"
)

type Artifact struct {
	Kind          ArtifactKind
	PlanID        string
	CandidateSlug string
	Content       []byte
	MediaType     string
}

type ArtifactRef struct {
	Kind    ArtifactKind `json:"kind"`
	Path    string       `json:"path"`
	SHA256  string       `json:"sha256"`
	Size    int64        `json:"size"`
	Written bool         `json:"written"`
}

type AuditEventType string

const (
	AuditModelCallStarted   AuditEventType = "model_call.started"
	AuditModelCallCompleted AuditEventType = "model_call.completed"
)

type AuditEvent struct {
	Type           AuditEventType
	Harness        Harness
	Model          string
	AccountID      string
	InputSHA256    string
	StdoutSHA256   string
	StderrSHA256   string
	ManifestSHA256 string
	ArtifactRefs   []ArtifactRef
	ExitCode       int
	Error          string
	At             time.Time
}
