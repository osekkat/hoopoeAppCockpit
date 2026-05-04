package log

import (
	"context"
	"sync"
	"testing"
)

func TestStoreReadsHundredMegabyteRangesByOffset(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	const (
		totalSize = int64(100 * 1024 * 1024)
		blockSize = int64(4 * 1024 * 1024)
	)
	appendPattern(t, ctx, store, "job_100mb", totalSize, blockSize)

	for _, tc := range []struct {
		name   string
		offset int64
		size   int64
	}{
		{name: "front", offset: 0, size: 1024 * 1024},
		{name: "middle", offset: 50 * 1024 * 1024, size: 10 * 1024 * 1024},
		{name: "tail", offset: 99 * 1024 * 1024, size: 1024 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunk, err := store.Read(ctx, "job_100mb", tc.offset, tc.size)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if chunk.Offset != tc.offset || chunk.NextOffset != tc.offset+tc.size || chunk.TotalBytes != totalSize {
				t.Fatalf("bad offsets: %+v", chunk)
			}
			assertPattern(t, chunk.Data, tc.offset)
		})
	}
}

func TestStoreReconnectByOffsetIsLossless(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	const (
		totalSize        = int64(8 * 1024 * 1024)
		disconnectOffset = int64(3 * 1024 * 1024)
	)
	appendPattern(t, ctx, store, "job_reconnect", totalSize, 1024*1024)

	first, err := store.Read(ctx, "job_reconnect", 0, disconnectOffset)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	second, err := store.Read(ctx, "job_reconnect", first.NextOffset, totalSize)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if first.NextOffset != disconnectOffset || second.NextOffset != totalSize || !second.EOF {
		t.Fatalf("unexpected reconnect chunks: first=%+v second=%+v", first, second)
	}
	assertPattern(t, first.Data, 0)
	assertPattern(t, second.Data, disconnectOffset)
}

func TestStoreConcurrentOffsetFetches(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	appendPattern(t, ctx, store, "job_concurrent", 8*1024*1024, 1024*1024)

	offsets := []int64{0, 128 * 1024, 2 * 1024 * 1024, 7 * 1024 * 1024}
	var wg sync.WaitGroup
	for _, offset := range offsets {
		offset := offset
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunk, err := store.Read(ctx, "job_concurrent", offset, 64*1024)
			if err != nil {
				t.Errorf("read offset %d: %v", offset, err)
				return
			}
			assertPattern(t, chunk.Data, offset)
		}()
	}
	wg.Wait()
}

func TestStorePollingCanCatchUpAsFileGrows(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	if next, err := store.Append(ctx, "job_active", []byte("abc")); err != nil || next != 3 {
		t.Fatalf("append first next=%d err=%v", next, err)
	}
	first, err := store.Read(ctx, "job_active", 0, 10)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if string(first.Data) != "abc" || first.NextOffset != 3 || !first.EOF {
		t.Fatalf("bad first chunk: %+v data=%q", first, string(first.Data))
	}

	if next, err := store.Append(ctx, "job_active", []byte("def")); err != nil || next != 6 {
		t.Fatalf("append second next=%d err=%v", next, err)
	}
	second, err := store.Read(ctx, "job_active", first.NextOffset, 10)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(second.Data) != "def" || second.NextOffset != 6 || second.TotalBytes != 6 {
		t.Fatalf("bad catch-up chunk: %+v data=%q", second, string(second.Data))
	}
}

func TestStoreRejectsUnsafeJobID(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	if _, err := store.Append(ctx, "../escape", []byte("bad")); err == nil {
		t.Fatal("append unsafe job id succeeded")
	}
	if _, err := store.Read(ctx, "nested/path", 0, 10); err == nil {
		t.Fatal("read unsafe job id succeeded")
	}
}

func appendPattern(t *testing.T, ctx context.Context, store Store, jobID string, totalSize int64, blockSize int64) {
	t.Helper()
	buf := make([]byte, blockSize)
	for written := int64(0); written < totalSize; {
		n := blockSize
		if remaining := totalSize - written; remaining < n {
			n = remaining
		}
		fillPattern(buf[:n], written)
		next, err := store.Append(ctx, jobID, buf[:n])
		if err != nil {
			t.Fatalf("append at %d: %v", written, err)
		}
		written += n
		if next != written {
			t.Fatalf("append next offset = %d, want %d", next, written)
		}
	}
}

func fillPattern(buf []byte, offset int64) {
	for i := range buf {
		buf[i] = patternByte(offset + int64(i))
	}
}

func assertPattern(t *testing.T, data []byte, offset int64) {
	t.Helper()
	for i, got := range data {
		if want := patternByte(offset + int64(i)); got != want {
			t.Fatalf("byte at offset %d = %d, want %d", offset+int64(i), got, want)
		}
	}
}

func patternByte(offset int64) byte {
	return byte(offset % 251)
}
