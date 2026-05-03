package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CaptureTransport buffers a fixed-size ring of entries in memory. Tests use
// it to grab the last N entries on assertion failure. Safe for concurrent
// use; entries() returns a snapshot.
type CaptureTransport struct {
	mu      sync.Mutex
	cap     int
	entries []Entry
	head    int // ring head (next write position)
	full    bool
}

// NewCaptureTransport returns a CaptureTransport with the given capacity.
// A capacity ≤ 0 defaults to 200 (per the bead spec).
func NewCaptureTransport(capacity int) *CaptureTransport {
	if capacity <= 0 {
		capacity = 200
	}
	return &CaptureTransport{
		cap:     capacity,
		entries: make([]Entry, capacity),
	}
}

// Emit implements Transport.
func (c *CaptureTransport) Emit(entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[c.head] = entry
	c.head = (c.head + 1) % c.cap
	if c.head == 0 {
		c.full = true
	}
}

// Entries returns a snapshot of the buffered entries in chronological
// order. Length is min(emitted_count, capacity).
func (c *CaptureTransport) Entries() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.full {
		out := make([]Entry, c.head)
		copy(out, c.entries[:c.head])
		return out
	}
	out := make([]Entry, c.cap)
	// Walk head→end then 0→head-1 for chronological order.
	copy(out, c.entries[c.head:])
	copy(out[c.cap-c.head:], c.entries[:c.head])
	return out
}

// Len returns the number of currently-buffered entries.
func (c *CaptureTransport) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.full {
		return c.cap
	}
	return c.head
}

// Reset clears the buffer.
func (c *CaptureTransport) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head = 0
	c.full = false
}

// JSONLines returns the buffered entries serialized as newline-delimited
// JSON, suitable for dumping into a test evidence file.
func (c *CaptureTransport) JSONLines() string {
	entries := c.Entries()
	var out []byte
	for _, e := range entries {
		bytes, err := json.Marshal(e)
		if err != nil {
			continue
		}
		out = append(out, bytes...)
		out = append(out, '\n')
	}
	return string(out)
}

// FileTransport emits one JSON line per entry to a daily-rotated log file
// under `<dir>/<component>-<YYYY-MM-DD>.log`. The file is opened lazily on
// first emit and re-opened when the date rolls over (UTC).
type FileTransport struct {
	mu        sync.Mutex
	dir       string
	component string
	now       func() time.Time
	currentDay string
	file      *os.File
}

// NewFileTransport returns a FileTransport rooted at dir. The component
// segment of the filename is fixed at construction time (one transport per
// component). Returns an error if dir cannot be created.
func NewFileTransport(dir, component string) (*FileTransport, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("logger: mkdir %s: %w", dir, err)
	}
	return &FileTransport{
		dir:       dir,
		component: component,
		now:       time.Now,
	}, nil
}

// SetClock overrides the time source for tests.
func (f *FileTransport) SetClock(now func() time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = now
}

// Emit implements Transport.
func (f *FileTransport) Emit(entry Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensureOpen(); err != nil {
		// Drop the entry rather than crashing the daemon. Higher layers
		// monitor disk health separately (plan.md §10.5).
		return
	}
	bytes, err := json.Marshal(entry)
	if err != nil {
		return
	}
	bytes = append(bytes, '\n')
	_, _ = f.file.Write(bytes)
}

// Close flushes and closes the underlying file. Safe to call multiple
// times.
func (f *FileTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	f.currentDay = ""
	return err
}

func (f *FileTransport) ensureOpen() error {
	day := f.now().UTC().Format("2006-01-02")
	if f.file != nil && day == f.currentDay {
		return nil
	}
	if f.file != nil {
		_ = f.file.Close()
		f.file = nil
	}
	path := filepath.Join(f.dir, f.component+"-"+day+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	f.file = file
	f.currentDay = day
	return nil
}
