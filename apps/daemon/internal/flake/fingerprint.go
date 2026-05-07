package flake

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// hp-aa02 fingerprint normalizer: turn raw test/build output into a
// stable fingerprint string the failure ledger keys on.
//
// Per the bead's "FAILURE NORMALIZER" contract:
//   - Strip absolute paths → relative project paths.
//   - Strip timestamps + durations.
//   - Strip memory addresses, hex IDs.
//   - Sort multi-error blocks deterministically.
//   - Tool-version prefix (so a node v18 vs v20 stack delta produces
//     a different fingerprint).
//
// The returned digest is a 64-char hex sha256 of the normalized
// representation. Two raw logs that differ only in stripped fields
// must produce the same fingerprint; two logs that differ in any
// retained field (failed test ID, error type, normalized stack frame
// path) must produce different fingerprints.

// errorBlockSeparator splits a raw log into independently-orderable
// blocks. The default separator is a blank line — the de-facto block
// boundary in vitest, jest, pytest, go-test, cargo-test, and most
// build outputs.
const errorBlockSeparator = "\n\n"

var (
	// absolutePathPattern matches POSIX absolute paths (`/foo/bar/baz.ts`)
	// and Windows-style drive paths (`C:\foo\bar\baz.ts`). The capture
	// keeps the trailing basename so a normalized log still says
	// `at <project>/baz.ts:42` rather than `at :42`.
	absolutePathPattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:/[^\s:()'"]*)+`)

	// timestampPatterns matches:
	//   - ISO-8601 timestamps with optional fractional seconds + offset
	//   - bare time-of-day stamps (`14:23:45.123`)
	//   - test-runner duration suffixes (`(123ms)`, `(1.234s)`).
	timestampPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`),
		regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}(?:\.\d+)?\b`),
		regexp.MustCompile(`\(\s*\d+(?:\.\d+)?\s*(?:ns|µs|us|ms|s|m|h)\s*\)`),
	}

	// hexAddressPattern matches memory addresses (`0xdeadbeef`,
	// `0x7fff5fbff8a0`) and hash-like hex IDs of 8+ chars.
	hexAddressPattern = regexp.MustCompile(`0x[0-9a-fA-F]{4,}|\b[0-9a-fA-F]{8,}\b`)

	// goroutineIDPattern matches go-test goroutine IDs like
	// `goroutine 17 [running]:`.
	goroutineIDPattern = regexp.MustCompile(`goroutine\s+\d+`)

	// portPattern matches ephemeral ports like `127.0.0.1:54321`.
	portPattern = regexp.MustCompile(`(\d{1,3}(?:\.\d{1,3}){3}):\d+`)
)

// Normalize returns the canonical-form representation of raw used
// for fingerprinting. The output is intended to be stable across
// runs that share the same root cause but differ in cosmetic state
// (timestamps, paths, addresses, port allocations, goroutine IDs,
// block ordering inside the same run).
//
// projectRoot, when non-empty, is rewritten to the literal token
// `<project>/` wherever it appears in the log; this ensures a path
// like `/home/user/proj/foo.go` and `/var/cache/builds/proj/foo.go`
// fingerprint identically as long as `projectRoot` matches the
// build-host's project root.
func Normalize(raw, projectRoot string) string {
	if raw == "" {
		return ""
	}
	out := raw
	if projectRoot != "" {
		out = strings.ReplaceAll(out, projectRoot, "<project>")
	}
	out = absolutePathPattern.ReplaceAllStringFunc(out, normalizeAbsolutePath)
	for _, pattern := range timestampPatterns {
		out = pattern.ReplaceAllString(out, "<ts>")
	}
	out = hexAddressPattern.ReplaceAllString(out, "<hex>")
	out = goroutineIDPattern.ReplaceAllString(out, "goroutine <gid>")
	out = portPattern.ReplaceAllString(out, "$1:<port>")
	out = collapseWhitespace(out)
	out = sortBlocks(out)
	return out
}

// Fingerprint returns the 64-char hex sha256 of Normalize(raw,
// projectRoot) prefixed with toolVersion. The prefix ensures a
// node-v18 vs node-v20 stack delta produces distinct fingerprints
// even when the normalized text matches: two different runtimes can
// produce identical-looking errors with different remedies, and the
// ledger should treat them as distinct rows.
//
// An empty toolVersion is allowed (some sources don't expose one);
// in that case the prefix is the literal `<no-tool-version>`.
func Fingerprint(raw, projectRoot, toolVersion string) string {
	tool := toolVersion
	if tool == "" {
		tool = "<no-tool-version>"
	}
	normalized := Normalize(raw, projectRoot)
	preimage := fmt.Sprintf("%s\x00%s", tool, normalized)
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])
}

// normalizeAbsolutePath keeps the trailing two segments of an
// absolute path so the fingerprint preserves "which file was this
// in" without holding the build-host's filesystem layout.
//
// `/home/user/proj/src/foo/bar.go` → `<abs>/foo/bar.go`
// `/usr/lib/go/src/runtime/panic.go` → `<abs>/runtime/panic.go`
func normalizeAbsolutePath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return "<abs>" + path
	}
	tail := parts[len(parts)-2:]
	return "<abs>/" + strings.Join(tail, "/")
}

func collapseWhitespace(in string) string {
	lines := strings.Split(in, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

func sortBlocks(in string) string {
	blocks := strings.Split(in, errorBlockSeparator)
	if len(blocks) <= 1 {
		return in
	}
	sort.Strings(blocks)
	return strings.Join(blocks, errorBlockSeparator)
}
