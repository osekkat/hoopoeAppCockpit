package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FuzzDefinitionParse exercises both definition deserializer paths (JSON via
// json.Unmarshal in LoadDefinitionFile and YAML via parseSimpleYAML at
// definitions.go:250). Definition bytes come from external configuration on
// disk, so the parsers must never panic on malformed input. Successful
// parses are then driven through DefinitionDisk.toDefinition to confirm the
// id/schedule/capabilities/repeat/timeout/max_concurrency invariants either
// pass Validate or surface ErrInvalidDefinition cleanly.
func FuzzDefinitionParse(f *testing.F) {
	for _, seed := range yamlSeeds() {
		f.Add(seed, true)
	}
	for _, seed := range jsonSeeds() {
		f.Add(seed, false)
	}

	f.Fuzz(func(t *testing.T, data []byte, asYAML bool) {
		var disk DefinitionDisk
		var parseErr error
		if asYAML {
			disk, parseErr = parseSimpleYAML(data)
		} else {
			parseErr = json.Unmarshal(data, &disk)
		}
		if parseErr != nil {
			// Lower-level parser errors are allowed to be wrapped or
			// surfaced bare (json.Unmarshal returns *json.SyntaxError /
			// *json.UnmarshalTypeError, which are not ErrInvalidDefinition
			// — that's intentional). The contract is just "never panic."
			return
		}

		def, defErr := disk.toDefinition()
		if defErr != nil {
			if !errors.Is(defErr, ErrInvalidDefinition) {
				t.Fatalf("toDefinition(%+v) returned non-domain error %v", disk, defErr)
			}
			return
		}

		if def.ID == "" {
			t.Fatalf("toDefinition succeeded but Definition.ID empty (disk=%+v)", disk)
		}
		if !def.Kind.Valid() {
			t.Fatalf("toDefinition succeeded but Kind %q invalid", def.Kind)
		}
		if err := def.Validate(); err != nil {
			t.Fatalf("toDefinition succeeded but Validate failed: %v (def=%+v)", err, def)
		}
		if err := def.Schedule.Validate(); err != nil {
			t.Fatalf("Schedule from accepted definition fails Validate: %v (schedule=%+v)", err, def.Schedule)
		}
		if def.Timeout < 0 {
			t.Fatalf("negative timeout %v from disk.Timeout=%q", def.Timeout, disk.Timeout)
		}
		if def.MaxConcurrency < 0 {
			t.Fatalf("negative max_concurrency %d from disk", def.MaxConcurrency)
		}
		if !def.Repeat.Forever && def.Repeat.Limit < 0 {
			t.Fatalf("negative repeat limit %d from disk.Repeat=%q", def.Repeat.Limit, disk.Repeat)
		}
		for _, cap := range def.CapabilitiesRequired {
			if cap == "" {
				t.Fatalf("empty capability_required from disk %+v", disk.CapabilitiesRequired)
			}
		}
	})
}

// FuzzLoadDefinitionFile drives the full file-IO path with a temp file so
// extension dispatch (.json vs .yaml/.yml) is also covered. Single-file
// scope keeps the per-execution overhead bounded compared to fuzzing
// LoadDefinitions over a directory.
func FuzzLoadDefinitionFile(f *testing.F) {
	for _, seed := range yamlSeeds() {
		f.Add(seed, ".yaml")
	}
	for _, seed := range yamlSeeds() {
		f.Add(seed, ".yml")
	}
	for _, seed := range jsonSeeds() {
		f.Add(seed, ".json")
	}
	f.Add([]byte("id: x\nkind: deterministic\nschedule: on demand\n"), ".unsupported")

	f.Fuzz(func(t *testing.T, data []byte, ext string) {
		// Restrict extensions to a small alphabet — Go fuzz mutates strings
		// freely, but the LoadDefinitionFile branch only cares about the
		// final segment. Anything outside the known set hits the "unsupported
		// extension" arm; we still want to ensure that arm doesn't panic.
		switch ext {
		case ".json", ".yaml", ".yml", ".unsupported":
		default:
			ext = ".unsupported"
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "def"+ext)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write temp definition: %v", err)
		}

		def, err := LoadDefinitionFile(context.Background(), path)
		if err != nil {
			return
		}
		if def.ID == "" {
			t.Fatalf("LoadDefinitionFile succeeded but Definition.ID empty: %+v", def)
		}
		if err := def.Validate(); err != nil {
			t.Fatalf("LoadDefinitionFile succeeded but Validate failed: %v", err)
		}
	})
}

func yamlSeeds() [][]byte {
	return [][]byte{
		[]byte(""),
		[]byte("\n"),
		[]byte("# comment-only\n"),
		[]byte("id: orchestrator-chat\nkind: orchestrator_chat\nschedule: on demand\nversion: 1\n"),
		[]byte("id: tend-1\nkind: deterministic\nschedule: every 5m\ntimeout: 30s\nmax_concurrency: 1\n"),
		[]byte("id: tend-cron\nkind: gated_agent\nschedule: \"0 */15 * * *\"\nrepeat: forever\nskills: [vibing-with-ntm, ntm]\n"),
		[]byte("id: tend-event\nkind: external_webhook\nschedule: \"on event: vps_commit_created\"\npaused: true\n"),
		[]byte("id: caps\nkind: deterministic\nschedule: on demand\ncapabilities_required: [\"git\", \"br\"]\ncapabilities_optional: ['rch']\n"),
		[]byte("id: with-extra\nkind: deterministic\nschedule: on demand\naudit_always: true\nretry_policy: exponential\nmisfire_policy: catch_up_bounded\ndead_letter_after: 5\n"),
		// Malformed lines / parser edge cases.
		[]byte("just-a-bare-line\n"),
		[]byte("id\n"),
		[]byte("schedule: \n"),
		[]byte("version: not-a-number\n"),
		[]byte("max_concurrency: 9999999999999999999999\n"),
		[]byte("dead_letter_after: -3\n"),
		[]byte("capabilities_required: [\n"),
		[]byte("capabilities_required: [a, , b,, ,]\n"),
		[]byte("schedule: every -1m\n"),
		[]byte("schedule: \"* * 31 2 *\"\n"),
		[]byte("kind: not-a-kind\nschedule: on demand\n"),
	}
}

func jsonSeeds() [][]byte {
	return [][]byte{
		[]byte("{}"),
		[]byte("null"),
		[]byte(`{"id":"json-od","kind":"orchestrator_chat","schedule":"on demand","version":1}`),
		[]byte(`{"id":"json-int","kind":"deterministic","schedule":"every 1h","timeout":"5m","max_concurrency":2}`),
		[]byte(`{"id":"json-cron","kind":"gated_agent","schedule":"0 12 * * 1-5","skills":["vibing-with-ntm"],"capabilities_required":["git"]}`),
		[]byte(`{"id":"json-bad","schedule":"every"}`),
		[]byte(`{"id":"json-impossible","kind":"deterministic","schedule":"* * 31 2 *"}`),
		[]byte(`{"max_concurrency":-1,"dead_letter_after":-9}`),
		[]byte(`{"id":"trail","kind":"deterministic","schedule":"on demand"`),
		[]byte(`[]`),
	}
}
