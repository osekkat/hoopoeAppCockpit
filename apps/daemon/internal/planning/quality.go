package planning

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// quality.go owns the plan-quality scoring surface — the Plan §7.1 quality
// tracker that scores a synthesized plan along the seven dimensions
// (intent / architecture / workflow / implementation / testing / risk /
// bead-readiness) and emits an advisory QualityReport.
//
// hp-k7as first cut: split out of planning.go (analytical [LOW]: the
// 1,178-line planning.go combined orchestration, artifact IO, parallel
// candidate execution, and quality scoring). Quality scoring is the
// most self-contained slice of that file — pure, deterministic markdown
// analysis that the orchestration layer calls into via EvaluatePlanQuality
// + qualityInput. Behavior unchanged: same package, same exported types
// (QualityDimension, QualityReport, QualityDimensionScore, QualityEvidence,
// QualityRequest), same exported function (EvaluatePlanQuality), same
// internal helpers (qualitySpec/qualityCheck/qualityDoc, qualitySpecs,
// qualityContext, headingContains, containsAny, isNumberedLine, qualityInput).
// Service.Run still drives Quality through same-package access.

type QualityDimension string

const (
	QualityIntentClarity    QualityDimension = "intent_clarity"
	QualityArchitecture     QualityDimension = "architecture_specificity"
	QualityWorkflowCoverage QualityDimension = "workflow_coverage"
	QualityImplementation   QualityDimension = "implementation_detail"
	QualityTesting          QualityDimension = "testing_specificity"
	QualityRisk             QualityDimension = "risk_coverage"
	QualityBeadReadiness    QualityDimension = "bead_readiness"
	qualityArtifactPath                      = "quality.json"
)

type QualityReport struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	PlanID          string                  `json:"planId"`
	PipelineVersion string                  `json:"pipelineVersion"`
	SourceStepID    string                  `json:"sourceStepId,omitempty"`
	GeneratedAt     time.Time               `json:"generatedAt"`
	OverallScore    int                     `json:"overallScore"`
	Advisory        bool                    `json:"advisory"`
	Guidance        string                  `json:"guidance"`
	Dimensions      []QualityDimensionScore `json:"dimensions"`
}

type QualityDimensionScore struct {
	Dimension QualityDimension  `json:"dimension"`
	Label     string            `json:"label"`
	Score     int               `json:"score"`
	Delta     *int              `json:"delta,omitempty"`
	Evidence  []QualityEvidence `json:"evidence"`
	Guidance  string            `json:"guidance"`
}

type QualityEvidence struct {
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	Matched  bool   `json:"matched"`
	Weight   int    `json:"weight"`
	Location string `json:"location,omitempty"`
}

type QualityRequest struct {
	PlanID       string
	SourceStepID string
	Markdown     string
	GeneratedAt  time.Time
	Previous     *QualityReport
}

func EvaluatePlanQuality(req QualityRequest) (QualityReport, error) {
	req.PlanID = sanitizeSegment(req.PlanID)
	req.SourceStepID = sanitizeSegment(req.SourceStepID)
	req.Markdown = strings.TrimSpace(req.Markdown)
	if req.PlanID == "" || req.Markdown == "" {
		return QualityReport{}, fmt.Errorf("%w: planId and markdown are required", ErrInvalidRequest)
	}
	generatedAt := req.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	ctx := qualityContext(req.Markdown)
	scores := make([]QualityDimensionScore, 0, len(qualitySpecs()))
	for _, spec := range qualitySpecs() {
		score := spec.score(ctx)
		if req.Previous != nil {
			if previous, ok := req.Previous.dimensionScore(score.Dimension); ok {
				delta := score.Score - previous.Score
				score.Delta = &delta
			}
		}
		scores = append(scores, score)
	}
	total := 0
	for _, score := range scores {
		total += score.Score
	}
	overall := 0
	if len(scores) > 0 {
		overall = total / len(scores)
	}
	return QualityReport{
		SchemaVersion:   SchemaVersion,
		PlanID:          req.PlanID,
		PipelineVersion: PipelineVersion,
		SourceStepID:    req.SourceStepID,
		GeneratedAt:     generatedAt.UTC(),
		OverallScore:    overall,
		Advisory:        true,
		Guidance:        "Plan quality scores are advisory decision aids; inspect the evidence before locking or converting to beads.",
		Dimensions:      scores,
	}, nil
}

