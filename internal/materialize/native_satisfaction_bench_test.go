package materialize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

var benchmarkSatisfactionEvidenceMatched bool
var benchmarkSatisfactionEvidenceProof ownership.Proof

func BenchmarkNativeSatisfactionEvidence(b *testing.B) {
	const fingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, count := range []int{1, 16} {
		b.Run(fmt.Sprintf("records=%d", count), func(b *testing.B) {
			store, err := ownership.Open(b.TempDir(), ownership.WithStateRoot(b.TempDir()))
			if err != nil {
				b.Fatal(err)
			}
			current := make(map[ownership.EntryID]string, count)
			records := make([]any, 0, count)
			for i := range count {
				value := ownership.Digest([]byte(fmt.Sprintf(`{"command":"echo evidence-%d","type":"command"}`, i)))
				identity := ownership.HookIdentityDigest("PreToolUse", value)
				current[ownership.EntryID{Kind: ownership.KindHook, Container: "PreToolUse", IdentityDigest: identity}] = value
				records = append(records, map[string]any{
					"container": "PreToolUse", "identity_digest": identity, "value_digest": value,
					"sources": []map[string]string{{"resource": "alpha", "source_digest": fingerprint}},
				})
			}
			wire := map[string]any{
				"version": 1, "canonical_root": store.CanonicalRoot(), "root_key": store.RootKey(),
				"generation": 1, "updated_at": "2026-10-04T00:00:00Z",
				"files": map[string]any{"hooks.json": map[string]any{
					"content_digest": "", "entries": []any{}, "hook_satisfactions": records,
				}},
			}
			data, err := json.Marshal(wire)
			if err != nil {
				b.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(store.StateFile()), 0o700); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(store.StateFile(), data, 0o600); err != nil {
				b.Fatal(err)
			}
			proof, err := store.Proof("hooks.json")
			if err != nil {
				b.Fatal(err)
			}
			inspection := nativeHookInspection{proof: proof, current: current}
			b.Run("proof-disk", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					proof, err := store.Proof("hooks.json")
					if err != nil {
						b.Fatal(err)
					}
					benchmarkSatisfactionEvidenceProof = proof
				}
			})
			b.Run("match-in-memory", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchmarkSatisfactionEvidenceMatched = inspection.hookSatisfied(fingerprint, "alpha")
				}
			})
		})
	}
}
