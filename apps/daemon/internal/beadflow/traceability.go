package beadflow

import (
	"sort"
	"strings"
	"time"
)

const (
	defaultOversizedMinutes = 480
	defaultDescriptionWords = 240
)

type TraceabilityOptions struct {
	PlanID                    string
	Now                       func() time.Time
	OversizedMinutesThreshold int
	OversizedWordsThreshold   int
}

func BuildTraceability(sections []PlanSection, beads []Bead, opts TraceabilityOptions) TraceabilityMap {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	oversizedMinutes := opts.OversizedMinutesThreshold
	if oversizedMinutes <= 0 {
		oversizedMinutes = defaultOversizedMinutes
	}
	oversizedWords := opts.OversizedWordsThreshold
	if oversizedWords <= 0 {
		oversizedWords = defaultDescriptionWords
	}

	sections = normalizedSections(sections)
	sectionByID := map[string]PlanSection{}
	sectionBeads := map[string][]string{}
	for _, section := range sections {
		if section.ID == "" {
			continue
		}
		sectionByID[section.ID] = section
	}

	beadTraces := make([]BeadTrace, 0, len(beads))
	orphanBeads := []BeadTrace{}
	oversizedBeads := []BeadTrace{}
	missingTestBeads := []BeadTrace{}
	for _, bead := range beads {
		trace := beadTrace(bead)
		validSections := make([]string, 0, len(trace.PlanSectionIDs))
		for _, sectionID := range trace.PlanSectionIDs {
			if _, ok := sectionByID[sectionID]; !ok {
				continue
			}
			validSections = append(validSections, sectionID)
			sectionBeads[sectionID] = append(sectionBeads[sectionID], trace.BeadID)
		}
		trace.PlanSectionIDs = uniqueSorted(validSections)
		if len(trace.PlanSectionIDs) == 0 {
			trace.Status = CoverageOrphan
			orphanBeads = append(orphanBeads, trace)
		} else {
			trace.Status = CoverageCovered
		}
		if trace.EstimatedMinutes > oversizedMinutes || trace.DescriptionWords > oversizedWords {
			oversizedBeads = append(oversizedBeads, trace)
		}
		if len(trace.TestObligations) == 0 && len(trace.AcceptanceCriteria) == 0 {
			missingTestBeads = append(missingTestBeads, trace)
		}
		beadTraces = append(beadTraces, trace)
	}
	sort.Slice(beadTraces, func(i, j int) bool { return beadTraces[i].BeadID < beadTraces[j].BeadID })

	sectionTraces := make([]SectionTrace, 0, len(sectionByID))
	unmapped := []SectionTrace{}
	for _, section := range sections {
		if section.ID == "" {
			continue
		}
		beadIDs := uniqueSorted(sectionBeads[section.ID])
		status := CoverageCovered
		if len(beadIDs) == 0 {
			status = CoverageMissing
		}
		missingTests := missingSectionTests(section, beadsForSection(beads, section.ID))
		if status == CoverageCovered && len(missingTests) > 0 {
			status = CoveragePartial
		}
		trace := SectionTrace{
			SectionID:       section.ID,
			Title:           section.Title,
			Status:          status,
			BeadIDs:         beadIDs,
			RequiredTests:   uniqueSorted(section.RequiredTests),
			MissingTests:    missingTests,
			AcceptanceItems: uniqueSorted(section.AcceptanceItems),
		}
		sectionTraces = append(sectionTraces, trace)
		if status == CoverageMissing {
			unmapped = append(unmapped, trace)
		}
	}

	return TraceabilityMap{
		SchemaVersion:     TraceabilitySchemaVersion,
		PlanID:            opts.PlanID,
		GeneratedAt:       now().UTC(),
		Sections:          sectionTraces,
		Beads:             beadTraces,
		UnmappedSections:  unmapped,
		OrphanBeads:       orphanBeads,
		OversizedBeads:    oversizedBeads,
		DuplicateGroups:   duplicateGroups(beadTraces),
		MissingTestBeads:  missingTestBeads,
		PlanCoverageScore: coverageScore(sectionTraces),
	}
}

func beadTrace(bead Bead) BeadTrace {
	return BeadTrace{
		BeadID:              strings.TrimSpace(bead.ID),
		Title:               strings.TrimSpace(bead.Title),
		PlanSectionIDs:      uniqueSorted(bead.PlanSections),
		TestObligations:     uniqueSorted(bead.TestObligations),
		AcceptanceCriteria:  uniqueSorted(bead.AcceptanceCriteria),
		DependsOn:           uniqueSorted(bead.DependsOn),
		EstimatedMinutes:    bead.EstimatedMinutes,
		DescriptionWords:    wordCount(bead.Description),
		ImplementationNotes: uniqueSorted(bead.ImplementationNotes),
	}
}

func missingSectionTests(section PlanSection, beads []Bead) []string {
	if len(section.RequiredTests) == 0 {
		return nil
	}
	present := map[string]bool{}
	for _, bead := range beads {
		for _, obligation := range bead.TestObligations {
			present[strings.ToLower(strings.TrimSpace(obligation))] = true
		}
		for _, acceptance := range bead.AcceptanceCriteria {
			present[strings.ToLower(strings.TrimSpace(acceptance))] = true
		}
	}
	missing := []string{}
	for _, required := range section.RequiredTests {
		if !present[strings.ToLower(strings.TrimSpace(required))] {
			missing = append(missing, required)
		}
	}
	return uniqueSorted(missing)
}

func beadsForSection(beads []Bead, sectionID string) []Bead {
	out := []Bead{}
	for _, bead := range beads {
		for _, linked := range bead.PlanSections {
			if strings.TrimSpace(linked) == sectionID {
				out = append(out, bead)
				break
			}
		}
	}
	return out
}

func normalizedSections(sections []PlanSection) []PlanSection {
	out := make([]PlanSection, 0, len(sections))
	for _, section := range sections {
		section.ID = strings.TrimSpace(section.ID)
		out = append(out, section)
	}
	return out
}

func coverageScore(sections []SectionTrace) int {
	if len(sections) == 0 {
		return 0
	}
	score := 0
	for _, section := range sections {
		switch section.Status {
		case CoverageCovered:
			score += 100
		case CoveragePartial:
			score += 60
		}
	}
	return clampScore(score / len(sections))
}

func duplicateGroups(beads []BeadTrace) []DuplicateBeads {
	byTitle := map[string][]string{}
	for _, bead := range beads {
		key := strings.ToLower(strings.Join(strings.Fields(bead.Title), " "))
		if key == "" {
			continue
		}
		byTitle[key] = append(byTitle[key], bead.BeadID)
	}
	groups := []DuplicateBeads{}
	for title, ids := range byTitle {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		groups = append(groups, DuplicateBeads{Reason: "same normalized title: " + title, Beads: ids})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Reason < groups[j].Reason })
	return groups
}

func uniqueSorted(values []string) []string {
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
	sort.Strings(out)
	return out
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