func (r QualityReport) dimensionScore(dimension QualityDimension) (QualityDimensionScore, bool) {
	for _, score := range r.Dimensions {
		if score.Dimension == dimension {
			return score, true
		}
	}
	return QualityDimensionScore{}, false
}

type qualitySpec struct {
	dimension QualityDimension
	label     string
	guidance  string
	checks    []qualityCheck
}

type qualityCheck struct {
	kind     string
	detail   string
	weight   int
	location string
	matches  func(qualityDoc) bool
}

type qualityDoc struct {
	raw         string
	lower       string
	headings    []string
	bullets     int
	numbered    int
	codeBlocks  int
	words       int
	fileRefs    int
	commandRefs int
}

func (s qualitySpec) score(doc qualityDoc) QualityDimensionScore {
	evidence := make([]QualityEvidence, 0, len(s.checks))
	totalWeight := 0
	matchedWeight := 0
	for _, check := range s.checks {
		totalWeight += check.weight
		matched := check.matches(doc)
		if matched {
			matchedWeight += check.weight
		}
		evidence = append(evidence, QualityEvidence{
			Kind:     check.kind,
			Detail:   check.detail,
			Matched:  matched,
			Weight:   check.weight,
			Location: check.location,
		})
	}
	score := 0
	if totalWeight > 0 {
		score = matchedWeight * 100 / totalWeight
	}
	return QualityDimensionScore{
		Dimension: s.dimension,
		Label:     s.label,
		Score:     score,
		Evidence:  evidence,
		Guidance:  s.guidance,
	}
}

func qualitySpecs() []qualitySpec {
	return []qualitySpec{
		{
			dimension: QualityIntentClarity,
			label:     "Intent clarity",
			guidance:  "Clarify the user goal, success criteria, and expected outcome before locking.",
			checks: []qualityCheck{
				{kind: "length", detail: "plan has enough substance to evaluate intent", weight: 15, matches: func(d qualityDoc) bool { return d.words >= 80 }},
				{kind: "heading", detail: "goal or objective section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "goal", "objective", "intent", "problem") }},
				{kind: "keyword", detail: "success or acceptance language is present", weight: 30, matches: func(d qualityDoc) bool { return containsAny(d.lower, "success", "acceptance", "outcome", "done when") }},
				{kind: "structure", detail: "plan uses bullets or numbered steps", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 3 }},
			},
		},
		{
			dimension: QualityArchitecture,
			label:     "Architecture specificity",
			guidance:  "Name components, boundaries, APIs, storage, and data flow explicitly.",
			checks: []qualityCheck{
				{kind: "heading", detail: "architecture or design section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "architecture", "design", "system", "component") }},
				{kind: "keyword", detail: "component or boundary terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "component", "boundary", "module", "service", "daemon", "desktop")
				}},
				{kind: "keyword", detail: "data flow, API, schema, or storage terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "api", "schema", "database", "sqlite", "event", "data flow")
				}},
				{kind: "artifact", detail: "file/module references or code blocks anchor the architecture", weight: 20, matches: func(d qualityDoc) bool { return d.fileRefs > 0 || d.codeBlocks > 0 }},
			},
		},
		{
			dimension: QualityWorkflowCoverage,
			label:     "Workflow coverage",
			guidance:  "Cover happy paths, edge cases, failure paths, and user-visible state transitions.",
			checks: []qualityCheck{
				{kind: "heading", detail: "workflow or user journey section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "workflow", "journey", "flow", "scenario") }},
				{kind: "keyword", detail: "happy-path language is present", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "happy path", "primary flow", "user can", "when the user")
				}},
				{kind: "keyword", detail: "edge or failure cases are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "edge case", "failure", "fallback", "error", "offline", "retry")
				}},
				{kind: "structure", detail: "multiple ordered or bulleted workflow steps are present", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 5 }},
			},
		},
		{
			dimension: QualityImplementation,
			label:     "Implementation detail",
			guidance:  "Specify files, commands, typed interfaces, and execution order.",
			checks: []qualityCheck{
				{kind: "heading", detail: "implementation section is present", weight: 25, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "implementation", "engineering", "tasks", "plan") }},
				{kind: "artifact", detail: "file or package references are present", weight: 25, matches: func(d qualityDoc) bool { return d.fileRefs >= 2 }},
				{kind: "artifact", detail: "commands or fenced code blocks are present", weight: 25, matches: func(d qualityDoc) bool { return d.commandRefs > 0 || d.codeBlocks > 0 }},
				{kind: "keyword", detail: "concrete technology or interface terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "endpoint", "rpc", "interface", "struct", "component", "test harness")
				}},
			},
		},
		{
			dimension: QualityTesting,
			label:     "Testing specificity",
			guidance:  "Name unit, integration, fixture, and verification commands with expected evidence.",
			checks: []qualityCheck{
				{kind: "heading", detail: "testing or verification section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "test", "testing", "verification", "qa") }},
				{kind: "keyword", detail: "test levels are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "unit test", "integration", "e2e", "fixture", "golden")
				}},
				{kind: "artifact", detail: "verification commands are present", weight: 25, matches: func(d qualityDoc) bool {
					return d.commandRefs > 0 && containsAny(d.lower, "go test", "bun run", "rch exec", "playwright")
				}},
				{kind: "keyword", detail: "evidence or acceptance requirements are present", weight: 20, matches: func(d qualityDoc) bool { return containsAny(d.lower, "evidence", "assert", "coverage", "regression") }},
			},
		},
		{
			dimension: QualityRisk,
			label:     "Risk coverage",
			guidance:  "Surface risks, mitigations, rollback paths, and high-stakes failure modes.",
			checks: []qualityCheck{
				{kind: "heading", detail: "risk or mitigation section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "risk", "mitigation", "failure", "rollback") }},
				{kind: "keyword", detail: "security or privacy risks are named", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "security", "privacy", "secret", "redaction", "credential")
				}},
				{kind: "keyword", detail: "operational failure modes are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "rollback", "retry", "timeout", "rate limit", "offline", "crash")
				}},
				{kind: "keyword", detail: "mitigation language is present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "mitigate", "fallback", "guard", "recover", "verify before")
				}},
			},
		},
		{
			dimension: QualityBeadReadiness,
			label:     "Bead readiness",
			guidance:  "Break work into traceable tasks with dependencies, acceptance criteria, and verification.",
			checks: []qualityCheck{
				{kind: "heading", detail: "bead, task, or acceptance section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "bead", "task", "acceptance", "definition of done") }},
				{kind: "keyword", detail: "dependency or sequencing language is present", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "depends", "dependency", "blocked", "unblocks", "sequence")
				}},
				{kind: "structure", detail: "enough discrete bullets exist to convert into beads", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 7 }},
				{kind: "keyword", detail: "verification and acceptance criteria are explicit", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "acceptance criteria", "definition of done", "verify", "test evidence")
				}},
			},
		},
	}
}

