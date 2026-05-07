package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCaptureFileSmallFileBelowCap(t *testing.T) {
	root := t.TempDir()
	body := []byte("hello, world")
	writeFile(t, root, "README.md", body)

	snap, err := CaptureFile(root, "README.md", 1024)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	if snap.Path != "README.md" {
		t.Errorf("Path = %q, want README.md", snap.Path)
	}
	if snap.SizeBytes != len(body) {
		t.Errorf("SizeBytes = %d, want %d", snap.SizeBytes, len(body))
	}
	wantHash := sha256.Sum256(body)
	if snap.Sha256 != hex.EncodeToString(wantHash[:]) {
		t.Errorf("Sha256 = %q, want %q", snap.Sha256, hex.EncodeToString(wantHash[:]))
	}
	wantB64 := base64.StdEncoding.EncodeToString(body)
	if snap.ContentB64 != wantB64 {
		t.Errorf("ContentB64 = %q, want %q", snap.ContentB64, wantB64)
	}
	if snap.TruncatedFromBytes != nil {
		t.Errorf("TruncatedFromBytes = %v, want nil for non-truncated capture", *snap.TruncatedFromBytes)
	}
}

func TestCaptureFileEmptyFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "empty.md", []byte{})
	snap, err := CaptureFile(root, "empty.md", 1024)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	if snap.SizeBytes != 0 {
		t.Errorf("SizeBytes = %d, want 0", snap.SizeBytes)
	}
	if snap.ContentB64 != "" {
		t.Errorf("ContentB64 = %q, want empty", snap.ContentB64)
	}
	emptyHash := sha256.Sum256([]byte{})
	if snap.Sha256 != hex.EncodeToString(emptyHash[:]) {
		t.Errorf("Sha256 = %q, want empty-bytes hash", snap.Sha256)
	}
}

func TestCaptureFileExactlyAtCap(t *testing.T) {
	root := t.TempDir()
	body := bytes.Repeat([]byte("A"), 100)
	writeFile(t, root, "exact.md", body)
	snap, err := CaptureFile(root, "exact.md", 100)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	if snap.SizeBytes != 100 {
		t.Errorf("SizeBytes = %d, want 100", snap.SizeBytes)
	}
	if snap.TruncatedFromBytes != nil {
		t.Errorf("file at exact cap should not be marked truncated")
	}
}

func TestCaptureFileOverCapTruncates(t *testing.T) {
	root := t.TempDir()
	body := bytes.Repeat([]byte("B"), 200)
	writeFile(t, root, "big.md", body)
	snap, err := CaptureFile(root, "big.md", 100)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	if snap.SizeBytes != 100 {
		t.Errorf("SizeBytes = %d, want 100 (capped)", snap.SizeBytes)
	}
	if snap.TruncatedFromBytes == nil {
		t.Fatal("TruncatedFromBytes should be set for over-cap capture")
	}
	if *snap.TruncatedFromBytes != 200 {
		t.Errorf("TruncatedFromBytes = %d, want 200", *snap.TruncatedFromBytes)
	}
	// Sha256 + ContentB64 are over the captured (truncated) bytes.
	wantHash := sha256.Sum256(body[:100])
	if snap.Sha256 != hex.EncodeToString(wantHash[:]) {
		t.Errorf("Sha256 not over truncated bytes")
	}
}

func TestCaptureFileMissing(t *testing.T) {
	root := t.TempDir()
	_, err := CaptureFile(root, "nope.md", 1024)
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("err = %v, want ErrFileNotFound", err)
	}
}

func TestCaptureFileOnDirectoryFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := CaptureFile(root, "docs", 1024)
	if !errors.Is(err, ErrIO) {
		t.Fatalf("err = %v, want ErrIO", err)
	}
}

func TestCaptureFilePathTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := CaptureFile(root, "../etc/passwd", 1024)
	if !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("err = %v, want ErrPathTraversal", err)
	}
}

func TestCaptureFileNegativeSizeCap(t *testing.T) {
	root := t.TempDir()
	_, err := CaptureFile(root, "any.md", -1)
	if !errors.Is(err, ErrInvalidSizeCap) {
		t.Fatalf("err = %v, want ErrInvalidSizeCap", err)
	}
}

func TestCaptureFileZeroSizeCapUsesDefault(t *testing.T) {
	root := t.TempDir()
	body := bytes.Repeat([]byte("C"), DefaultFileSizeCap+10)
	writeFile(t, root, "big.md", body)

	snap, err := CaptureFile(root, "big.md", 0)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	if snap.SizeBytes != DefaultFileSizeCap {
		t.Errorf("SizeBytes = %d, want DefaultFileSizeCap=%d", snap.SizeBytes, DefaultFileSizeCap)
	}
	if snap.TruncatedFromBytes == nil || *snap.TruncatedFromBytes != DefaultFileSizeCap+10 {
		t.Errorf("TruncatedFromBytes mishandled: %v", snap.TruncatedFromBytes)
	}
}

func TestCaptureFileEmptyArgsRejected(t *testing.T) {
	_, err := CaptureFile("", "x.md", 1024)
	if err == nil {
		t.Fatal("empty projectRoot should error")
	}
	_, err = CaptureFile("/tmp", "", 1024)
	if err == nil {
		t.Fatal("empty relativePath should error")
	}
}

func TestCaptureFileAcceptsNestedPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, filepath.Join("docs", "architecture", "overview.md"), []byte("nested"))
	snap, err := CaptureFile(root, filepath.Join("docs", "architecture", "overview.md"), 1024)
	if err != nil {
		t.Fatalf("CaptureFile nested: %v", err)
	}
	// Path should be POSIX-style even on Windows builds.
	if strings.Contains(snap.Path, "\\") {
		t.Errorf("Path contains backslash: %q", snap.Path)
	}
}
