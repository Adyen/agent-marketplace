package sourcerevision

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkBuild(b *testing.B) {
	quietRevisionBenchmark(b)
	snapshot := b.TempDir()
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("skill-%02d", i)
		writeFixture(b, filepath.Join(snapshot, ".factory", "skills", name, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: benchmark skill\n---\nBenchmark body %d.\n", name, i))
		writeFixture(b, filepath.Join(snapshot, ".factory", "skills", name, "guide.txt"), "supporting content\n")
	}
	input := Input{SnapshotRoot: snapshot, SourceHarness: "factory"}
	if _, err := Build(input); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(input); err != nil {
			b.Fatal(err)
		}
	}
}
