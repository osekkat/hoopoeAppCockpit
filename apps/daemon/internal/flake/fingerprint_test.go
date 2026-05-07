package flake

import (
	"strings"
	"testing"
)

func TestFingerprintIsStableAcrossCosmeticDifferences(t *testing.T) {
	t.Parallel()
	rawA := strings.Join([]string{
		"2026-05-07T14:23:45.123Z [error] /home/agent/proj/src/foo.go:42",
		"  pointer 0xdeadbeef00000000 nil deref",
		"  goroutine 17 [running]:",
		"  serving on 127.0.0.1:54321 (123ms)",
	}, "\n")
	rawB := strings.Join([]string{
		"2026-05-07T19:01:02.987Z [error] /var/cache/proj/src/foo.go:42",
		"  pointer 0xfeedface11111111 nil deref",
		"  goroutine 942 [running]:",
		"  serving on 127.0.0.1:33333 (456ms)",
	}, "\n")

	if got, want := Fingerprint(rawA, "", "go1.26"), Fingerprint(rawB, "", "go1.26"); got != want {
		t.Fatalf("expected identical fingerprints across cosmetic deltas\n  A=%s\n  B=%s", got, want)
	}
}

func TestFingerprintDistinguishesDifferentRootCauses(t *testing.T) {
	t.Parallel()
	nilDeref := "panic: nil pointer dereference"
	indexBound := "panic: runtime error: index out of range [42] with length 7"

	if got, want := Fingerprint(nilDeref, "", "go1.26"), Fingerprint(indexBound, "", "go1.26"); got == want {
		t.Fatalf("two different root causes must fingerprint differently, got %s for both", got)
	}
}

func TestFingerprintIsToolVersionSensitive(t *testing.T) {
	t.Parallel()
	const raw = "panic: nil pointer dereference"
	v18 := Fingerprint(raw, "", "node-18.20.0")
	v20 := Fingerprint(raw, "", "node-20.10.0")
	if v18 == v20 {
		t.Fatalf("tool-version delta must produce distinct fingerprints, got %s for both", v18)
	}
}

func TestFingerprintEmptyToolVersionUsesSentinel(t *testing.T) {
	t.Parallel()
	a := Fingerprint("panic", "", "")
	b := Fingerprint("panic", "", "<no-tool-version>")
	if a != b {
		t.Fatalf("empty toolVersion must collapse to the sentinel; got %s vs %s", a, b)
	}
}

func TestFingerprintIdempotent(t *testing.T) {
	t.Parallel()
	const raw = "panic: nil pointer dereference at /home/x/foo.go:42 (123ms)"
	first := Fingerprint(raw, "", "go1.26")
	second := Fingerprint(raw, "", "go1.26")
	if first != second {
		t.Fatalf("fingerprint must be idempotent, got %s vs %s", first, second)
	}
}

func TestNormalizeStripsAbsolutePaths(t *testing.T) {
	t.Parallel()
	const raw = "FAIL /home/user/proj/src/foo/bar.go:42"
	out := Normalize(raw, "")
	if strings.Contains(out, "/home/user/proj") {
		t.Fatalf("absolute path leaked into normalized output: %q", out)
	}
	if !strings.Contains(out, "foo/bar.go") {
		t.Fatalf("expected trailing path segments preserved, got %q", out)
	}
}

func TestNormalizeRewritesProjectRootToToken(t *testing.T) {
	t.Parallel()
	const root = "/home/agent/proj"
	a := Normalize(root+"/src/foo.go failed", root)
	b := Normalize("/var/cache/builds/proj/src/foo.go failed", root)
	if a == b {
		t.Fatalf("projectRoot rewrite should leave a non-matching path in its absolute-path form, but they collapsed: a=%q b=%q", a, b)
	}
	if !strings.Contains(a, "<project>") {
		t.Fatalf("projectRoot rewrite missing <project> token: %q", a)
	}
}

func TestNormalizeStripsTimestamps(t *testing.T) {
	t.Parallel()
	cases := []string{
		"2026-05-07T14:23:45.123Z error",
		"2026-05-07 14:23:45 error",
		"14:23:45.123 error",
		"error (123ms)",
		"error (1.234s)",
	}
	want := "<ts>"
	for _, raw := range cases {
		out := Normalize(raw, "")
		if !strings.Contains(out, want) {
			t.Errorf("expected %q to contain %q, got %q", raw, want, out)
		}
	}
}

func TestNormalizeStripsHexAddressesAndGoroutineIDs(t *testing.T) {
	t.Parallel()
	out := Normalize("at 0xdeadbeef00 in goroutine 17 [running]: cafedeadbeef0123", "")
	if strings.Contains(out, "0xdeadbeef") {
		t.Fatalf("hex address leaked: %q", out)
	}
	if strings.Contains(out, "goroutine 17") {
		t.Fatalf("goroutine ID leaked: %q", out)
	}
	if strings.Contains(out, "cafedeadbeef0123") {
		t.Fatalf("long hex token leaked: %q", out)
	}
}

func TestNormalizeStripsEphemeralPorts(t *testing.T) {
	t.Parallel()
	out := Normalize("listening on 127.0.0.1:54321", "")
	if strings.Contains(out, "54321") {
		t.Fatalf("ephemeral port leaked: %q", out)
	}
	if !strings.Contains(out, "127.0.0.1") {
		t.Fatalf("expected host preserved: %q", out)
	}
}

func TestNormalizeSortsMultiBlockOutputDeterministically(t *testing.T) {
	t.Parallel()
	in := "block-z error\n\nblock-a error\n\nblock-m error"
	got := Normalize(in, "")
	want := "block-a error\n\nblock-m error\n\nblock-z error"
	if got != want {
		t.Fatalf("multi-block sort failed:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNormalizeEmptyInput(t *testing.T) {
	t.Parallel()
	if got := Normalize("", ""); got != "" {
		t.Fatalf("empty input must round-trip to empty, got %q", got)
	}
	if got := Fingerprint("", "", "go1.26"); got == "" {
		t.Fatalf("empty input fingerprint should still produce a deterministic digest, got empty string")
	}
}

func TestNormalizeSingleBlockUntouched(t *testing.T) {
	t.Parallel()
	const raw = "FAIL: TestX expected 1 got 2"
	out := Normalize(raw, "")
	if !strings.Contains(out, "TestX expected 1 got 2") {
		t.Fatalf("single-block content was mutated: %q", out)
	}
}

func TestFingerprintIs64HexChars(t *testing.T) {
	t.Parallel()
	got := Fingerprint("anything", "", "go1.26")
	if len(got) != 64 {
		t.Fatalf("fingerprint length = %d, want 64 (sha256 hex)", len(got))
	}
	for _, ch := range got {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			t.Fatalf("fingerprint contains non-hex char %q in %s", ch, got)
		}
	}
}
