package telemetry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuePushAndDrain(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)

	order := []int{1, 2, 3}
	for _, n := range order {
		if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, []byte(fmt.Sprintf("payload-%d", n))); err != nil {
			t.Fatalf("push %d: %v", n, err)
		}
	}

	if got := q.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}

	var drained []string
	drain := func(_ context.Context, data []byte) error {
		drained = append(drained, string(data))
		return nil
	}

	if err := q.Drain(context.Background(), drain); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(drained) != 3 {
		t.Fatalf("drained %d items, want 3", len(drained))
	}
	for i, n := range order {
		want := fmt.Sprintf("payload-%d", n)
		if drained[i] != want {
			t.Fatalf("drain order[%d] = %q, want %q", i, drained[i], want)
		}
	}

	if q.Count() != 0 {
		t.Fatalf("count after drain = %d, want 0", q.Count())
	}
}

func TestQueueUnsupportedSignal(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)

	err := q.Push(context.Background(), Route{Name: "primary", Signal: "logs"}, []byte("x"))
	if err == nil {
		t.Fatal("expected error for unsupported signal")
	}
}

func TestQueueEvictionBySize(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)
	q.maxBytes = 100

	payloads := [][]byte{
		[]byte("012345678901234567890123456789012345678901234567890123456789"),
		[]byte("abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz"),
		[]byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"),
	}

	for i, p := range payloads {
		if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, p); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	if q.Size() > q.maxBytes {
		t.Fatalf("queue size %d exceeds max %d", q.Size(), q.maxBytes)
	}

	// Only the most recent payload should fit after eviction.
	if q.Count() != 1 {
		t.Fatalf("count = %d, want 1 after size eviction", q.Count())
	}
}

func TestQueueEvictionByAge(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)
	q.maxAge = 0

	oldPath := filepath.Join(dir, queueSubdirMetrics, "stale.json")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldPath, time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, []byte("fresh")); err != nil {
		t.Fatalf("push: %v", err)
	}

	if q.Count() != 1 {
		t.Fatalf("count = %d, want 1 after age eviction", q.Count())
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("stale file should have been evicted")
	}
}

func TestQueueDrainFailureRequeues(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)

	if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, []byte("payload")); err != nil {
		t.Fatalf("push: %v", err)
	}

	calls := 0
	drain := func(_ context.Context, data []byte) error {
		calls++
		return fmt.Errorf("network down")
	}

	if err := q.Drain(context.Background(), drain); err == nil {
		t.Fatal("expected drain error")
	}

	if calls != 1 {
		t.Fatalf("send calls = %d, want 1", calls)
	}

	if q.Count() != 1 {
		t.Fatalf("count after failed drain = %d, want 1", q.Count())
	}
}

func TestQueueDrainConcurrent(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)

	if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, []byte("payload")); err != nil {
		t.Fatalf("push: %v", err)
	}

	var sent atomic.Int32
	started := make(chan struct{})
	resume := make(chan struct{})

	drain := func(_ context.Context, data []byte) error {
		sent.Add(1)
		close(started)
		<-resume
		return nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = q.Drain(context.Background(), drain)
		}()
	}

	<-started
	close(resume)
	wg.Wait()

	if sent.Load() != 1 {
		t.Fatalf("sent = %d, want 1", sent.Load())
	}
	if q.Count() != 0 {
		t.Fatalf("count = %d, want 0", q.Count())
	}
}

func TestQueueCrashRecovery(t *testing.T) {
	dir := t.TempDir()

	subdirs := []string{queueSubdirMetrics, queueSubdirTraces}
	for _, subdir := range subdirs {
		sub := filepath.Join(dir, subdir)
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		sending := filepath.Join(sub, "crash.json.sending")
		tmp := filepath.Join(sub, "inflight.json.tmp")
		normal := filepath.Join(sub, "existing.json")
		if err := os.WriteFile(sending, []byte("sending-payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmp, []byte("tmp-payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(normal, []byte("normal-payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	q := NewQueue(dir)

	if q.Count() != 4 {
		t.Fatalf("count = %d, want 4 recovered json files (2 sending + 2 existing)", q.Count())
	}

	for _, subdir := range subdirs {
		sub := filepath.Join(dir, subdir)
		entries, err := os.ReadDir(sub)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".sending") || strings.HasSuffix(name, ".tmp") {
				t.Fatalf("stale file %q should have been removed or renamed", name)
			}
		}
	}
}
