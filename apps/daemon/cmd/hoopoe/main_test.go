package main

import "testing"

func TestVersionPlaceholder(t *testing.T) {
	if version == "" {
		t.Fatal("version must be non-empty")
	}
}
