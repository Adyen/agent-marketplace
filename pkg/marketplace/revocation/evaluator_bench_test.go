package revocation

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkEvaluatorLookupOverlay(b *testing.B) {
	const targetCount = 256
	revisions := make([]string, 0, targetCount)
	for i := 0; i < targetCount; i++ {
		digest := fmt.Sprintf("sha256:%064x", i)
		revisions = append(revisions, fmt.Sprintf("mp/src/skill/skill-%03d@%s", i, digest))
	}
	evaluator, err := New(config.Config{Revocations: config.Revocations{Revisions: revisions}})
	if err != nil {
		b.Fatal(err)
	}
	revision := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "skill-127"},
		ContentDigest: "sha256:000000000000000000000000000000000000000000000000000000000000007f",
	}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if evaluator.Apply(revision).Revocation == nil {
			b.Fatal("expected benchmark revision to be revoked")
		}
	}
}
