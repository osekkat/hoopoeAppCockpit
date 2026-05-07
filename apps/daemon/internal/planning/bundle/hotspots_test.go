package bundle

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func float32Ptr(f float32) *float32 { return &f }

func TestRankHotspotsEmpty(t *testing.T) {
	out, err := RankHotspots(nil, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if out == nil {
		t.Errorf("out is nil; want empty slice for stable JSON shape")
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
}

func TestRankHotspotsSingle(t *testing.T) {
	out, err := RankHotspots([]RawHotspot{{
		Path:            "src/foo.ts",
		CompositeScore:  87.5,
		Language:        "TypeScript",
		ChurnScore:      float32Ptr(12),
		ComplexityScore: float32Ptr(34),
	}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	got := out[0]
	if got.Path != "src/foo.ts" {
		t.Errorf("Path = %q, want src/foo.ts", got.Path)
	}
	if got.CompositeScore != 87.5 {
		t.Errorf("CompositeScore = %v, want 87.5", got.CompositeScore)
	}
	if got.Language == nil || *got.Language != "typescript" {
		t.Errorf("Language = %v, want lowercased typescript", got.Language)
	}
	if got.ChurnScore == nil || *got.ChurnScore != 12 {
		t.Errorf("ChurnScore = %v, want 12", got.ChurnScore)
	}
	if got.ComplexityScore == nil || *got.ComplexityScore != 34 {
		t.Errorf("ComplexityScore = %v, want 34", got.ComplexityScore)
	}
}

func TestRankHotspotsMissingPathRejected(t *testing.T) {
	_, err := RankHotspots([]RawHotspot{{Path: "", CompositeScore: 1}}, 0)
	if !errors.Is(err, ErrInvalidHotspot) {
		t.Errorf("err = %v, want ErrInvalidHotspot", err)
	}
}

func TestRankHotspotsNegativeCompositeRejected(t *testing.T) {
	_, err := RankHotspots([]RawHotspot{{Path: "foo.ts", CompositeScore: -1}}, 0)
	if !errors.Is(err, ErrInvalidHotspot) {
		t.Errorf("err = %v, want ErrInvalidHotspot", err)
	}
}

func TestRankHotspotsOrderingByCompositeDesc(t *testing.T) {
	in := []RawHotspot{
		{Path: "a.ts", CompositeScore: 10},
		{Path: "b.ts", CompositeScore: 50},
		{Path: "c.ts", CompositeScore: 25},
	}
	out, err := RankHotspots(in, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	wantOrder := []string{"b.ts", "c.ts", "a.ts"}
	for i, w := range wantOrder {
		if out[i].Path != w {
			t.Errorf("out[%d].Path = %q, want %q", i, out[i].Path, w)
		}
	}
}

func TestRankHotspotsTieBreakByPath(t *testing.T) {
	in := []RawHotspot{
		{Path: "z.ts", CompositeScore: 50},
		{Path: "a.ts", CompositeScore: 50},
		{Path: "m.ts", CompositeScore: 50},
	}
	out, err := RankHotspots(in, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	wantOrder := []string{"a.ts", "m.ts", "z.ts"}
	for i, w := range wantOrder {
		if out[i].Path != w {
			t.Errorf("out[%d].Path = %q, want %q (tie-break)", i, out[i].Path, w)
		}
	}
}

func TestRankHotspotsTopNCap(t *testing.T) {
	in := make([]RawHotspot, 100)
	for i := range in {
		in[i] = RawHotspot{
			Path:           "f" + string(rune('a'+i%26)) + ".ts",
			CompositeScore: float32(i),
		}
	}
	// Make path unique by suffix.
	for i := range in {
		in[i].Path = "src/" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + ".ts"
	}
	out, err := RankHotspots(in, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if len(out) != MaxHotspotSummaries {
		t.Errorf("len(out) = %d, want default cap %d", len(out), MaxHotspotSummaries)
	}
}

func TestRankHotspotsExplicitMaxCount(t *testing.T) {
	in := []RawHotspot{
		{Path: "a.ts", CompositeScore: 10},
		{Path: "b.ts", CompositeScore: 20},
		{Path: "c.ts", CompositeScore: 30},
	}
	out, err := RankHotspots(in, 2)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	// Top-2 by composite desc: c (30), b (20).
	if out[0].Path != "c.ts" || out[1].Path != "b.ts" {
		t.Errorf("cap kept wrong hotspots: %v", out)
	}
}

func TestRankHotspotsPathPosixNormalization(t *testing.T) {
	// On Linux, filepath.Join produces forward slashes — but verify
	// the explicit normalization doesn't break things if the input
	// arrives with a backslash (e.g., a Windows-built fixture in CI).
	weird := strings.ReplaceAll(filepath.Join("src", "foo.ts"), "/", "\\")
	out, err := RankHotspots([]RawHotspot{{Path: weird, CompositeScore: 1}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if strings.Contains(out[0].Path, "\\") {
		t.Errorf("Path contains backslash after normalization: %q", out[0].Path)
	}
}

func TestRankHotspotsAbsentLanguageStaysNil(t *testing.T) {
	out, err := RankHotspots([]RawHotspot{{Path: "a.ts", CompositeScore: 1}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if out[0].Language != nil {
		t.Errorf("Language = %v, want nil for absent input", out[0].Language)
	}
	if HasHotspotLanguage(out[0]) {
		t.Error("HasHotspotLanguage should report false for nil Language")
	}
}

func TestRankHotspotsLanguagePresent(t *testing.T) {
	out, err := RankHotspots([]RawHotspot{{
		Path: "a.go", CompositeScore: 1, Language: "Go",
	}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if !HasHotspotLanguage(out[0]) {
		t.Error("HasHotspotLanguage should report true for non-nil Language")
	}
	if out[0].Language == nil || *out[0].Language != "go" {
		t.Errorf("Language = %v, want lowercased go", out[0].Language)
	}
}

func TestRankHotspotsDeterministic(t *testing.T) {
	in := []RawHotspot{
		{Path: "src/c.ts", CompositeScore: 50},
		{Path: "src/a.ts", CompositeScore: 50},
		{Path: "src/b.ts", CompositeScore: 50},
	}
	a, _ := RankHotspots(in, 0)
	in[0], in[2] = in[2], in[0]
	b, _ := RankHotspots(in, 0)
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path {
			t.Errorf("order drift at %d: %q vs %q", i, a[i].Path, b[i].Path)
		}
	}
}

func TestRankHotspotsAllOptionalAbsent(t *testing.T) {
	// Composite score is the only required signal — every other
	// optional pointer should remain nil end-to-end.
	out, err := RankHotspots([]RawHotspot{{Path: "a.ts", CompositeScore: 1}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if out[0].ChurnScore != nil || out[0].ComplexityScore != nil || out[0].Language != nil {
		t.Errorf("optional fields not nil: %+v", out[0])
	}
}

func TestRankHotspotsZeroCompositeAccepted(t *testing.T) {
	// CompositeScore == 0 is a valid (lowest-impact) ranking. Only
	// negative is rejected.
	out, err := RankHotspots([]RawHotspot{{Path: "a.ts", CompositeScore: 0}}, 0)
	if err != nil {
		t.Fatalf("RankHotspots: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("len(out) = %d, want 1 for zero-composite hotspot", len(out))
	}
}
