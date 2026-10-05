package materialize

import (
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkPortableOwnedUpdateDisk(b *testing.B) {
	for _, readonly := range []bool{false, true} {
		name := "writable"
		if readonly {
			name = "readonly"
		}
		b.Run(name, func(b *testing.B) {
			source, parent := ownedRemovalBenchmarkFixture(b)
			if readonly {
				setOwnedRemovalBenchmarkModes(b, source, 0o555)
			}
			state := &portableState{entries: make(map[string]portableManifestEntry)}
			links := []skillLink{{name: "probe", target: source}}
			skills := filepath.Join(parent, "skills")
			b.Cleanup(func() { setOwnedRemovalBenchmarkModes(b, filepath.Join(skills, "probe"), 0o755) })
			if _, err := syncPortableSkillTrees(skills, links, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			index := 0
			for b.Loop() {
				b.StopTimer()
				body := "changed one"
				if index%2 == 0 {
					body = "changed two"
				}
				if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(body), 0o640); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, err := syncPortableSkillTrees(skills, links, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
					b.Fatal(err)
				}
				index++
			}
		})
	}
}

func BenchmarkPortableOwnedPruneDisk(b *testing.B) {
	for _, readonly := range []bool{false, true} {
		name := "writable"
		if readonly {
			name = "readonly"
		}
		b.Run(name, func(b *testing.B) {
			source, parent := ownedRemovalBenchmarkFixture(b)
			if readonly {
				setOwnedRemovalBenchmarkModes(b, source, 0o555)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				state := &portableState{entries: make(map[string]portableManifestEntry)}
				if _, err := syncPortableSkillTrees(filepath.Join(parent, "skills"), []skillLink{{name: "probe", target: source}}, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, _, err := prunePortableEntries(state, outputKey{target: hcc.TargetFactory}, parent, parent, make(map[string]bool), false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func ownedRemovalBenchmarkFixture(b *testing.B) (string, string) {
	b.Helper()
	source, parent := b.TempDir(), b.TempDir()
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o755); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"SKILL.md", "nested/data.txt"} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte("source content"), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(func() { setOwnedRemovalBenchmarkModes(b, source, 0o755) })
	return source, parent
}

func setOwnedRemovalBenchmarkModes(b *testing.B, root string, mode os.FileMode) {
	b.Helper()
	for _, name := range []string{root, filepath.Join(root, "nested")} {
		if err := os.Chmod(name, mode); err != nil && !os.IsNotExist(err) {
			b.Error(err)
		}
	}
}
