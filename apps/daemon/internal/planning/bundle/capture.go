// capture.go — file-capture primitive for the bundle discovery walk
// (hp-rsly fourth slice).
//
// `CaptureFile` reads a single file from the project root, applies a
// size cap, and returns a `*schemas.FileSnapshot` with the captured
// bytes, SHA-256, base64 encoding, and explicit truncation tracking.
//
// Why size cap + explicit truncation:
//
//   - The discovery walk (hp-rsly residual) caps each surface at a
//     documented limit (50 KB README, 100 KB total architecture docs,
//     etc.). A primitive that silently drops content would let the
//     model see partial files without flagging the elision; the
//     `TruncatedFromBytes` field surfaces that truncation to the UI's
//     "showing first N bytes of M" indicator.
//
//   - sha256 + base64 are both computed against the captured bytes,
//     not the original file: two builds with the same cap produce
//     identical FileSnapshots even when the underlying file grew
//     above the cap. The content-addressable cache relies on this.
//
// Errors:
//
//   - ErrFileNotFound — file doesn't exist at `path` (joined with
//     project root). Wraps `os.ErrNotExist` so callers can `errors.Is`.
//   - ErrPathTraversal — path escapes the project root after Clean.
//   - ErrInvalidSizeCap — sizeCap < 0.
//   - ErrIO — any other read error.

package bundle

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// DefaultFileSizeCap is the per-file ceiling used when the caller
// passes `0` for sizeCap. 50 KB matches the README cap from the
// hp-rsly bead body; the discovery walk overrides per surface
// (e.g., 100 KB total budget for architecture docs).
const DefaultFileSizeCap = 50 * 1024

var (
	// ErrFileNotFound wraps os.ErrNotExist for convenient errors.Is checks.
	ErrFileNotFound = errors.New("planning/bundle: file not found")

	// ErrPathTraversal is returned when the joined path escapes the project root.
	ErrPathTraversal = errors.New("planning/bundle: path escapes project root")

	// ErrInvalidSizeCap is returned when sizeCap is negative.
	ErrInvalidSizeCap = errors.New("planning/bundle: sizeCap must be >= 0")

	// ErrIO wraps a non-not-found read error.
	ErrIO = errors.New("planning/bundle: io error")
)

// CaptureFile reads `relativePath` (under `projectRoot`) up to
// `sizeCap` bytes and returns a FileSnapshot. Pass sizeCap=0 to use
// DefaultFileSizeCap.
//
// The returned FileSnapshot carries:
//
//   - Path: relativePath, normalized to POSIX-style separators.
//   - Sha256: SHA-256 of the captured bytes (lowercase hex).
//   - SizeBytes: number of captured bytes (== len(read)).
//   - ContentB64: base64 of the captured bytes.
//   - TruncatedFromBytes: original on-disk size, set ONLY when capture
//     truncated. Absent (nil) when the file fit under the cap.
func CaptureFile(projectRoot string, relativePath string, sizeCap int) (*schemas.FileSnapshot, error) {
	if sizeCap < 0 {
		return nil, ErrInvalidSizeCap
	}
	if sizeCap == 0 {
		sizeCap = DefaultFileSizeCap
	}
	if projectRoot == "" {
		return nil, fmt.Errorf("%w: projectRoot is required", ErrIO)
	}
	if relativePath == "" {
		return nil, fmt.Errorf("%w: relativePath is required", ErrIO)
	}

	cleanRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIO, err)
	}
	joined := filepath.Join(cleanRoot, relativePath)
	cleaned := filepath.Clean(joined)

	// Defense in depth: refuse paths that escape the project root.
	if cleaned != cleanRoot && !strings.HasPrefix(cleaned, cleanRoot+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: %s", ErrPathTraversal, relativePath)
	}

	stat, err := os.Stat(cleaned)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, relativePath)
		}
		return nil, fmt.Errorf("%w: stat: %v", ErrIO, err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("%w: %s is a directory", ErrIO, relativePath)
	}
	originalSize := stat.Size()

	f, err := os.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrIO, err)
	}
	defer f.Close()

	// Read up to sizeCap+1 so we can detect truncation in one pass.
	buf := make([]byte, sizeCap+1)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: read: %v", ErrIO, err)
	}
	captured := buf[:read]
	truncated := read > sizeCap
	if truncated {
		captured = captured[:sizeCap]
	}

	digest := sha256.Sum256(captured)
	snap := &schemas.FileSnapshot{
		Path:       toPosixPath(relativePath),
		Sha256:     hex.EncodeToString(digest[:]),
		SizeBytes:  len(captured),
		ContentB64: base64.StdEncoding.EncodeToString(captured),
	}
	if truncated {
		original := int(originalSize)
		snap.TruncatedFromBytes = &original
	}
	return snap, nil
}

// toPosixPath replaces native separators with `/` so the snapshot's
// Path field is portable across capture-on-Linux / replay-on-macOS
// developer setups.
func toPosixPath(p string) string {
	if os.PathSeparator == '/' {
		return p
	}
	return strings.ReplaceAll(p, string(os.PathSeparator), "/")
}
