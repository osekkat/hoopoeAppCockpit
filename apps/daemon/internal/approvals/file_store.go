package approvals

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// FileStore persists approvals as a JSONL append-only event log.
//
// hp-rh0w: production transport.prepareAuthRuntime previously constructed
// the approval queue with the implicit MemoryStore default, which meant
// every pending approval, every Approve/Deny decision, and every
// consumed/revoked record was lost across daemon restarts — undercutting
// plan.md §1.5 (restartability) and docs/security.md's approval-checkpoint
// promise. FileStore mirrors auth/pairing_store.go's flock+JSONL pattern:
// every Save appends a full schemas.Approval record on its own line, and
// Load replays the file with last-wins semantics per approval.Id while
// preserving first-appearance order so List remains stable across
// restarts.
//
// File format (one JSON object per line):
//
//	{"id": "...", "schemaVersion": 1, "state": "pending", ...}
//	{"id": "...", "schemaVersion": 1, "state": "approved", ...}
//
// State transitions append a new line; the final state wins. The file is
// flock-protected so concurrent daemon instances or supervisor restarts
// cannot interleave writes.
type FileStore struct {
	mu   sync.Mutex
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Save appends approval to the JSONL log under flock and fsyncs the file.
func (s *FileStore) Save(ctx context.Context, approval schemas.Approval) error {
	if s == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	if approval.Id == "" {
		return fmt.Errorf("%w: approval id is required", ErrInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withFileLock(func() error {
		return s.appendUnlocked(approval)
	})
}

// Get returns the latest persisted state for id by replaying the file.
func (s *FileStore) Get(ctx context.Context, id string) (schemas.Approval, bool, error) {
	if s == nil {
		return schemas.Approval{}, false, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return schemas.Approval{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var approval schemas.Approval
	var found bool
	err := s.withFileLock(func() error {
		records, _, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		approval, found = records[id]
		return nil
	})
	if err != nil || !found {
		return schemas.Approval{}, false, err
	}
	return cloneApproval(approval), true, nil
}

// List replays the file and returns approvals in first-appearance order.
func (s *FileStore) List(ctx context.Context) ([]schemas.Approval, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var ordered []schemas.Approval
	err := s.withFileLock(func() error {
		records, order, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		ordered = make([]schemas.Approval, 0, len(order))
		for _, id := range order {
			if record, ok := records[id]; ok {
				ordered = append(ordered, cloneApproval(record))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ordered, nil
}

func (s *FileStore) withFileLock(fn func() error) error {
	if s.path == "" {
		return fmt.Errorf("%w: empty approvals store path", ErrInvalidRequest)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	return fn()
}

// loadUnlocked replays the JSONL file. records map last-wins per id;
// order is the list of ids in first-appearance order. A missing file
// is treated as empty state, matching pairing_store semantics.
func (s *FileStore) loadUnlocked() (map[string]schemas.Approval, []string, error) {
	records := make(map[string]schemas.Approval)
	order := make([]string, 0)
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return records, order, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	// The default 64 KiB scanner buffer is large enough for an
	// approval record; if a future schema balloons past that we'll
	// need to lift the buffer cap, but no production approval comes
	// close today.
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var approval schemas.Approval
		if err := json.Unmarshal(line, &approval); err != nil {
			return nil, nil, fmt.Errorf("%w: malformed approval log line: %v", ErrInvalidRequest, err)
		}
		if approval.Id == "" {
			return nil, nil, fmt.Errorf("%w: approval log line missing id", ErrInvalidRequest)
		}
		if _, exists := records[approval.Id]; !exists {
			order = append(order, approval.Id)
		}
		records[approval.Id] = approval
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return records, order, nil
}

func (s *FileStore) appendUnlocked(approval schemas.Approval) error {
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(cloneApproval(approval)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
