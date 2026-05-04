// test_helpers_test.go — small file/dir helpers used by runner_test.go.
package migrations

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(content)
	return err
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// readBackupDir returns the names of files matching `daemon-*.db` in
// the given dir, ignoring subdirectories like `release/`.
func readBackupDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "daemon-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}
