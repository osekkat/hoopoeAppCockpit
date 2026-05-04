// Package log owns persisted byte-addressable job log storage.
package log

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultReadLimit = int64(256 * 1024)
	MaxReadLimit     = int64(16 * 1024 * 1024)
)

type Store struct {
	Dir string
}

type Chunk struct {
	Offset     int64
	NextOffset int64
	TotalBytes int64
	Data       []byte
	EOF        bool
}

func (s Store) Append(ctx context.Context, jobID string, data []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !validJobID(jobID) {
		return 0, fmt.Errorf("job log: invalid job id")
	}
	if s.Dir == "" {
		return 0, fmt.Errorf("job log: empty log dir")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(s.path(jobID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	start, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(data); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	return start + int64(len(data)), nil
}

func (s Store) Read(ctx context.Context, jobID string, offset int64, limit int64) (Chunk, error) {
	if err := ctx.Err(); err != nil {
		return Chunk{}, err
	}
	if !validJobID(jobID) {
		return Chunk{}, fmt.Errorf("job log: invalid job id")
	}
	if s.Dir == "" {
		return Chunk{}, fmt.Errorf("job log: empty log dir")
	}
	if offset < 0 {
		return Chunk{}, fmt.Errorf("job log: negative offset")
	}
	limit = NormalizeLimit(limit)

	f, err := os.Open(s.path(jobID))
	if os.IsNotExist(err) {
		return Chunk{Offset: offset, NextOffset: offset, TotalBytes: 0, EOF: true}, nil
	}
	if err != nil {
		return Chunk{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Chunk{}, err
	}
	total := info.Size()
	if offset >= total {
		return Chunk{Offset: offset, NextOffset: offset, TotalBytes: total, EOF: true}, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return Chunk{}, err
	}
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return Chunk{}, err
	}
	next := offset + int64(n)
	return Chunk{
		Offset:     offset,
		NextOffset: next,
		TotalBytes: total,
		Data:       buf[:n],
		EOF:        next >= total,
	}, nil
}

func NormalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultReadLimit
	}
	if limit > MaxReadLimit {
		return MaxReadLimit
	}
	return limit
}

func (s Store) path(jobID string) string {
	return filepath.Join(s.Dir, jobID+".log")
}

func validJobID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}
