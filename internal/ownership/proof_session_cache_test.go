package ownership

import (
	"os"
	"testing"
	"time"
)

func TestProofSessionCacheTracksCommittedGenerationAndExpires(t *testing.T) {
	f := newFixture(t)
	loads := 0
	store := f.store(WithProofLoadObserver(func() { loads++ }))
	oldBytes := []byte(`{"mcpServers":{"server":{"command":"old"}}}`)
	f.writeAggregate(string(oldBytes))
	mustApply(t, store, Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: oldBytes,
		Entries: []Entry{mcpEntry("server", "resource", `{"command":"old"}`)}, Reason: ReasonSync,
	})
	loads = 0

	if err := store.WithWriteSession("proof-cache", func() error {
		proof, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		nextBytes := []byte(`{"mcpServers":{"server":{"command":"new"}}}`)
		if _, err := store.Apply(Mutation{
			Rel: f.rel, AbsFile: f.absFile, NextBytes: nextBytes,
			Entries: []Entry{mcpEntry("server", "resource", `{"command":"new"}`)}, Reason: ReasonSync,
			ExpectPriorGeneration: proof.Generation(),
			ExpectPriorDigest:     Digest(oldBytes),
		}); err != nil {
			return err
		}
		updated, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		if updated.Generation() != 2 {
			t.Fatalf("generation after Apply = %d, want 2", updated.Generation())
		}
		return nil
	}); err != nil {
		t.Fatalf("WithWriteSession: %v", err)
	}
	if loads != 1 {
		t.Fatalf("Proof-path state loads in one session = %d, want 1", loads)
	}
	loads = 0
	for i := 0; i < 2; i++ {
		if _, err := store.Proof(f.rel); err != nil {
			t.Fatalf("Proof outside session: %v", err)
		}
	}
	if loads != 2 {
		t.Fatalf("Proof-path state loads outside a session = %d, want 2", loads)
	}

	store.tryLock = func(*os.File) error { return ErrLockUnsupported }
	loads = 0
	if err := store.WithWriteSession("degraded-proof-cache", func() error {
		for i := 0; i < 2; i++ {
			if _, err := store.Proof(f.rel); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("WithWriteSession on degraded lock: %v", err)
	}
	if loads != 2 {
		t.Fatalf("Proof-path state loads without a held lock = %d, want 2", loads)
	}
}

func TestProofSessionCacheReloadsWhenStateChangesExternally(t *testing.T) {
	f := newFixture(t)
	loads := 0
	store := f.store(WithProofLoadObserver(func() { loads++ }))
	initial := []byte(`{"mcpServers":{"server":{"command":"old"}}}`)
	f.writeAggregate(string(initial))
	mustApply(t, store, Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: initial,
		Entries: []Entry{mcpEntry("server", "resource", `{"command":"old"}`)}, Reason: ReasonSync,
	})
	loads = 0

	if err := store.WithWriteSession("external-proof-cache", func() error {
		if _, err := store.Proof(f.rel); err != nil {
			return err
		}
		foreignStore := f.store()
		record, err := foreignStore.Load()
		if err != nil {
			return err
		}
		record.Generation++
		if err := foreignStore.commitRecord(record); err != nil {
			return err
		}
		proof, err := store.Proof(f.rel)
		if err != nil {
			return err
		}
		if proof.Generation() != 2 {
			t.Fatalf("generation after external state write = %d, want 2", proof.Generation())
		}
		return nil
	}); err != nil {
		t.Fatalf("WithWriteSession: %v", err)
	}
	if loads != 2 {
		t.Fatalf("Proof-path state loads after external state write = %d, want 2", loads)
	}
}

func TestProofObserverDoesNotBlockSessionRelease(t *testing.T) {
	f := newFixture(t)
	store := f.store()
	initial := []byte(`{"mcpServers":{"server":{"command":"old"}}}`)
	f.writeAggregate(string(initial))
	mustApply(t, store, Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: initial,
		Entries: []Entry{mcpEntry("server", "resource", `{"command":"old"}`)}, Reason: ReasonSync,
	})
	release, err := store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	observed := make(chan struct{})
	resume := make(chan struct{}, 1)
	resumed := false
	defer func() {
		if !resumed {
			resume <- struct{}{}
		}
	}()
	store.proofLoadObserver = func() {
		close(observed)
		<-resume
	}
	proofDone := make(chan error, 1)
	go func() {
		_, err := store.Proof(f.rel)
		proofDone <- err
	}()
	<-observed

	releaseDone := make(chan struct{})
	go func() {
		release()
		close(releaseDone)
	}()
	select {
	case <-releaseDone:
	case <-time.After(time.Second):
		t.Fatal("session release blocked behind the Proof observer")
	}
	resume <- struct{}{}
	resumed = true
	if err := <-proofDone; err != nil {
		t.Fatalf("Proof: %v", err)
	}
}
