// Package modelcontext owns project model-context policy enforcement and
// per-call context manifests. It records what reaches subscription-backed
// model harnesses without ever storing raw model provider credentials.
package modelcontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const SchemaVersion = 1

var (
	ErrInvalidPolicy     = errors.New("modelcontext: invalid policy")
	ErrPolicyDenied      = errors.New("modelcontext: policy denied model context")
	ErrContextTooLarge   = errors.New("modelcontext: context too large")
	ErrProviderDenied    = errors.New("modelcontext: provider not allowed")
	ErrSecretBlocked     = errors.New("modelcontext: sensitive content blocked")
	ErrUnsafeManifestRef = errors.New("modelcontext: unsafe manifest reference")
)

type Stage string

const (
	StagePlanning Stage = "planning"
	StageBeads    Stage = "beads"
	StageSwarm    Stage = "swarm"
	StageTending  Stage = "tending"
	StageReview   Stage = "review"
)

type RawSourceMode string

const (
	RawSourceNever         RawSourceMode = "never"
	RawSourceSummariesOnly RawSourceMode = "summaries_only"
	RawSourceAllowed       RawSourceMode = "allowed"
)

type SecretHandling string

const (
	SecretHandlingRedact SecretHandling = "redact"
	SecretHandlingBlock  SecretHandling = "block"
)

type PolicyFile struct {
	SchemaVersion int    `json:"schemaVersion"`
	ContextPolicy Policy `json:"contextPolicy"`
}

type Policy struct {
	RawSourceMode    RawSourceMode         `json:"rawSourceMode"`
	IncludeAuditLog  bool                  `json:"includeAuditLog"`
	IncludeLogs      bool                  `json:"includeLogs"`
	IncludeFileGlobs []string              `json:"includeFileGlobs"`
	ExcludeFileGlobs []string              `json:"excludeFileGlobs"`
	MaxContextBytes  int64                 `json:"maxContextBytes"`
	SecretHandling   SecretHandling        `json:"secretHandling"`
	HarnessAllowlist []string              `json:"harnessAllowlist"`
	StageDefaults    map[Stage]StagePolicy `json:"stageDefaults"`
}

type StagePolicy struct {
	RawSourceMode   RawSourceMode  `json:"rawSourceMode,omitempty"`
	MaxContextBytes int64          `json:"maxContextBytes,omitempty"`
	SecretHandling  SecretHandling `json:"secretHandling,omitempty"`
}

func DefaultPolicyFile() PolicyFile {
	return PolicyFile{
		SchemaVersion: SchemaVersion,
		ContextPolicy: DefaultPolicy(),
	}
}

func DefaultPolicy() Policy {
	return Policy{
		RawSourceMode:    RawSourceSummariesOnly,
		IncludeAuditLog:  false,
		IncludeLogs:      false,
		IncludeFileGlobs: []string{},
		ExcludeFileGlobs: []string{
			".env",
			".env.*",
			"**/.env",
			"**/.env.*",
			"**/secrets/**",
			"**/.ssh/**",
			"**/*_rsa",
			"**/*_ed25519",
			"**/id_rsa",
			"**/id_ed25519",
		},
		MaxContextBytes: 256 * 1024,
		SecretHandling:  SecretHandlingRedact,
		HarnessAllowlist: []string{
			"claude_code",
			"codex_cli",
			"gemini_cli",
			"oracle_browser",
		},
		StageDefaults: map[Stage]StagePolicy{
			StagePlanning: {RawSourceMode: RawSourceSummariesOnly, MaxContextBytes: 512 * 1024, SecretHandling: SecretHandlingRedact},
			StageBeads:    {RawSourceMode: RawSourceSummariesOnly, MaxContextBytes: 256 * 1024, SecretHandling: SecretHandlingRedact},
			StageSwarm:    {RawSourceMode: RawSourceNever, MaxContextBytes: 128 * 1024, SecretHandling: SecretHandlingBlock},
			StageTending:  {RawSourceMode: RawSourceNever, MaxContextBytes: 128 * 1024, SecretHandling: SecretHandlingBlock},
			StageReview:   {RawSourceMode: RawSourceSummariesOnly, MaxContextBytes: 512 * 1024, SecretHandling: SecretHandlingRedact},
		},
	}
}

