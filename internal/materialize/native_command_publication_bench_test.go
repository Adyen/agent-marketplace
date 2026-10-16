package materialize

import (
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func BenchmarkNativeCommandUpdateDisk(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "commands")
	links := []skillLink{{name: "probe.md", artifacts: []artifact.Artifact{{Path: "probe.md", Bytes: []byte("---\ndescription: inert\n---\n\nfirst inert body"), Mode: 0o644}}}}
	if _, err := syncCommandFiles(dir, links, false, false); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		body := "first inert body"
		if i%2 == 0 {
			body = "second inert body"
		}
		links[0].artifacts[0].Bytes = []byte("---\ndescription: inert\n---\n\n" + body)
		if _, err := syncCommandFiles(dir, links, false, false); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
