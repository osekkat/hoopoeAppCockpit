package main

import "testing"

func TestVersionPlaceholder(t *testing.T) {
	if Version == "" {
		t.Fatal("Version constant must be non-empty")
	}
}
