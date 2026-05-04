package acfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunReapsBashWhenCurlStartFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux /proc child inspection required")
	}

	dir := t.TempDir()
	fakeBash := filepath.Join(dir, "fake-bash")
	if err := os.WriteFile(fakeBash, []byte("#!/bin/sh\nwhile :; do sleep 60; done\n"), 0o755); err != nil {
		t.Fatalf("write fake bash: %v", err)
	}

	before := currentChildPIDs(t)
	_, err := (OSExecutor{Now: fixedNow}).Run(context.Background(), CommandSpec{
		CurlPath: filepath.Join(dir, "missing-curl"),
		BashPath: fakeBash,
		URL:      "https://example.invalid/install.sh",
		Ref:      "main",
		Timeout:  time.Minute,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "start curl") {
		t.Fatalf("Run error = %v, want curl start failure", err)
	}

	after := currentChildPIDs(t)
	for pid := range after {
		if _, existed := before[pid]; existed {
			continue
		}
		t.Fatalf("Run left child process %d after curl start failure", pid)
	}
}

func currentChildPIDs(t *testing.T) map[int]struct{} {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatalf("read /proc/self/task: %v", err)
	}
	children := make(map[int]struct{})
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join("/proc/self/task", entry.Name(), "children"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read task children: %v", err)
		}
		for _, field := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(field)
			if err != nil {
				t.Fatalf("parse child pid %q: %v", field, err)
			}
			children[pid] = struct{}{}
		}
	}
	return children
}

func fixedNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}
