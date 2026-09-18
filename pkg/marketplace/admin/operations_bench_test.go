package admin

import (
	"context"
	"testing"
)

func BenchmarkManagedReadSQLite(b *testing.B) {
	s := managedFixture(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Read(ctx, "marketplace"); err != nil {
			b.Fatal(err)
		}
	}
}