func LoadPolicyFile(path string) (PolicyFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return PolicyFile{}, fmt.Errorf("%w: path is required", ErrInvalidPolicy)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return PolicyFile{}, fmt.Errorf("modelcontext: read policy: %w", err)
	}
	var parsed PolicyFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return PolicyFile{}, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
	}
	if err := parsed.Validate(); err != nil {
		return PolicyFile{}, err
	}
	return parsed, nil
}

func WriteDefaultPolicyIfMissing(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%w: path is required", ErrInvalidPolicy)
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("modelcontext: stat policy: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("modelcontext: mkdir policy dir: %w", err)
	}
	body, err := json.MarshalIndent(DefaultPolicyFile(), "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func (f PolicyFile) Validate() error {
	if f.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schemaVersion must be %d", ErrInvalidPolicy, SchemaVersion)
	}
	return f.ContextPolicy.Validate()
}

func (p Policy) Validate() error {
	switch p.RawSourceMode {
	case RawSourceNever, RawSourceSummariesOnly, RawSourceAllowed:
	default:
		return fmt.Errorf("%w: rawSourceMode %q", ErrInvalidPolicy, p.RawSourceMode)
	}
	switch p.SecretHandling {
	case SecretHandlingRedact, SecretHandlingBlock:
	default:
		return fmt.Errorf("%w: secretHandling %q", ErrInvalidPolicy, p.SecretHandling)
	}
	if p.MaxContextBytes <= 0 {
		return fmt.Errorf("%w: maxContextBytes must be positive", ErrInvalidPolicy)
	}
	for _, glob := range append(append([]string{}, p.IncludeFileGlobs...), p.ExcludeFileGlobs...) {
		if strings.TrimSpace(glob) == "" {
			return fmt.Errorf("%w: empty glob", ErrInvalidPolicy)
		}
	}
	for stage, override := range p.StageDefaults {
		if strings.TrimSpace(string(stage)) == "" {
			return fmt.Errorf("%w: empty stage", ErrInvalidPolicy)
		}
		if override.RawSourceMode != "" {
			switch override.RawSourceMode {
			case RawSourceNever, RawSourceSummariesOnly, RawSourceAllowed:
			default:
				return fmt.Errorf("%w: stage %s rawSourceMode %q", ErrInvalidPolicy, stage, override.RawSourceMode)
			}
		}
		if override.SecretHandling != "" {
			switch override.SecretHandling {
			case SecretHandlingRedact, SecretHandlingBlock:
			default:
				return fmt.Errorf("%w: stage %s secretHandling %q", ErrInvalidPolicy, stage, override.SecretHandling)
			}
		}
	}
	return nil
}

func (p Policy) ForStage(stage Stage) Policy {
	out := p
	if override, ok := p.StageDefaults[stage]; ok {
		if override.RawSourceMode != "" {
			out.RawSourceMode = override.RawSourceMode
		}
		if override.MaxContextBytes > 0 {
			out.MaxContextBytes = override.MaxContextBytes
		}
		if override.SecretHandling != "" {
			out.SecretHandling = override.SecretHandling
		}
	}
	return out
}

func (p Policy) Digest() string {
	body, _ := json.Marshal(p)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type Source struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	URI     string `json:"uri,omitempty"`
	Content []byte `json:"-"`
	Raw     bool   `json:"raw,omitempty"`
}

type EvaluationRequest struct {
	ProjectID     string
	Stage         Stage
	Harness       string
	Model         string
	AccountID     string
	PolicyRule    string
	Prompt        []byte
	Sources       []Source
	CorrelationID string
	Now           func() time.Time
}

type EvaluationResult struct {
	Manifest        Manifest
	RedactedSources []Source
}

