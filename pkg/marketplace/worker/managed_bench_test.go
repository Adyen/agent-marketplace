package worker

import (
	"context"
	"testing"
)

func BenchmarkManagedScheduleSQLiteIdempotent(b *testing.B) {
	deps, _ := managedFixture(b)
	ctx := context.Background()
	if err := scheduleManaged(ctx, deps, "bucket"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := scheduleManaged(ctx, deps, "bucket"); err != nil {
			b.Fatal(err)
		}
	}
}