func qualityInput(outputs map[string]string) string {
	keys := make([]string, 0, len(outputs))
	for key := range outputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("# ")
		b.WriteString(key)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(outputs[key]))
		b.WriteString("\n\n")
	}
	return b.String()
}

func qualityContext(markdown string) qualityDoc {
	doc := qualityDoc{
		raw:   markdown,
		lower: strings.ToLower(markdown),
	}
	inCode := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			doc.codeBlocks++
			inCode = !inCode
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			doc.headings = append(doc.headings, strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "#"))))
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			doc.bullets++
		}
		if isNumberedLine(trimmed) {
			doc.numbered++
		}
		if inCode || strings.HasPrefix(trimmed, "$ ") || strings.HasPrefix(trimmed, "go test ") || strings.HasPrefix(trimmed, "rch exec ") || strings.HasPrefix(trimmed, "bun run ") {
			doc.commandRefs++
		}
		for _, field := range strings.Fields(trimmed) {
			if strings.Contains(field, "/") || strings.HasSuffix(field, ".go") || strings.HasSuffix(field, ".ts") || strings.HasSuffix(field, ".tsx") || strings.HasSuffix(field, ".md") || strings.HasSuffix(field, ".json") {
				doc.fileRefs++
			}
		}
	}
	doc.codeBlocks /= 2
	doc.words = len(strings.Fields(markdown))
	return doc
}

func headingContains(doc qualityDoc, needles ...string) bool {
	for _, heading := range doc.headings {
		if containsAny(heading, needles...) {
			return true
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func isNumberedLine(line string) bool {
	if line == "" || line[0] < '0' || line[0] > '9' {
		return false
	}
	for i := 1; i < len(line); i++ {
		if line[i] >= '0' && line[i] <= '9' {
			continue
		}
		return (line[i] == '.' || line[i] == ')') && i+1 < len(line) && line[i+1] == ' '
	}
	return false
}