type Manifest struct {
	SchemaVersion   int               `json:"schemaVersion"`
	ManifestID      string            `json:"manifestId"`
	ProjectID       string            `json:"projectId"`
	Stage           Stage             `json:"stage"`
	Harness         string            `json:"harness"`
	Model           string            `json:"model,omitempty"`
	AccountID       string            `json:"accountId,omitempty"`
	PolicyRule      string            `json:"policyRule"`
	PolicyDigest    string            `json:"policyDigest"`
	InputSHA256     string            `json:"inputSha256"`
	ContextBytes    int64             `json:"contextBytes"`
	MaxContextBytes int64             `json:"maxContextBytes"`
	SourceRefs      []SourceRef       `json:"sourceRefs,omitempty"`
	ExcludedRefs    []ExcludedRef     `json:"excludedRefs,omitempty"`
	Redactions      []RedactionRecord `json:"redactions,omitempty"`
	GeneratedAt     time.Time         `json:"generatedAt"`
	CorrelationID   string            `json:"correlationId,omitempty"`
}

type SourceRef struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	URI       string `json:"uri,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"sizeBytes"`
	Raw       bool   `json:"raw,omitempty"`
}

type ExcludedRef struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	URI    string `json:"uri,omitempty"`
	Reason string `json:"reason"`
}

type RedactionRecord struct {
	SourcePath    string `json:"sourcePath,omitempty"`
	PatternID     string `json:"patternId"`
	Count         int    `json:"count"`
	BytesRedacted int    `json:"bytesRedacted"`
}

func Evaluate(policy Policy, req EvaluationRequest) (EvaluationResult, error) {
	if err := policy.Validate(); err != nil {
		return EvaluationResult{}, err
	}
	stagePolicy := policy.ForStage(req.Stage)
	if err := requireHarnessAllowed(stagePolicy, req.Harness); err != nil {
		return EvaluationResult{}, err
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		ProjectID:       strings.TrimSpace(req.ProjectID),
		Stage:           req.Stage,
		Harness:         strings.TrimSpace(req.Harness),
		Model:           strings.TrimSpace(req.Model),
		AccountID:       strings.TrimSpace(req.AccountID),
		PolicyRule:      choose(strings.TrimSpace(req.PolicyRule), "default."+string(req.Stage)),
		PolicyDigest:    stagePolicy.Digest(),
		InputSHA256:     digestBytes(req.Prompt),
		ContextBytes:    int64(len(req.Prompt)),
		MaxContextBytes: stagePolicy.MaxContextBytes,
		GeneratedAt:     now().UTC(),
		CorrelationID:   strings.TrimSpace(req.CorrelationID),
	}
	if manifest.ContextBytes > stagePolicy.MaxContextBytes {
		return EvaluationResult{}, fmt.Errorf("%w: got %d want <= %d", ErrContextTooLarge, manifest.ContextBytes, stagePolicy.MaxContextBytes)
	}
	redactor := redaction.NewDefault()
	redactedSources := make([]Source, 0, len(req.Sources))
	for _, source := range req.Sources {
		cleanSource, err := normalizeSource(source)
		if err != nil {
			return EvaluationResult{}, err
		}
		if excluded, reason := sourceExcluded(stagePolicy, cleanSource); excluded {
			manifest.ExcludedRefs = append(manifest.ExcludedRefs, excludedRef(cleanSource, reason))
			continue
		}
		if cleanSource.Raw && stagePolicy.RawSourceMode != RawSourceAllowed {
			manifest.ExcludedRefs = append(manifest.ExcludedRefs, excludedRef(cleanSource, "raw_source_not_allowed"))
			continue
		}
		redacted := cleanSource
		if len(cleanSource.Content) > 0 {
			text, traces := redactor.RedactText(redaction.Surface("model-context"), cleanSource.Path, string(cleanSource.Content))
			if len(traces) > 0 && stagePolicy.SecretHandling == SecretHandlingBlock {
				return EvaluationResult{}, fmt.Errorf("%w: %s", ErrSecretBlocked, cleanSource.Path)
			}
			if len(traces) > 0 {
				redacted.Content = []byte(text)
				for _, trace := range traces {
					manifest.Redactions = append(manifest.Redactions, RedactionRecord{
						SourcePath:    cleanSource.Path,
						PatternID:     trace.PatternID,
						Count:         trace.Count,
						BytesRedacted: trace.BytesRedacted,
					})
				}
			}
		}
		manifest.ContextBytes += int64(len(redacted.Content))
		if manifest.ContextBytes > stagePolicy.MaxContextBytes {
			return EvaluationResult{}, fmt.Errorf("%w: got %d want <= %d", ErrContextTooLarge, manifest.ContextBytes, stagePolicy.MaxContextBytes)
		}
		manifest.SourceRefs = append(manifest.SourceRefs, SourceRef{
			Kind:      cleanSource.Kind,
			Path:      cleanSource.Path,
			URI:       cleanSource.URI,
			SHA256:    digestBytes(redacted.Content),
			SizeBytes: int64(len(redacted.Content)),
			Raw:       cleanSource.Raw,
		})
		redactedSources = append(redactedSources, redacted)
	}
	sortRedactions(manifest.Redactions)
	manifest.ManifestID = manifestID(manifest)
	return EvaluationResult{Manifest: manifest, RedactedSources: redactedSources}, nil
}

