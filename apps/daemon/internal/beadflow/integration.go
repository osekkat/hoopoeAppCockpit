package beadflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/br"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/bv"
)

func BeadsFromBRIssues(issues []br.Issue) []Bead {
	beads := make([]Bead, 0, len(issues))
	for _, issue := range issues {
		beads = append(beads, Bead{
			ID:           issue.ID,
			Title:        issue.Title,
			Description:  issue.Description,
			Status:       issue.Status,
			Priority:     issue.Priority,
			PlanSections: planSectionsFromBRIssue(issue),
			DependsOn:    dependsOnFromBRIssue(issue),
		})
	}
	return beads
}

func GraphHealthFromBV(plan *bv.PlanOutput, insights *bv.InsightsOutput) (GraphHealth, error) {
	health := GraphHealth{}
	if plan != nil {
		health.ReadyCount = plan.Plan.TotalActionable
		health.BlockedCount = plan.Plan.TotalBlocked
		for i, track := range plan.Plan.Tracks {
			beadIDs := make([]string, 0, len(track.Items))
			for _, item := range track.Items {
				if strings.TrimSpace(item.ID) != "" {
					beadIDs = append(beadIDs, item.ID)
				}
			}
			if len(beadIDs) == 0 {
				continue
			}
			health.ParallelTracks = append(health.ParallelTracks, Track{
				ID:      fmt.Sprintf("track-%d", i+1),
				BeadIDs: uniquePreserveOrder(beadIDs),
			})
		}
	}
	if insights != nil {
		cycles, err := decodeBVCycles(insights.Cycles)
		if err != nil {
			return health, err
		}
		health.Cycles = cycles
	}
	return health, nil
}

func planSectionsFromBRIssue(issue br.Issue) []string {
	sections := []string{}
	for _, label := range issue.Labels {
		sections = append(sections, trimKnownPrefix(label, "plan-section:"))
		sections = append(sections, trimKnownPrefix(label, "plan:"))
		sections = append(sections, trimKnownPrefix(label, "section:"))
	}
	if ref := strings.TrimSpace(issue.ExternalRef); ref != "" {
		if section := sectionFromExternalRef(ref); section != "" {
			sections = append(sections, section)
		}
	}
	for _, line := range strings.Split(issue.Description, "\n") {
		if section := sectionFromDescriptionLine(line); section != "" {
			sections = append(sections, section)
		}
	}
	return uniqueSorted(sections)
}

func dependsOnFromBRIssue(issue br.Issue) []string {
	out := []string{}
	for _, dep := range issue.Dependencies {
		if dep.DependsOnID != "" && dep.DependsOnID != issue.ID {
			out = append(out, dep.DependsOnID)
		}
	}
	return uniqueSorted(out)
}

func trimKnownPrefix(value, prefix string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	return ""
}

func sectionFromExternalRef(ref string) string {
	for _, marker := range []string{"#plan-section=", "#section=", "plan-section:", "plan:"} {
		if i := strings.Index(ref, marker); i >= 0 {
			return strings.TrimSpace(ref[i+len(marker):])
		}
	}
	return ""
}

func sectionFromDescriptionLine(line string) string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	if key != "plan section" && key != "plan sections" {
		return ""
	}
	return strings.TrimSpace(strings.Split(parts[1], ",")[0])
}

func decodeBVCycles(raw json.RawMessage) ([][]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var direct [][]string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return normalizeCycles(direct), nil
	}
	var objectCycles []struct {
		Nodes []string `json:"nodes"`
		Cycle []string `json:"cycle"`
		Path  []string `json:"path"`
	}
	if err := json.Unmarshal(raw, &objectCycles); err != nil {
		return nil, fmt.Errorf("beadflow: decode bv cycles: %w", err)
	}
	out := make([][]string, 0, len(objectCycles))
	for _, cycle := range objectCycles {
		switch {
		case len(cycle.Nodes) > 0:
			out = append(out, uniquePreserveOrder(cycle.Nodes))
		case len(cycle.Cycle) > 0:
			out = append(out, uniquePreserveOrder(cycle.Cycle))
		case len(cycle.Path) > 0:
			out = append(out, uniquePreserveOrder(cycle.Path))
		}
	}
	return normalizeCycles(out), nil
}

func normalizeCycles(cycles [][]string) [][]string {
	out := make([][]string, 0, len(cycles))
	for _, cycle := range cycles {
		clean := uniquePreserveOrder(cycle)
		if len(clean) > 0 {
			out = append(out, clean)
		}
	}
	return out
}

func uniquePreserveOrder(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
