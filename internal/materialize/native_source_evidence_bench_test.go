package materialize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

var benchmarkNativeSourceSatisfied bool

func BenchmarkNativeSourceSatisfaction(b *testing.B) {
	const first = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const second = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const other = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	root := b.TempDir()
	store, err := ownership.Open(root, ownership.WithStateRoot(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	value := ownership.Digest([]byte(`{"type":"command","command":"benchmark-source-proof"}`))
	entry := ownership.Entry{Kind: ownership.KindHook, Container: "PreToolUse", IdentityDigest: ownership.HookIdentityDigest("PreToolUse", value), ValueDigest: value, Resource: "alpha", SourceDigest: first}
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		b.Fatal(err)
	}
	var extended map[string]any
	if err := json.Unmarshal(entryBytes, &extended); err != nil {
		b.Fatal(err)
	}
	extended["source_evidence"] = []map[string]string{{"resource": "beta", "source_digest": second}}
	record := map[string]any{
		"version": 1, "canonical_root": store.CanonicalRoot(), "root_key": store.RootKey(), "generation": 1,
		"updated_at": "2026-10-01T00:00:00Z",
		"files":      map[string]any{"hooks.json": map[string]any{"content_digest": value, "entries": []any{extended}}},
	}
	data, err := json.Marshal(record)
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
	inspection := nativeHookInspection{proof: proof, current: map[ownership.EntryID]string{entry.ID(): value}}
	for _, tc := range []struct {
		name, resource, fingerprint string
	}{
		{"scalar", "alpha", first},
		{"second-contributor", "beta", second},
		{"mismatched-contributor", "beta", other},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkNativeSourceSatisfied = inspection.hookSatisfied(tc.fingerprint, tc.resource)
			}
		})
	}
}