func requireHarnessAllowed(policy Policy, harness string) error {
	harness = strings.TrimSpace(harness)
	if harness == "" {
		return fmt.Errorf("%w: harness is required", ErrInvalidPolicy)
	}
	if len(policy.HarnessAllowlist) == 0 {
		return nil
	}
	for _, allowed := range policy.HarnessAllowlist {
		if harness == strings.TrimSpace(allowed) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrProviderDenied, harness)
}

func normalizeSource(source Source) (Source, error) {
	source.Kind = strings.TrimSpace(source.Kind)
	if source.Kind == "" {
		source.Kind = "artifact"
	}
	source.URI = strings.TrimSpace(source.URI)
	source.Path = strings.TrimSpace(filepath.ToSlash(filepath.Clean(source.Path)))
	if source.Path == "." {
		source.Path = ""
	}
	if strings.HasPrefix(source.Path, "../") || source.Path == ".." || filepath.IsAbs(source.Path) {
		return Source{}, fmt.Errorf("%w: %s", ErrUnsafeManifestRef, source.Path)
	}
	return source, nil
}

func sourceExcluded(policy Policy, source Source) (bool, string) {
	if source.Path == "" {
		return false, ""
	}
	for _, glob := range policy.ExcludeFileGlobs {
		if globMatches(glob, source.Path) {
			return true, "excluded_by_policy"
		}
	}
	if len(policy.IncludeFileGlobs) == 0 {
		return false, ""
	}
	for _, glob := range policy.IncludeFileGlobs {
		if globMatches(glob, source.Path) {
			return false, ""
		}
	}
	return true, "not_in_include_globs"
}

func globMatches(glob, path string) bool {
	glob = filepath.ToSlash(strings.TrimSpace(glob))
	path = filepath.ToSlash(strings.TrimSpace(path))
	if glob == "" || path == "" {
		return false
	}
	if ok, _ := filepath.Match(glob, path); ok {
		return true
	}
	if strings.HasPrefix(glob, "**/") {
		tail := strings.TrimPrefix(glob, "**/")
		if ok, _ := filepath.Match(tail, path); ok {
			return true
		}
		return strings.HasSuffix(path, "/"+tail) || strings.Contains(path, "/"+strings.TrimSuffix(tail, "/**")+"/")
	}
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	return false
}

func excludedRef(source Source, reason string) ExcludedRef {
	return ExcludedRef{
		Kind:   source.Kind,
		Path:   source.Path,
		URI:    source.URI,
		Reason: reason,
	}
}

func sortRedactions(records []RedactionRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].SourcePath != records[j].SourcePath {
			return records[i].SourcePath < records[j].SourcePath
		}
		return records[i].PatternID < records[j].PatternID
	})
}

func manifestID(manifest Manifest) string {
	copy := manifest
	copy.ManifestID = ""
	body, _ := json.Marshal(copy)
	sum := sha256.Sum256(body)
	return "ctx_" + hex.EncodeToString(sum[:8])
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func choose(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
