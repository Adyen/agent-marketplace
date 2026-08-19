package telemetry

import (
	"context"
	"testing"
)

func BenchmarkQueuePushDrain(b *testing.B) {
	dir := b.TempDir()
	q := NewQueue(dir)

	payload := []byte(`{"route":"primary","signal":"metrics","payload":"ZGF0YQ=="}`)
	send := func(data []byte) error { return nil }

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
