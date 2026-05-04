// Package ubs wraps Ultimate Bug Scanner as Hoopoe's deterministic first-pass
// review adapter. It never invokes a shell and emits review findings already
// stamped with source:"ubs" for the Phase 12 finding ledger.
package ubs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "ubs"

	CapabilityScan = "ubs.scan"

	SourceUBS = "ubs"

	RoundFirstPass      ScanRound = "round_0"
	RoundHotspotTarget  ScanRound = "round_5"
	defaultMaxJSONBytes           = 8 << 20
)

var (
	ErrInvalidRequest  = errors.New("ubs: invalid request")
	ErrMissingBinary   = errors.New("ubs: binary not found")
	ErrOutputTooLarge  = errors.New("ubs: command output exceeded limit")
	ErrCommandContract = errors.New("ubs: command contract violation")
)

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		ExitCode: 0,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if isExecNotFoundErr(err) {
		result.ExitCode = -1
		return result, ErrMissingBinary
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner       Runner
	Now          func() time.Time
	MaxJSONBytes int
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{
		Runner:       runner,
		Now:          time.Now,
		MaxJSONBytes: defaultMaxJSONBytes,
	}
}

type ScanRound string

type ScanRequest struct {
	ProjectDir string
	Round      ScanRound
	Hotspots   []string
	Languages  []string
	Categories []string
	Jobs       int
}

type ScanResult struct {
	ProjectDir string          `json:"projectDir"`
	Round      ScanRound       `json:"round"`
	Findings   []Finding       `json:"findings"`
	Summary    ScanSummary     `json:"summary"`
	CheckedAt  time.Time       `json:"checkedAt"`
	RawSARIF   json.RawMessage `json:"-"`
}

type ScanSummary struct {
	Files    int `json:"files,omitempty"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type LineRange struct {
	StartLine   int `json:"startLine"`
	EndLine     int `json:"endLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

type Finding struct {
	FindingID   string    `json:"findingId"`
	Source      string    `json:"source"`
	Sources     []string  `json:"sources"`
	FilePath    string    `json:"filePath"`
	LineRange   LineRange `json:"lineRange"`
	Severity    Severity  `json:"severity"`
	Category    string    `json:"category"`
	Message     string    `json:"message"`
	RuleID      string    `json:"ruleId"`
	CodeContext string    `json:"codeContext,omitempty"`
	Time        time.Time `json:"time"`
}

func VersionArgv() []string {
	return []string{ToolName, "--version"}
}

func FirstPassArgv(req ScanRequest) ([]string, error) {
	req.Round = RoundFirstPass
	req.Hotspots = nil
	return ScanArgv(req)
}

func HotspotArgv(req ScanRequest) ([]string, error) {
	req.Round = RoundHotspotTarget
	if len(req.Hotspots) == 0 {
		return nil, fmt.Errorf("%w: hotspot scan requires at least one path", ErrInvalidRequest)
	}
	return ScanArgv(req)
}

func ScanArgv(req ScanRequest) ([]string, error) {
	projectDir, err := normalizeProjectDir(req.ProjectDir)
	if err != nil {
		return nil, err
	}
	round, err := normalizeRound(req.Round, len(req.Hotspots) > 0)
	if err != nil {
		return nil, err
	}
	if round == RoundFirstPass {
		req.Hotspots = nil
	}
	if round == RoundHotspotTarget && len(req.Hotspots) == 0 {
		return nil, fmt.Errorf("%w: hotspot scan requires at least one path", ErrInvalidRequest)
	}
	argv := []string{
		ToolName,
		"--format=sarif",
		"--ci",
		"--non-interactive",
		"--no-auto-update",
	}
	if req.Jobs > 0 {
		argv = append(argv, "--jobs="+strconv.Itoa(req.Jobs))
	}
	if values, err := normalizeCSVValues(req.Languages, "language"); err != nil {
		return nil, err
	} else if len(values) > 0 {
		argv = append(argv, "--only="+strings.Join(values, ","))
	}
	if values, err := normalizeCSVValues(req.Categories, "category"); err != nil {
		return nil, err
	} else if len(values) > 0 {
		argv = append(argv, "--category="+strings.Join(values, ","))
	}
	hotspots, err := normalizeScanPaths(projectDir, req.Hotspots)
	if err != nil {
		return nil, err
	}
	if len(hotspots) > 0 {
		argv = append(argv, "--files="+strings.Join(hotspots, ","))
	}
	argv = append(argv, projectDir)
	return argv, nil
}

func (a *Adapter) FirstPass(ctx context.Context, req ScanRequest) (ScanResult, error) {
	req.Round = RoundFirstPass
	req.Hotspots = nil
	return a.Scan(ctx, req)
}

func (a *Adapter) Hotspots(ctx context.Context, req ScanRequest) (ScanResult, error) {
	req.Round = RoundHotspotTarget
	if len(req.Hotspots) == 0 {
		return ScanResult{}, fmt.Errorf("%w: hotspot scan requires at least one path", ErrInvalidRequest)
	}
	return a.Scan(ctx, req)
}

func (a *Adapter) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	argv, err := ScanArgv(req)
	if err != nil {
		return ScanResult{}, err
	}
	round, _ := normalizeRound(req.Round, len(req.Hotspots) > 0)
	result, err := a.run(ctx, argv)
	if err != nil {
		return ScanResult{}, err
	}
	raw, err := extractJSONDocument(result.Stdout, a.maxJSONBytes())
	if err != nil {
		return ScanResult{}, err
	}
	checkedAt := a.now().UTC()
	findings, summary, err := ParseSARIF(raw, req.ProjectDir, checkedAt)
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{
		ProjectDir: filepath.Clean(strings.TrimSpace(req.ProjectDir)),
		Round:      round,
		Findings:   findings,
		Summary:    summary,
		CheckedAt:  checkedAt,
		RawSARIF:   raw,
	}, nil
}

