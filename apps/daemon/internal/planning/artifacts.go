package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// artifacts.go owns the planning pipeline's on-disk artifact IO and
// meta-snapshot helpers — atomic JSON writes, plain text writes, the
// `meta.json` round-trip that keeps Service.Run resumable, the
// `history.jsonl` append-only log of completed steps, and the small
// pure helpers (cloneStringMap, recordsFromMeta, digestString) the
// orchestrator needs to roll meta forward without leaking state.
//
// hp-k7as second cut: split out of planning.go to continue the
// "split runner/orchestration, artifacts/meta, quality evaluation,
// and normalization helpers" decomposition the bead opens against
// this 1,178-line file. Cuts so far: quality.go (#1, 7146da9), now
// artifacts.go (#2). Behavior unchanged - same package, same exported
// surface (none of these helpers are exported), same atomic-write
// semantics. Service.Run + restoreCompletedStep + runStep keep using
// these through same-package access.

func writeTextFile(path string, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o600)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func readPlanMeta(path string) (PlanMeta, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PlanMeta{}, false, nil
	}
	if err != nil {
		return PlanMeta{}, false, err
	}
	var meta PlanMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return PlanMeta{}, false, err
	}
	return meta, true, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func recordsFromMeta(meta PlanMeta) map[string]StepRecord {
	out := make(map[string]StepRecord, len(meta.Steps))
	for _, record := range meta.Steps {
		if record.ID != "" {
			out[record.ID] = record
		}
	}
	return out
}

func appendHistory(path string, entry historyEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
