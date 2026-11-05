package harnesscompile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func materializeBookkeepingArtifacts(count int) []Artifact {
	artifacts := make([]Artifact, count)
	for i := range artifacts {
		artifacts[i] = Artifact{Path: fmt.Sprintf("file-%05d.txt", i), Bytes: []byte("unchanged payload\n"), Mode: 0o640}
	}
	return artifacts
}

// Include real SDK reads, admission, staging, publication and cleanup on disk.
// Exclude initial materialization, dirty-file resets, compilation and network.
func BenchmarkMaterializeBookkeepingDiskIONoNetwork(b *testing.B) {
	for _, tc := range []struct {
		name       string
		count      int
		writeEvery int
	}{
		{"skipped-1", 1, 0},
		{"skipped-64", 64, 0},
		{"skipped-1024", 1024, 0},
		{"skipped-4096", 4096, 0},
		{"mixed-1024", 1024, 128},
		{"written-16", 16, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			out := b.TempDir()
			result := &Result{Artifacts: materializeBookkeepingArtifacts(tc.count)}
			first, err := Materialize(result, out, WithWriteMode(WriteModeOverwrite))
			if err != nil || len(first.Written) != tc.count {
				b.Fatalf("seed materialization failed: %v, report=%+v", err, first)
			}
			before, err := os.Stat(filepath.Join(out, result.Artifacts[tc.count-1].Path))
			if err != nil {
				b.Fatal(err)
			}
			wantWritten := 0
			if tc.writeEvery > 0 {
				wantWritten = (tc.count + tc.writeEvery - 1) / tc.writeEvery
			}
			b.ReportAllocs()
			for b.Loop() {
				if tc.writeEvery > 0 {
					b.StopTimer()
					for i := 0; i < tc.count; i += tc.writeEvery {
						if err := os.WriteFile(filepath.Join(out, result.Artifacts[i].Path), []byte("dirty fixture\n"), 0o640); err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
				}
				report, err := Materialize(result, out, WithWriteMode(WriteModeOverwrite))
				if err != nil || len(report.Written) != wantWritten || len(report.Skipped) != tc.count-wantWritten {
					b.Fatalf("materialization failed: %v, report=%+v", err, report)
				}
			}
			after, err := os.Stat(filepath.Join(out, result.Artifacts[tc.count-1].Path))
			if err != nil {
				b.Fatal(err)
			}
			if tc.writeEvery != 1 && (!os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime())) {
				b.Fatal("unchanged control was replaced, chmodded or rewritten")
			}
		})
	}
}