func normalizeRound(round ScanRound, hasHotspots bool) (ScanRound, error) {
	switch round {
	case "":
		if hasHotspots {
			return RoundHotspotTarget, nil
		}
		return RoundFirstPass, nil
	case RoundFirstPass:
		return RoundFirstPass, nil
	case RoundHotspotTarget:
		return RoundHotspotTarget, nil
	default:
		return "", fmt.Errorf("%w: unknown scan round %q", ErrInvalidRequest, round)
	}
}

func (a *Adapter) Version(ctx context.Context) (string, error) {
	result, err := a.run(ctx, VersionArgv())
	if err != nil {
		return "", err
	}
	version := ParseVersion(result.Stdout)
	if version == "" {
		return "", fmt.Errorf("%w: missing version", ErrCommandContract)
	}
	return version, nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolUBS,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities: map[string]capabilities.Capability{
			CapabilityScan: {Status: capabilities.StatusMissing},
		},
	}
	version, err := a.Version(ctx)
	if err != nil {
		report.Capabilities[CapabilityScan] = capabilities.Capability{
			Status: statusForError(err),
			Notes:  err.Error(),
		}
		return report, nil
	}
	report.Version = version
	report.Capabilities[CapabilityScan] = capabilities.Capability{
		Status:    capabilities.StatusOK,
		Transport: "stdio",
	}
	return report, nil
}

func ParseVersion(out []byte) string {
	text := strings.TrimSpace(string(out))
	fields := strings.Fields(text)
	for i, field := range fields {
		if field == "v" && i+1 < len(fields) {
			return strings.TrimPrefix(fields[i+1], "v")
		}
		if strings.HasPrefix(field, "v") && len(field) > 1 && field[1] >= '0' && field[1] <= '9' {
			return strings.TrimPrefix(field, "v")
		}
	}
	return ""
}

func ParseSARIF(raw []byte, projectDir string, checkedAt time.Time) ([]Finding, ScanSummary, error) {
	var doc sarifDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, ScanSummary{}, fmt.Errorf("ubs: decode sarif: %w", err)
	}
	if doc.Version == "" || len(doc.Runs) == 0 {
		return nil, ScanSummary{}, fmt.Errorf("%w: sarif missing version or runs", ErrCommandContract)
	}
	findings := []Finding{}
	summary := ScanSummary{}
	seenFiles := map[string]struct{}{}
	for _, run := range doc.Runs {
		driver := strings.TrimSpace(run.Tool.Driver.Name)
		for _, result := range run.Results {
			finding, ok, err := findingFromSARIFResult(result, projectDir, checkedAt, driver)
			if err != nil {
				return nil, ScanSummary{}, err
			}
			if !ok {
				continue
			}
			findings = append(findings, finding)
			if finding.FilePath != "" {
				seenFiles[finding.FilePath] = struct{}{}
			}
			switch finding.Severity {
			case SeverityCritical:
				summary.Critical++
			case SeverityWarning:
				summary.Warning++
			default:
				summary.Info++
			}
		}
	}
	summary.Files = len(seenFiles)
	sort.SliceStable(findings, func(i, j int) bool {
		return findingSortKey(findings[i]) < findingSortKey(findings[j])
	})
	return findings, summary, nil
}

