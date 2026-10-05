package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/portable-tree-publication-preserves-replacement-generation
func TestPortableReadOnlyOwnedLifecycle(t *testing.T) {
	for _, mode := range []os.FileMode{0o500, 0o555, 0o510} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			source := portableModeSource(t, mode, mode)
			parent := t.TempDir()
			skills := filepath.Join(parent, "skills")
			destination := filepath.Join(skills, "probe")
			resetPortableModeTreeOnCleanup(t, destination)
			state := &portableState{entries: make(map[string]portableManifestEntry)}
			boundary := &destinationBoundary{repoRoot: parent, roots: []string{parent}}
			sync := func() TargetReport {
				report, err := syncPortableSkillTrees(skills, []skillLink{{name: "probe", target: source}}, false, state, hcc.TargetFactory, "skills", make(map[string]bool), boundary)
				if err != nil {
					t.Fatalf("owned lifecycle, uid %d: %v", os.Geteuid(), err)
				}
				return report
			}
			if report := sync(); len(report.Written) != 1 {
				t.Fatalf("initial control: %+v", report)
			}
			assertPortableModeTree(t, destination, mode, mode, "original payload")
			for _, body := range []string{"first changed payload", "second changed payload"} {
				changePortableModeSourceBody(t, source, body)
				sourceBefore := portableModeSnapshot(t, source)
				if report := sync(); len(report.Written) != 1 {
					t.Fatalf("changed sync did not publish: %+v", report)
				}
				assertPortableModeTree(t, destination, mode, mode, body)
				assertPortableModeSnapshot(t, source, sourceBefore)
				before := portableModeSnapshot(t, destination)
				if report := sync(); len(report.Written) != 0 || len(report.Skipped) != 1 {
					t.Fatalf("identical repeat rewrote: %+v", report)
				}
				assertPortableModeSnapshot(t, destination, before)
				assertNoPortableTreeStage(t, skills)
			}
			reports, diags, err := prunePortableEntries(state, outputKey{target: hcc.TargetFactory}, parent, parent, make(map[string]bool), false)
			if err != nil || len(diags) != 0 || len(reports) != 1 || len(reports[0].Removed) != 1 || len(state.entries) != 0 {
				t.Fatalf("final-selection prune: %+v %+v %v", reports, diags, err)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("readonly owned tree remained: %v", err)
			}
			assertPortableModeTree(t, source, mode, mode, "second changed payload")
		})
	}
}

func TestPortableReadOnlyOwnedPrune(t *testing.T) {
	for _, mode := range []os.FileMode{0o500, 0o555, 0o510} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			source := portableModeSource(t, mode, mode)
			parent := t.TempDir()
			destination := filepath.Join(parent, "skills", "probe")
			resetPortableModeTreeOnCleanup(t, destination)
			state := &portableState{entries: make(map[string]portableManifestEntry)}
			if _, err := syncPortableSkillTrees(filepath.Dir(destination), []skillLink{{name: "probe", target: source}}, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
				t.Fatal(err)
			}
			sourceBefore := portableModeSnapshot(t, source)
			before := portableModeSnapshot(t, destination)
			if reports, _, err := prunePortableEntries(state, outputKey{target: hcc.TargetFactory}, parent, parent, make(map[string]bool), true); err != nil || len(reports) != 1 {
				t.Fatalf("dry-run prune control: %+v %v", reports, err)
			}
			assertPortableModeSnapshot(t, destination, before)
			if reports, _, err := prunePortableEntries(state, outputKey{target: hcc.TargetFactory}, parent, parent, make(map[string]bool), false); err != nil || len(reports) != 1 || len(state.entries) != 0 {
				t.Fatalf("readonly prune, uid %d: %+v %v", os.Geteuid(), reports, err)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("readonly prune left output: %v", err)
			}
			assertPortableModeSnapshot(t, source, sourceBefore)
		})
	}
}

func changePortableModeSourceBody(t *testing.T, source, body string) {
	t.Helper()
	path := filepath.Join(source, "SKILL.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
}
