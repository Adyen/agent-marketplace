package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func BenchmarkOwnedHookContributorWorkLegacyMicro(b *testing.B) {
	discardBenchLogs(b)
	raw := []byte(`{"hooks":[{"command":"echo shared-proof","type":"command"}],"matcher":"Execute"}`)
	alpha := ownedJSONEntry{
		rel: "hooks.json", kind: ownership.KindHook, container: "PreToolUse",
		raw: raw, identity: ownership.HookIdentityDigest("PreToolUse", ownership.Digest(raw)), resource: "alpha",
	}
	beta := alpha
	beta.resource = "beta"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work := &ownedJSONAggregateWork{}
		work.add(alpha)
		work.add(beta)
		if len(work.entries) != 1 || work.entries[0].resource != "alpha" {
			b.Fatal("empty-proof duplicate changed the first winner")
		}
	}
}