func MergeFindings(existing []Finding, incoming []Finding) []Finding {
	merged := make([]Finding, 0, len(existing)+len(incoming))
	index := map[string]int{}
	for _, finding := range existing {
		finding.Sources = normalizeSources(finding.Source, finding.Sources)
		index[dedupeKey(finding)] = len(merged)
		merged = append(merged, finding)
	}
	for _, finding := range incoming {
		finding.Sources = normalizeSources(finding.Source, finding.Sources)
		key := dedupeKey(finding)
		if idx, ok := index[key]; ok {
			merged[idx].Sources = mergeSources(merged[idx].Sources, finding.Sources)
			if merged[idx].Source == "" && finding.Source != "" {
				merged[idx].Source = finding.Source
			}
			continue
		}
		index[key] = len(merged)
		merged = append(merged, finding)
	}
	return merged
}

func (a *Adapter) run(ctx context.Context, argv []string) (CommandResult, error) {
	if a == nil {
		return CommandResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return CommandResult{}, fmt.Errorf("ubs: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return CommandResult{}, commandError{argv: argv, result: result}
	}
	return result, nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	detail := strings.TrimSpace(string(e.result.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(e.result.Stdout))
	}
	return fmt.Sprintf("ubs: command %v exited %d: %s", e.argv, e.result.ExitCode, detail)
}

type sarifDocument struct {
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name string `json:"name"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int          `json:"startLine"`
	EndLine     int          `json:"endLine"`
	StartColumn int          `json:"startColumn"`
	EndColumn   int          `json:"endColumn"`
	Snippet     sarifSnippet `json:"snippet"`
}

type sarifSnippet struct {
	Text string `json:"text"`
}

func findingFromSARIFResult(result sarifResult, projectDir string, checkedAt time.Time, driver string) (Finding, bool, error) {
	if len(result.Locations) == 0 {
		return Finding{}, false, nil
	}
	location := result.Locations[0].PhysicalLocation
	filePath, err := normalizeReportedPath(projectDir, location.ArtifactLocation.URI)
	if err != nil {
		return Finding{}, false, err
	}
	lineRange := LineRange{
		StartLine:   location.Region.StartLine,
		EndLine:     location.Region.EndLine,
		StartColumn: location.Region.StartColumn,
		EndColumn:   location.Region.EndColumn,
	}
	if lineRange.StartLine == 0 {
		lineRange.StartLine = 1
	}
	if lineRange.EndLine == 0 {
		lineRange.EndLine = lineRange.StartLine
	}
	message := strings.TrimSpace(result.Message.Text)
	ruleID := strings.TrimSpace(result.RuleID)
	severity := severityFromSARIFLevel(result.Level)
	finding := Finding{
		Source:      SourceUBS,
		Sources:     []string{SourceUBS},
		FilePath:    filePath,
		LineRange:   lineRange,
		Severity:    severity,
		Category:    categoryFromRuleID(ruleID, driver),
		Message:     message,
		RuleID:      ruleID,
		CodeContext: strings.TrimSpace(location.Region.Snippet.Text),
		Time:        checkedAt,
	}
	finding.FindingID = findingID(finding)
	return finding, true, nil
}

func severityFromSARIFLevel(level string) Severity {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return SeverityCritical
	case "warning":
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

func categoryFromRuleID(ruleID string, fallback string) string {
	ruleID = strings.TrimSpace(ruleID)
	if ruleID != "" {
		if idx := strings.Index(ruleID, "."); idx > 0 {
			return ruleID[:idx]
		}
		return ruleID
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "ubs"
	}
	return fallback
}

func findingID(finding Finding) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		SourceUBS,
		finding.FilePath,
		strconv.Itoa(finding.LineRange.StartLine),
		strconv.Itoa(finding.LineRange.EndLine),
		finding.RuleID,
		finding.Message,
	}, "\x00")))
	return "ubs_" + hex.EncodeToString(hash[:])[:20]
}

func dedupeKey(finding Finding) string {
	return strings.Join([]string{
		filepath.ToSlash(filepath.Clean(finding.FilePath)),
		strconv.Itoa(finding.LineRange.StartLine),
		finding.RuleID,
	}, "\x00")
}

func findingSortKey(finding Finding) string {
	return strings.Join([]string{
		finding.FilePath,
		fmt.Sprintf("%010d", finding.LineRange.StartLine),
		finding.RuleID,
		finding.Message,
	}, "\x00")
}

func normalizeProjectDir(raw string) (string, error) {
	projectDir := strings.TrimSpace(raw)
	if projectDir == "" {
		return "", fmt.Errorf("%w: projectDir is required", ErrInvalidRequest)
	}
	if strings.ContainsRune(projectDir, '\x00') {
		return "", fmt.Errorf("%w: projectDir contains NUL", ErrInvalidRequest)
	}
	return filepath.Clean(projectDir), nil
}

func normalizeCSVValues(raw []string, label string) ([]string, error) {
	seen := map[string]struct{}{}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, ",=\x00") || strings.HasPrefix(value, "-") || strings.Contains(value, " ") {
			return nil, fmt.Errorf("%w: unsafe %s %q", ErrInvalidRequest, label, item)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}

func normalizeScanPaths(projectDir string, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	projectAbs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve projectDir: %v", ErrInvalidRequest, err)
	}
	projectAbs = filepath.Clean(projectAbs)
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(raw))
	for _, item := range raw {
		path := strings.TrimSpace(item)
		if path == "" {
			continue
		}
		if strings.ContainsAny(path, ",\x00") || strings.HasPrefix(path, "-") {
			return nil, fmt.Errorf("%w: unsafe scan path %q", ErrInvalidRequest, item)
		}
		clean := filepath.Clean(path)
		if filepath.IsAbs(clean) {
			rel, err := filepath.Rel(projectAbs, clean)
			if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
				return nil, fmt.Errorf("%w: scan path %q is outside projectDir", ErrInvalidRequest, item)
			}
			clean = rel
		}
		if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			return nil, fmt.Errorf("%w: scan path %q is outside projectDir", ErrInvalidRequest, item)
		}
		clean = filepath.ToSlash(clean)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths, nil
}

func normalizeReportedPath(projectDir string, rawURI string) (string, error) {
	uri := strings.TrimSpace(rawURI)
	if uri == "" {
		return "", fmt.Errorf("%w: sarif result missing artifact URI", ErrCommandContract)
	}
	if parsed, err := url.Parse(uri); err == nil && parsed.Scheme == "file" {
		uri = parsed.Path
	}
	path := filepath.Clean(uri)
	projectDir = strings.TrimSpace(projectDir)
	if projectDir != "" && filepath.IsAbs(path) {
		projectAbs, err := filepath.Abs(projectDir)
		if err == nil {
			if rel, relErr := filepath.Rel(filepath.Clean(projectAbs), path); relErr == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				path = rel
			}
		}
	}
	return filepath.ToSlash(path), nil
}

func extractJSONDocument(output []byte, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = defaultMaxJSONBytes
	}
	if len(output) > limit {
		return nil, ErrOutputTooLarge
	}
	trimmed := bytes.TrimSpace(output)
	start := bytes.IndexByte(trimmed, '{')
	end := bytes.LastIndexByte(trimmed, '}')
	if start < 0 || end < start {
		return nil, fmt.Errorf("%w: missing JSON document", ErrCommandContract)
	}
	return append([]byte(nil), trimmed[start:end+1]...), nil
}

func normalizeSources(primary string, sources []string) []string {
	if primary != "" {
		sources = append(sources, primary)
	}
	return mergeSources(nil, sources)
}

func mergeSources(left []string, right []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(left)+len(right))
	for _, source := range append(left, right...) {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		if commandErr.result.ExitCode == 124 {
			return capabilities.StatusDegraded
		}
		if commandErr.result.ExitCode == 127 {
			return capabilities.StatusMissing
		}
		return capabilities.StatusDegraded
	}
	if errors.Is(err, ErrMissingBinary) || errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") {
		return capabilities.StatusMissing
	}
	if errors.Is(err, ErrCommandContract) || strings.Contains(err.Error(), "decode sarif") {
		return capabilities.StatusDegraded
	}
	return capabilities.StatusDegraded
}

func isExecNotFoundErr(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "executable file not found") || strings.Contains(text, "no such file or directory")
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Adapter) maxJSONBytes() int {
	if a != nil && a.MaxJSONBytes > 0 {
		return a.MaxJSONBytes
	}
	return defaultMaxJSONBytes
}
