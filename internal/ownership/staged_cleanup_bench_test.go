package ownership

import (
	"errors"
	"os"
	"testing"
)

func BenchmarkRecoverStagedRollbackDisk(b *testing.B) {
	discardBenchLogs(b)
	store, rel, abs := benchStore(b)
	prior, next := []byte("inert prior payload"), []byte("inert next payload")
	if err := os.WriteFile(abs, prior, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		store.crasher = CrashAt(CrashAfterStagedWrite)
		_, err := store.Apply(Mutation{Rel: rel, AbsFile: abs, NextBytes: next, Reason: ReasonSync, ExpectPriorDigest: Digest(prior)})
		if !errors.Is(err, ErrInjectedCrash) {
			b.Fatalf("prepared mutation did not reach crash: %v", err)
		}
		store.crasher = nil
		if outcomes, err := store.Recover(); err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
			b.Fatalf("actual staged rollback: %v %v", outcomes, err)
		}
	}
}
