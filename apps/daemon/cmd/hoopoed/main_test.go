package main

import "testing"

func TestWantsMockMode(t *testing.T) {
	t.Setenv("HOOPOE_MOCK_SCENARIO", "")
	for _, args := range [][]string{
		{"--mock"},
		{"--mock=true"},
		{"-mock"},
		{"--addr", "127.0.0.1:0", "--mock"},
	} {
		if !wantsMockMode(args) {
			t.Fatalf("wantsMockMode(%v) = false, want true", args)
		}
	}

	if wantsMockMode([]string{"--addr", "127.0.0.1:0"}) {
		t.Fatal("wantsMockMode returned true without mock flag or env")
	}
	if wantsMockMode([]string{"--mock=false"}) {
		t.Fatal("wantsMockMode returned true for --mock=false")
	}

	t.Setenv("HOOPOE_MOCK_SCENARIO", "fresh")
	if !wantsMockMode(nil) {
		t.Fatal("wantsMockMode returned false with HOOPOE_MOCK_SCENARIO")
	}
}
