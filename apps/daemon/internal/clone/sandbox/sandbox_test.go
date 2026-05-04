package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveInsideAllowsRegularAndInternalSymlinkPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "README.md"), "# hi")
	mustMkdir(t, filepath.Join(root, "docs"))
	mustWrite(t, filepath.Join(root, "docs", "guide.md"), "guide")
	if err := os.Symlink(filepath.Join(root, "docs", "guide.md"), filepath.Join(root, "guide-link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := ResolveInside(root, "guide-link.md")
	if err != nil {
		t.Fatalf("ResolveInside: %v", err)
	}
	if got.Root == "" || got.Absolute != filepath.Join(root, "docs", "guide.md") || got.Relative != "docs/guide.md" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestResolveInsideRejectsTraversalAbsoluteOutsideAndSymlinkEscape(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	outside := filepath.Join(parent, "outside")
	mustMkdir(t, root)
	mustMkdir(t, outside)
	mustWrite(t, filepath.Join(root, "safe.md"), "ok")
	mustWrite(t, filepath.Join(outside, "secret.md"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "leak.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	tests := []string{
		"../outside/secret.md",
		filepath.Join(outside, "secret.md"),
		"leak.md",
	}
	for _, requested := range tests {
		_, err := ResolveInside(root, requested)
		if !errors.Is(err, ErrPathOutsideRoot) {
			t.Fatalf("ResolveInside(%q) err = %v, want ErrPathOutsideRoot", requested, err)
		}
	}
}

func TestOpenEditorCommandUsesExplicitArgvAndQuotedDisplay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "notes with spaces.md"), "notes")

	spec, err := OpenEditorCommand("/Applications/Visual Studio Code.app/bin/code", []string{"--goto"}, root, "notes with spaces.md")
	if err != nil {
		t.Fatalf("OpenEditorCommand: %v", err)
	}
	if spec.Path != "/Applications/Visual Studio Code.app/bin/code" {
		t.Fatalf("path = %q", spec.Path)
	}
	if !reflect.DeepEqual(spec.Args, []string{"--goto", filepath.Join(root, "notes with spaces.md")}) {
		t.Fatalf("args = %#v", spec.Args)
	}
	if strings.Contains(spec.Display, "notes with spaces.md") && !strings.Contains(spec.Display, "'") {
		t.Fatalf("display command was not quoted: %q", spec.Display)
	}
}

func TestOpenEditorCommandRejectsShellBinary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "README.md"), "ok")
	_, err := OpenEditorCommand("sh", []string{"-c", "code README.md"}, root, "README.md")
	if !errors.Is(err, ErrShellBinary) {
		t.Fatalf("err = %v, want ErrShellBinary", err)
	}
}

func TestRipgrepCommandUsesExplicitArgsAndRejectsTraversal(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	outside := filepath.Join(parent, "outside")
	mustMkdir(t, root)
	mustMkdir(t, outside)
	mustWrite(t, filepath.Join(root, "README.md"), "needle")
	mustWrite(t, filepath.Join(outside, "secret.md"), "secret")

	spec, err := RipgrepCommand(RipgrepRequest{
		Root:    root,
		Pattern: "needle; rm -rf /",
		Paths:   []string{"README.md"},
		Literal: true,
	})
	if err != nil {
		t.Fatalf("RipgrepCommand: %v", err)
	}
	wantArgs := []string{
		"--color=never",
		"--line-number",
		"--column",
		"--hidden",
		"--glob",
		"!.git",
		"--fixed-strings",
		"--",
		"needle; rm -rf /",
		"README.md",
	}
	if !reflect.DeepEqual(spec.Args, wantArgs) || spec.Path != "rg" || spec.Cwd != root {
		t.Fatalf("spec = %+v", spec)
	}
	for _, arg := range append([]string{spec.Path}, spec.Args...) {
		if arg == "sh" || arg == "-c" || arg == "&&" {
			t.Fatalf("unsafe shell token in argv: %#v", spec.Args)
		}
	}

	_, err = RipgrepCommand(RipgrepRequest{Root: root, Pattern: "secret", Paths: []string{"../outside/secret.md"}})
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("traversal err = %v, want ErrPathOutsideRoot", err)
	}
}

func TestRenderCommandRejectsUnparseableControlCharacters(t *testing.T) {
	t.Parallel()
	if _, err := RenderCommand([]string{"code", "ok\nbad"}); !errors.Is(err, ErrUnsafeArgv) {
		t.Fatalf("newline err = %v, want ErrUnsafeArgv", err)
	}
	got, err := RenderCommand([]string{"code", "--goto", "docs/Plan's notes.md"})
	if err != nil {
		t.Fatalf("RenderCommand: %v", err)
	}
	if got != "code --goto 'docs/Plan'\\''s notes.md'" {
		t.Fatalf("display = %q", got)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
