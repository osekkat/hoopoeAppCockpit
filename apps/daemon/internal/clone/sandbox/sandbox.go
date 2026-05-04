// Package sandbox validates operations against the desktop local-clone trust
// boundary. The clone contains arbitrary origin content, so file actions must
// resolve paths inside the clone root and subprocesses must use explicit argv.
package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidPath      = errors.New("clone sandbox: invalid path")
	ErrPathOutsideRoot  = errors.New("clone sandbox: path outside clone root")
	ErrMissingPath      = errors.New("clone sandbox: path does not exist")
	ErrUnsafeArgv       = errors.New("clone sandbox: unsafe argv")
	ErrShellBinary      = errors.New("clone sandbox: shell binary is not allowed")
	ErrInvalidRipgrep   = errors.New("clone sandbox: invalid ripgrep request")
	ErrInvalidEditorCmd = errors.New("clone sandbox: invalid editor command")
)

type ResolvedPath struct {
	Root     string
	Absolute string
	Relative string
}

type CommandSpec struct {
	Path    string
	Args    []string
	Cwd     string
	Display string
}

type RipgrepRequest struct {
	Binary  string
	Root    string
	Pattern string
	Paths   []string
	Literal bool
}

func ResolveInside(root string, requested string) (ResolvedPath, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return ResolvedPath{}, fmt.Errorf("%w: root required", ErrInvalidPath)
	}
	if hasControl(requested) {
		return ResolvedPath{}, fmt.Errorf("%w: requested path contains control characters", ErrInvalidPath)
	}
	if requested == "" {
		requested = "."
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: clone root %q: %v", ErrMissingPath, root, err)
	}

	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, candidate)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	candidateResolved, err := filepath.EvalSymlinks(candidateAbs)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %q: %v", ErrMissingPath, requested, err)
	}

	rel, err := filepath.Rel(rootResolved, candidateResolved)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ResolvedPath{}, fmt.Errorf("%w: %q", ErrPathOutsideRoot, requested)
	}
	if hasControl(rel) {
		return ResolvedPath{}, fmt.Errorf("%w: resolved path contains control characters", ErrInvalidPath)
	}
	if rel == "" {
		rel = "."
	}
	return ResolvedPath{
		Root:     rootResolved,
		Absolute: candidateResolved,
		Relative: filepath.ToSlash(rel),
	}, nil
}

func OpenEditorCommand(editorBinary string, editorArgs []string, root string, requested string) (CommandSpec, error) {
	editorBinary = strings.TrimSpace(editorBinary)
	if editorBinary == "" {
		return CommandSpec{}, fmt.Errorf("%w: binary required", ErrInvalidEditorCmd)
	}
	if isShellBinary(editorBinary) {
		return CommandSpec{}, fmt.Errorf("%w: %s", ErrShellBinary, editorBinary)
	}
	resolved, err := ResolveInside(root, requested)
	if err != nil {
		return CommandSpec{}, err
	}
	args := append([]string(nil), editorArgs...)
	args = append(args, resolved.Absolute)
	return newCommandSpec(editorBinary, args, "", ErrInvalidEditorCmd)
}

func RipgrepCommand(req RipgrepRequest) (CommandSpec, error) {
	binary := strings.TrimSpace(req.Binary)
	if binary == "" {
		binary = "rg"
	}
	if isShellBinary(binary) {
		return CommandSpec{}, fmt.Errorf("%w: %s", ErrShellBinary, binary)
	}
	if strings.TrimSpace(req.Pattern) == "" || hasControl(req.Pattern) {
		return CommandSpec{}, fmt.Errorf("%w: pattern required and must not contain control characters", ErrInvalidRipgrep)
	}
	root, err := ResolveInside(req.Root, ".")
	if err != nil {
		return CommandSpec{}, err
	}

	args := []string{
		"--color=never",
		"--line-number",
		"--column",
		"--hidden",
		"--glob",
		"!.git",
	}
	if req.Literal {
		args = append(args, "--fixed-strings")
	}
	args = append(args, "--", req.Pattern)

	if len(req.Paths) == 0 {
		args = append(args, ".")
	} else {
		for _, requested := range req.Paths {
			resolved, err := ResolveInside(req.Root, requested)
			if err != nil {
				return CommandSpec{}, err
			}
			args = append(args, resolved.Relative)
		}
	}
	return newCommandSpec(binary, args, root.Absolute, ErrInvalidRipgrep)
}

func RenderCommand(argv []string) (string, error) {
	if err := validateArgv(argv); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, QuoteArg(arg))
	}
	return strings.Join(parts, " "), nil
}

func QuoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if isBareSafe(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func newCommandSpec(path string, args []string, cwd string, wrap error) (CommandSpec, error) {
	argv := append([]string{path}, args...)
	if err := validateArgv(argv); err != nil {
		return CommandSpec{}, fmt.Errorf("%w: %v", wrap, err)
	}
	display, err := RenderCommand(argv)
	if err != nil {
		return CommandSpec{}, fmt.Errorf("%w: %v", wrap, err)
	}
	return CommandSpec{
		Path:    path,
		Args:    append([]string(nil), args...),
		Cwd:     cwd,
		Display: display,
	}, nil
}

func validateArgv(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%w: empty argv", ErrUnsafeArgv)
	}
	for i, arg := range argv {
		if i == 0 && strings.TrimSpace(arg) == "" {
			return fmt.Errorf("%w: command path required", ErrUnsafeArgv)
		}
		if hasControl(arg) {
			return fmt.Errorf("%w: arg %d contains control characters", ErrUnsafeArgv, i)
		}
	}
	if isShellBinary(argv[0]) {
		return fmt.Errorf("%w: %s", ErrShellBinary, argv[0])
	}
	return nil
}

func hasControl(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}

func isShellBinary(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "sh", "bash", "dash", "zsh", "fish", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func isBareSafe(arg string) bool {
	for _, r := range arg {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '@', '%', '+', '=', ':', ',', '.', '/', '-':
			continue
		default:
			return false
		}
	}
	return true
}
