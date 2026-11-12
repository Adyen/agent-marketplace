package ownership

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func sessionCacheFixture(t *testing.T) (*fixture, string, []byte) {
	t.Helper()
	f := newFixture(t)
	store := f.store()
	first := []byte(`{"mcpServers":{"alpha":{"command":"alpha"}}}`)
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: first,
		Entries: []Entry{mcpEntry("alpha", "alpha", `{"command":"alpha"}`)}, Reason: ReasonSync})
	secondRel := "hooks.json"
	secondAbs := filepath.Join(f.targetRoot, secondRel)
	mustApply(t, store, Mutation{
		Rel: secondRel, AbsFile: secondAbs,
		NextBytes: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo hook"}]}}`),
		Entries:   []Entry{hookEntry("PreToolUse", "hook", `{"type":"command","command":"echo hook"}`)},
		Reason:    ReasonSync,
	})
	return f, secondRel, first
}

func TestWriteSessionProofsLoadStateOnce(t *testing.T) {
	f, secondRel, _ := sessionCacheFixture(t)
	loads := 0
	store := f.store(WithStateLoadObserver(func() { loads++ }))
	if err := store.WithWriteSession("proofs", func() error {
		for range 3 {
			if _, err := store.Proof(f.rel); err != nil {
				return err
			}
			if _, err := store.Proof(secondRel); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("state loads = %d, want 1 for two recorded files", loads)
	}
}

func TestProofOutsideWriteSessionLoadsStateEveryTime(t *testing.T) {
	f, _, _ := sessionCacheFixture(t)
	loads := 0
	store := f.store(WithStateLoadObserver(func() { loads++ }))
	for range 3 {
		if _, err := store.Proof(f.rel); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 3 {
		t.Fatalf("state loads = %d, want one for each unlocked Proof", loads)
	}
}

func TestWriteSessionApplySeedsProofCache(t *testing.T) {
	f, _, first := sessionCacheFixture(t)
	loads := 0
	store := f.store(WithStateLoadObserver(func() { loads++ }))
	if err := store.WithWriteSession("apply", func() error {
		prior, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		next := []byte(`{"mcpServers":{"alpha":{"command":"updated"}}}`)
		_, err = store.Apply(Mutation{
			Rel: f.rel, AbsFile: f.absFile, NextBytes: next,
			Entries: []Entry{mcpEntry("alpha", "alpha", `{"command":"updated"}`)},
			Reason:  ReasonSync, ExpectPriorGeneration: prior.Generation(),
			ExpectPriorDigest: Digest(first),
		})
		if err != nil {
			return err
		}
		after, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		if after.Generation() != prior.Generation()+1 {
			t.Fatalf("Proof generation = %d, want %d", after.Generation(), prior.Generation()+1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("state loads = %d, want 1 including Apply", loads)
	}
}

func TestWriteSessionProofReloadsReplacedState(t *testing.T) {
	f, _, _ := sessionCacheFixture(t)
	loads := 0
	store := f.store(WithStateLoadObserver(func() { loads++ }))
	if err := store.WithWriteSession("replace", func() error {
		record, err := store.Load()
		if err != nil {
			return err
		}
		firstGeneration := record.Generation
		replacement := cloneRecord(record)
		replacement.Generation += 100
		data, err := json.Marshal(replacement)
		if err != nil {
			return err
		}
		temp := filepath.Join(filepath.Dir(store.StateFile()), "replacement.json")
		if err := os.WriteFile(temp, data, 0o600); err != nil {
			return err
		}
		if err := os.Rename(temp, store.StateFile()); err != nil {
			return err
		}
		proof, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		if proof.Generation() != firstGeneration+100 {
			t.Fatalf("Proof generation = %d, want replacement generation %d", proof.Generation(), firstGeneration+100)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("state loads = %d, want 2 after the replacement", loads)
	}
}
