package api

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkManagedAvailability(b *testing.B) {
	server, _, _, rev := availabilityFixture(b)
	view, err := server.loadAvailability(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	b.Run("legacy-decision", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if resourceAvailability(rev, store.Registry{Acquisition: store.AcquisitionMirror}, config.RejectedResourcesBlock) != AvailabilityAvailable {
				b.Fatal("not available")
			}
		}
	})
	b.Run("managed-decision", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if server.evaluateAvailability(view, rev).Availability != AvailabilityAvailable {
				b.Fatal("not available")
			}
		}
	})
	b.Run("snapshot-decode-no-database-IO", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := server.loadAvailability(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
