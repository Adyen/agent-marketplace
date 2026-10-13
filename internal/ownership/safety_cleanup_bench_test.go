package ownership

import (
	"errors"
	"os"
	"testing"
)

func BenchmarkApplyCASCleanupDisk(b *testing.B) {
	discardBenchLogs(b)
	store, rel, absolute := benchStore(b)
	prior := benchAggregate(25)
	if err := os.WriteFile(absolute, prior, 0o600); err != nil {
		b.Fatal(err)
	}
	refused := errors.New("inert policy refusal")
	b.ReportAllocs()
	for b.Loop() {
		checks := 0
		_, err := store.Apply(Mutation{Rel: rel, AbsFile: absolute, NextBytes: []byte("inert next payload"), Reason: ReasonSync, ExpectPriorDigest: Digest(prior), ValidatePolicy: func() error {
			checks++
			if checks > 1 {
				return refused
			}
			return nil
		}})
		if !errors.Is(err, refused) {
			b.Fatalf("actual late CAS cleanup: %v", err)
		}
	}
}
