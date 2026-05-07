package bundle

import (
	"errors"
	"strings"
	"testing"
)

func TestSummarizeBeadsEmpty(t *testing.T) {
	out, err := SummarizeBeads(nil, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if out == nil {
		t.Errorf("out is nil; want empty slice for stable JSON shape")
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
}

func TestSummarizeBeadsSingle(t *testing.T) {
	out, err := SummarizeBeads([]RawBead{{
		Id:              "hp-abc",
		Title:           "demo bead",
		IssueType:       "task",
		Priority:        1,
		Status:          "open",
		DependencyCount: 3,
	}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	got := out[0]
	if got.Id != "hp-abc" || got.Title != "demo bead" || got.IssueType != "task" {
		t.Errorf("field copy mismatch: %+v", got)
	}
	if got.Priority != 1 || got.Status != "open" || got.DependencyCount != 3 {
		t.Errorf("priority/status/dep mismatch: %+v", got)
	}
}

func TestSummarizeBeadsMissingIdRejected(t *testing.T) {
	_, err := SummarizeBeads([]RawBead{{Id: "", IssueType: "task"}}, 0)
	if !errors.Is(err, ErrInvalidBead) {
		t.Errorf("err = %v, want ErrInvalidBead", err)
	}
}

func TestSummarizeBeadsMissingIssueTypeRejected(t *testing.T) {
	_, err := SummarizeBeads([]RawBead{{Id: "hp-abc"}}, 0)
	if !errors.Is(err, ErrInvalidBead) {
		t.Errorf("err = %v, want ErrInvalidBead", err)
	}
}

func TestSummarizeBeadsTitleTruncation(t *testing.T) {
	long := strings.Repeat("x", 250)
	out, err := SummarizeBeads([]RawBead{{Id: "hp-1", Title: long, IssueType: "task"}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if len([]rune(out[0].Title)) != MaxBeadTitleLen {
		t.Errorf("title rune-len = %d, want %d", len([]rune(out[0].Title)), MaxBeadTitleLen)
	}
	if !IsBeadTitleTruncated(out[0]) {
		t.Error("IsBeadTitleTruncated should report true for over-cap title")
	}
}

func TestSummarizeBeadsTitleAtCapNotTruncated(t *testing.T) {
	exact := strings.Repeat("y", MaxBeadTitleLen)
	out, err := SummarizeBeads([]RawBead{{Id: "hp-1", Title: exact, IssueType: "task"}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if out[0].Title != exact {
		t.Errorf("title at cap was modified: %q", out[0].Title)
	}
	if IsBeadTitleTruncated(out[0]) {
		t.Errorf("IsBeadTitleTruncated should report false for at-cap title")
	}
}

func TestSummarizeBeadsTitleMultiByteSafe(t *testing.T) {
	// 250 emoji characters; rune-aware slicing must not produce a
	// half-codepoint at the boundary.
	emoji := strings.Repeat("🐝", 250)
	out, err := SummarizeBeads([]RawBead{{Id: "hp-1", Title: emoji, IssueType: "task"}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	// Verify utf-8 round-trip is intact.
	for i, r := range out[0].Title {
		if r == '�' {
			t.Errorf("title contains replacement char at byte %d (mid-codepoint slice)", i)
		}
	}
}

func TestSummarizeBeadsCountCap(t *testing.T) {
	in := make([]RawBead, 150)
	for i := range in {
		in[i] = RawBead{Id: "hp-zzz", IssueType: "task", Priority: i % 5}
	}
	// Make IDs unique so sort is well-defined.
	for i := range in {
		in[i].Id = "hp-" + idFromInt(i)
	}
	out, err := SummarizeBeads(in, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if len(out) != MaxBeadSummaries {
		t.Errorf("len(out) = %d, want default cap %d", len(out), MaxBeadSummaries)
	}
}

func TestSummarizeBeadsExplicitMaxCount(t *testing.T) {
	in := []RawBead{
		{Id: "hp-a", IssueType: "task", Priority: 0},
		{Id: "hp-b", IssueType: "task", Priority: 1},
		{Id: "hp-c", IssueType: "task", Priority: 2},
	}
	out, err := SummarizeBeads(in, 2)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("len(out) = %d, want 2", len(out))
	}
	// Highest priority (lowest int) should survive the cap.
	if out[0].Id != "hp-a" || out[1].Id != "hp-b" {
		t.Errorf("cap kept wrong beads: %v", out)
	}
}

func TestSummarizeBeadsOrderingByPriorityThenID(t *testing.T) {
	in := []RawBead{
		{Id: "hp-z", IssueType: "task", Priority: 1},
		{Id: "hp-a", IssueType: "task", Priority: 1},
		{Id: "hp-m", IssueType: "task", Priority: 0},
	}
	out, err := SummarizeBeads(in, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	wantOrder := []string{"hp-m", "hp-a", "hp-z"}
	for i, w := range wantOrder {
		if out[i].Id != w {
			t.Errorf("out[%d].Id = %q, want %q", i, out[i].Id, w)
		}
	}
}

func TestSummarizeBeadsDeterministic(t *testing.T) {
	in := []RawBead{
		{Id: "hp-c", IssueType: "task", Priority: 1},
		{Id: "hp-a", IssueType: "task", Priority: 1},
		{Id: "hp-b", IssueType: "task", Priority: 1},
	}
	a, _ := SummarizeBeads(in, 0)
	// Re-shuffle.
	in[0], in[2] = in[2], in[0]
	b, _ := SummarizeBeads(in, 0)
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Id != b[i].Id {
			t.Errorf("order drift at %d: %q vs %q", i, a[i].Id, b[i].Id)
		}
	}
}

func TestSummarizeBeadsDefaultStatusAndPriority(t *testing.T) {
	out, err := SummarizeBeads([]RawBead{{Id: "hp-x", IssueType: "task"}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if out[0].Status != "open" {
		t.Errorf("default Status = %q, want open", out[0].Status)
	}
	if out[0].Priority != 0 {
		t.Errorf("default Priority = %d, want 0 (zero-value pass-through)", out[0].Priority)
	}
}

func TestSummarizeBeadsOutOfRangePriorityClamp(t *testing.T) {
	out, err := SummarizeBeads([]RawBead{{Id: "hp-x", IssueType: "task", Priority: 99}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if out[0].Priority != 4 {
		t.Errorf("out-of-range priority should clamp to 4, got %d", out[0].Priority)
	}
}

func TestSummarizeBeadsDependencyCountCopied(t *testing.T) {
	out, err := SummarizeBeads([]RawBead{{
		Id: "hp-x", IssueType: "task", DependencyCount: 7,
	}}, 0)
	if err != nil {
		t.Fatalf("SummarizeBeads: %v", err)
	}
	if out[0].DependencyCount != 7 {
		t.Errorf("DependencyCount = %d, want 7", out[0].DependencyCount)
	}
}

// idFromInt returns a 4-char alphabetical id derived from i. Avoids
// the test depending on `strconv.Itoa` (which would produce "hp-0"
// etc. — fine for this case but the alpha id better mimics br's
// real shape).
func idFromInt(i int) string {
	chars := []byte{'a', 'a', 'a', 'a'}
	for pos := 3; pos >= 0 && i > 0; pos-- {
		chars[pos] = byte('a' + (i % 26))
		i /= 26
	}
	return string(chars)
}
