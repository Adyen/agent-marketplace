package materialize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func benchmarkHookReviewSetup(b *testing.B) (outputKey, ownedJSONFileSpec, string, []ownedJSONExistingEntry, ownership.Proof) {
	b.Helper()
	repo := b.TempDir()
	key := outputKey{target: hcc.TargetFactory}
	_, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		b.Fatal(err)
	}
	outDir := filepath.Join(repo, ".factory")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		b.Fatal(err)
	}
	abs := filepath.Join(outDir, filepath.Base(file.rel))

	raws := make([][]byte, 16)
	entries := make([]ownership.Entry, 16)
	existing := make([]ownedJSONExistingEntry, 16)
	body := map[string][]json.RawMessage{"PreToolUse": {}}
	for i := range raws {
		raw, err := canonicalOwnedJSON(json.RawMessage(fmt.Sprintf(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo bench-%02d"}]}`, i)))
		if err != nil {
			b.Fatal(err)
		}
		raws[i] = raw
		identity, err := ownedJSONHookIdentity("PreToolUse", raw)
		if err != nil {
			b.Fatal(err)
		}
		id := ownership.EntryID{Kind: ownership.KindHook, Container: "PreToolUse", IdentityDigest: identity}
		digest := ownership.Digest(raw)
		entries[i] = ownership.Entry{Kind: id.Kind, Container: id.Container, IdentityDigest: id.IdentityDigest, ValueDigest: digest, Resource: fmt.Sprintf("hook-%02d", i)}
		existing[i] = ownedJSONExistingEntry{
			value:     ownership.EntryValue{ID: id, ValueDigest: digest},
			container: "PreToolUse",
			raw:       raw,
		}
		body["PreToolUse"] = append(body["PreToolUse"], json.RawMessage(raw))
	}
	content, err := json.Marshal(body)
	if err != nil {
		b.Fatal(err)
	}
	store, err := ownership.Open(outDir)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := store.Apply(ownership.Mutation{Rel: file.rel, AbsFile: abs, NextBytes: content, Entries: entries, Reason: ownership.ReasonSync}); err != nil {
		b.Fatal(err)
	}
	proof, err := store.Proof(file.rel)
	if err != nil {
		b.Fatal(err)
	}
	return key, file, abs, existing, proof
}

func benchmarkHookReviewPlan(b *testing.B, existing []ownedJSONExistingEntry, mutate func(int) bool) ownedJSONPatch {
	b.Helper()
	elements := map[string][][]byte{"PreToolUse": {}}
	var owned []ownership.Entry
	for i, candidate := range existing {
		raw := candidate.raw
		if mutate(i) {
			raw, _ = canonicalOwnedJSON(json.RawMessage(fmt.Sprintf(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo bench-%02d-v2"}]}`, i)))
		}
		elements["PreToolUse"] = append(elements["PreToolUse"], raw)
		identity, err := ownedJSONHookIdentity("PreToolUse", raw)
		if err != nil {
			b.Fatal(err)
		}
		owned = append(owned, ownership.Entry{
			Kind:           ownership.KindHook,
			Container:      "PreToolUse",
			IdentityDigest: identity,
			ValueDigest:    ownership.Digest(raw),
			Resource:       fmt.Sprintf("hook-%02d", i),
		})
	}
	return ownedJSONPatch{owned: owned, hookElements: elements}
}

func BenchmarkNativeHookReviewDelta(b *testing.B) {
	key, file, abs, existing, proof := benchmarkHookReviewSetup(b)

	changed := benchmarkHookReviewPlan(b, existing, func(i int) bool { return i%2 == 0 })
	unchanged := benchmarkHookReviewPlan(b, existing, func(int) bool { return false })

	delta := nativeHookReviewDelta(key, file, abs, existing, proof, changed)
	if len(delta) != 8 {
		b.Fatalf("changed-plan delta = %#v, want 8 paired rows", delta)
	}
	for _, row := range delta {
		if len(row.Before) == 0 || len(row.After) == 0 {
			b.Fatalf("changed-plan row = %#v, want paired before+after", row)
		}
	}
	if rows := nativeHookReviewDelta(key, file, abs, existing, proof, unchanged); len(rows) != 0 {
		b.Fatalf("unchanged-plan delta = %#v, want zero rows", rows)
	}

	b.Run("before-after", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = nativeHookReviewDelta(key, file, abs, existing, proof, changed)
		}
	})
	b.Run("noop", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = nativeHookReviewDelta(key, file, abs, existing, proof, unchanged)
		}
	})
}
