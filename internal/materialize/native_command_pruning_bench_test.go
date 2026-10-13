package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

func BenchmarkNativeCommandRemovalDisk(b *testing.B) {
	for _, guarded := range []bool{false, true} {
		name := "legacy"
		if guarded {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			leaf := filepath.Join(b.TempDir(), "command.md")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				if err := os.WriteFile(leaf, []byte("---\n_managed_by: agent-marketplace\n---\n\ninert body"), 0o644); err != nil {
					b.Fatal(err)
				}
				info, err := os.Lstat(leaf)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if guarded {
					err = pruneNativeCommandFile(leaf, info, nil, false)
				} else {
					err = os.Remove(leaf)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNativeSkillMarkedFileDryRunDisk(b *testing.B) {
	root := b.TempDir()
	out := filepath.Join(root, "skills")
	if err := os.Mkdir(out, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "marked.md"), []byte("---\n_managed_by: agent-marketplace\n---\n\ninert marked body\n"), 0o640); err != nil {
		b.Fatal(err)
	}
	boundary := &destinationBoundary{repoRoot: root, roots: []string{out}}
	b.ReportAllocs()
	for b.Loop() {
		report, err := syncSkillLinks(out, nil, true, true, portableSyncContext{boundary: boundary})
		if err != nil || len(report.Removed) != 1 || report.Removed[0] != "marked.md" {
			b.Fatalf("dry-run control: %+v %v", report, err)
		}
	}
}

func BenchmarkNativeCommandSymlinkPruneDisk(b *testing.B) {
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	for _, dryRun := range []bool{false, true} {
		name := "write"
		if dryRun {
			name = "dry-run"
		}
		b.Run(name, func(b *testing.B) {
			root := b.TempDir()
			dir := filepath.Join(root, "commands")
			if err := os.Mkdir(dir, 0o755); err != nil {
				b.Fatal(err)
			}
			leaf := filepath.Join(dir, "stale.md")
			target := filepath.Join(cache.CacheDir(), "missing-target")
			boundary := &destinationBoundary{repoRoot: root, roots: []string{dir}}
			if dryRun {
				if err := os.Symlink(target, leaf); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if !dryRun {
					b.StopTimer()
					if err := os.Symlink(target, leaf); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
				report, err := syncCommandFiles(dir, nil, dryRun, true, portableSyncContext{boundary: boundary})
				if err != nil || len(report.Removed) != 1 || report.Removed[0] != "stale.md" {
					b.Fatalf("command symlink control: %+v %v", report, err)
				}
			}
		})
	}
}
