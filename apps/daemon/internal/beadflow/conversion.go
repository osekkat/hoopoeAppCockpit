package beadflow

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/br"
)

const ConversionSchemaVersion = 1

type ConversionStatus string

const (
	ConversionReady   ConversionStatus = "ready"
	ConversionBlocked ConversionStatus = "blocked"
)

type BeadOperationKind string

const (
	BeadOperationCreate BeadOperationKind = "create"
	BeadOperationUpdate BeadOperationKind = "update"
)

type ConversionInput struct {
	PlanID            string
	PlanLocked        bool
	AllowUnlockedPlan bool
	Sections          []PlanSection
	ExistingBeads     []Bead
	DraftBeads        []Bead
	Graph             GraphHealth
	PreviousQuality   *QualityReport
	Now               func() time.Time
}

type ConversionPlan struct {
	SchemaVersion int              `json:"schemaVersion"`
	PlanID        string           `json:"planId"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Status        ConversionStatus `json:"status"`
	Operations    []BeadOperation  `json:"operations"`
	Traceability  TraceabilityMap  `json:"traceability"`
	Quality       QualityReport    `json:"quality"`
	Polish        PolishPlan       `json:"polish"`
	Findings      []string         `json:"findings,omitempty"`
}

type BeadOperation struct {
	Kind       BeadOperationKind `json:"kind"`
	DraftID    string            `json:"draftId"`
	ExistingID string            `json:"existingId,omitempty"`
	Bead       Bead              `json:"bead"`
	Reasons    []string          `json:"reasons,omitempty"`
}

type BRMutation struct {
	Kind         BeadOperationKind `json:"kind"`
	DraftID      string            `json:"draftId"`
	ExistingID   string            `json:"existingId,omitempty"`
	Create       *br.CreateRequest `json:"create,omitempty"`
	Update       *br.UpdateRequest `json:"update,omitempty"`
	Dependencies []br.DepRequest   `json:"dependencies,omitempty"`
}

func BuildConversionPlan(input ConversionInput) ConversionPlan {
	now := input.Now
	if now == nil {
		now = time.Now
	}
	findings := []string{}
	if !input.PlanLocked && !input.AllowUnlockedPlan {
		findings = append(findings, "plan is not locked; conversion requires an override")
	}
	if len(input.DraftBeads) == 0 {
		findings = append(findings, "no draft beads supplied")
	}

	operations, opFindings := buildOperations(input.Sections, input.ExistingBeads, input.DraftBeads)
	findings = append(findings, opFindings...)
	converted := mergedBeads(input.ExistingBeads, operations)
	trace := BuildTraceability(input.Sections, converted, TraceabilityOptions{
		PlanID: input.PlanID,
		Now:    now,
	})
	quality := ComputeQuality(QualityInput{
		Traceability: trace,
		Graph:        input.Graph,
		Previous:     input.PreviousQuality,
		Now:          now,
	})
	polish := BuildPolishPlan(PolishInput{
		PlanID:       input.PlanID,
		Traceability: trace,
		Quality:      quality,
		Graph:        input.Graph,
		Now:          now,
	})
	status := ConversionReady
	if len(findings) > 0 {
		status = ConversionBlocked
	}
	return ConversionPlan{
		SchemaVersion: ConversionSchemaVersion,
		PlanID:        input.PlanID,
		GeneratedAt:   now().UTC(),
		Status:        status,
		Operations:    operations,
		Traceability:  trace,
		Quality:       quality,
		Polish:        polish,
		Findings:      uniqueSorted(findings),
	}
}

func BRMutationsFromOperations(ops []BeadOperation, common br.CommonOptions) ([]BRMutation, error) {
	mutations := make([]BRMutation, 0, len(ops))
	for _, op := range ops {
		switch op.Kind {
		case BeadOperationCreate:
			req := br.CreateRequest{
				Common:      common,
				Title:       op.Bead.Title,
				IssueType:   "task",
				Priority:    strconv.Itoa(op.Bead.Priority),
				Description: renderBeadDescription(op.Bead),
				Labels:      planSectionLabels(op.Bead.PlanSections),
				Deps:        strings.Join(op.Bead.DependsOn, ","),
				EstimateMin: op.Bead.EstimatedMinutes,
			}
			mutations = append(mutations, BRMutation{Kind: op.Kind, DraftID: op.DraftID, Create: &req})
		case BeadOperationUpdate:
			targetID := op.ExistingID
			if targetID == "" {
				targetID = op.Bead.ID
			}
			if targetID == "" {
				return nil, fmt.Errorf("beadflow: update operation %q has no target bead id", op.DraftID)
			}
			req := br.UpdateRequest{
				Common:             common,
				IDs:                []string{targetID},
				Title:              op.Bead.Title,
				Description:        renderBeadDescription(op.Bead),
				AcceptanceCriteria: strings.Join(op.Bead.AcceptanceCriteria, "\n"),
				Priority:           strconv.Itoa(op.Bead.Priority),
				Estimate:           estimateString(op.Bead.EstimatedMinutes),
				AddLabels:          planSectionLabels(op.Bead.PlanSections),
			}
			mutation := BRMutation{Kind: op.Kind, DraftID: op.DraftID, ExistingID: targetID, Update: &req}
			for _, dep := range op.Bead.DependsOn {
				mutation.Dependencies = append(mutation.Dependencies, br.DepRequest{
					Common:    common,
					IssueID:   targetID,
					DependsOn: dep,
					DepType:   "blocks",
				})
			}
			mutations = append(mutations, mutation)
		default:
			return nil, fmt.Errorf("beadflow: unsupported bead operation kind %q", op.Kind)
		}
	}
	return mutations, nil
}

func buildOperations(sections []PlanSection, existing []Bead, drafts []Bead) ([]BeadOperation, []string) {
	sections = normalizedSections(sections)
	validSection := map[string]bool{}
	for _, section := range sections {
		if section.ID != "" {
			validSection[section.ID] = true
		}
	}
	existingByID := map[string]Bead{}
	existingByTitle := map[string]Bead{}
	for _, bead := range existing {
		bead.ID = strings.TrimSpace(bead.ID)
		if bead.ID != "" {
			existingByID[bead.ID] = bead
		}
		if key := normalizedTitle(bead.Title); key != "" {
			existingByTitle[key] = bead
		}
	}

	findings := []string{}
	ops := make([]BeadOperation, 0, len(drafts))
	usedDraftIDs := map[string]int{}
	for _, draft := range drafts {
		draft = normalizeBead(draft)
		if draft.Title == "" {
			findings = append(findings, "draft bead missing title")
			continue
		}
		if len(draft.PlanSections) == 0 {
			findings = append(findings, fmt.Sprintf("draft bead %q is not linked to a plan section", draft.Title))
		}
		for _, sectionID := range draft.PlanSections {
			if !validSection[sectionID] {
				findings = append(findings, fmt.Sprintf("draft bead %q references unknown plan section %q", draft.Title, sectionID))
			}
		}

		draftID := draft.ID
		kind := BeadOperationCreate
		existingID := ""
		reasons := []string{"no matching bead exists"}
		if draftID != "" {
			if current, ok := existingByID[draftID]; ok {
				kind = BeadOperationUpdate
				existingID = current.ID
				reasons = []string{"draft id matched existing bead"}
			}
		}
		if kind == BeadOperationCreate {
			if current, ok := existingByTitle[normalizedTitle(draft.Title)]; ok {
				kind = BeadOperationUpdate
				existingID = current.ID
				draft.ID = current.ID
				reasons = []string{"normalized title matched existing bead"}
			}
		}
		if draftID == "" {
			draftID = uniqueDraftID(slug(draft.Title), usedDraftIDs)
		} else {
			draftID = uniqueDraftID(draftID, usedDraftIDs)
		}
		if kind == BeadOperationCreate {
			draft.ID = draftID
		}
		ops = append(ops, BeadOperation{
			Kind:       kind,
			DraftID:    draftID,
			ExistingID: existingID,
			Bead:       draft,
			Reasons:    reasons,
		})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].DraftID < ops[j].DraftID })
	return ops, findings
}

func mergedBeads(existing []Bead, ops []BeadOperation) []Bead {
	byID := map[string]Bead{}
	for _, bead := range existing {
		bead = normalizeBead(bead)
		if bead.ID != "" {
			byID[bead.ID] = bead
		}
	}
	for _, op := range ops {
		bead := normalizeBead(op.Bead)
		if bead.ID == "" {
			bead.ID = op.DraftID
		}
		byID[bead.ID] = bead
	}
	out := make([]Bead, 0, len(byID))
	for _, bead := range byID {
		out = append(out, bead)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func normalizeBead(bead Bead) Bead {
	bead.ID = strings.TrimSpace(bead.ID)
	bead.Title = strings.TrimSpace(bead.Title)
	bead.PlanSections = uniqueSorted(bead.PlanSections)
	bead.TestObligations = uniqueSorted(bead.TestObligations)
	bead.AcceptanceCriteria = uniqueSorted(bead.AcceptanceCriteria)
	bead.DependsOn = uniqueSorted(bead.DependsOn)
	bead.ImplementationNotes = uniqueSorted(bead.ImplementationNotes)
	return bead
}

func renderBeadDescription(bead Bead) string {
	sections := []string{}
	if body := strings.TrimSpace(bead.Description); body != "" {
		sections = append(sections, body)
	}
	appendList := func(title string, values []string) {
		values = uniqueSorted(values)
		if len(values) == 0 {
			return
		}
		lines := []string{title}
		for _, value := range values {
			lines = append(lines, "- "+value)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	appendList("Plan sections", bead.PlanSections)
	appendList("Acceptance criteria", bead.AcceptanceCriteria)
	appendList("Test obligations", bead.TestObligations)
	appendList("Implementation notes", bead.ImplementationNotes)
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func planSectionLabels(sectionIDs []string) []string {
	out := make([]string, 0, len(sectionIDs))
	for _, sectionID := range uniqueSorted(sectionIDs) {
		out = append(out, "plan-section:"+sectionID)
	}
	return out
}

func estimateString(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	return strconv.Itoa(minutes)
}

func normalizedTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func uniqueDraftID(base string, used map[string]int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "draft"
	}
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
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
	return strings.Trim(b.String(), "-")
}
