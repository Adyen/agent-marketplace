package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkQueuePushDrain(b *testing.B) {
	dir := b.TempDir()
	q := NewQueue(dir)

	payload := []byte(`{"route":"primary","signal":"metrics","payload":"ZGF0YQ=="}`)
	send := func(_ context.Context, data []byte) error { return nil }

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Push(context.Background(), Route{Name: "primary", Signal: queueSubdirMetrics}, payload); err != nil {
			b.Fatal(err)
		}
		if err := q.Drain(context.Background(), send); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueueCrashRecovery(b *testing.B) {
	root := b.TempDir()
	subdir := filepath.Join(root, queueSubdirMetrics)
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		path := filepath.Join(subdir, fmt.Sprintf("queued-%03d.json", i))
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			b.Fatal(err)
		}
	}

	q := &Queue{root: root}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.recoverCrash()
	}
}
