package materialize

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestPortableTreeCleanupPreservesNestedForeignMetadata(t *testing.T) {
	for _, replacement := range []string{"alias", "hardlink", "directory"} {
		t.Run(replacement, func(t *testing.T) {
			source := portableModeSource(t, 0o500, 0o510)
			external := portableModeSource(t, 0o555, 0o500)
			externalBefore := portableModeSnapshot(t, external)
			parent := t.TempDir()
			retained := filepath.Join(parent, "retained-nested")
			t.Cleanup(func() {
				for _, name := range []string{retained, filepath.Join(retained, "deeper")} {
					if err := os.Chmod(name, 0o700); err != nil {
						t.Error(err)
					}
				}
			})
			var stage string
			var retainedBefore, replacementBefore map[string]os.FileInfo
			boundary := &destinationBoundary{repoRoot: parent, linkPublicationHook: func(string) {
				stages, err := filepath.Glob(filepath.Join(parent, ".agent-marketplace-portable-tree-*"))
				if err != nil || len(stages) != 1 {
					t.Fatalf("stage fixture: %v %v", stages, err)
				}
				stage = stages[0]
				assertPortableModeTree(t, stage, 0o500, 0o510, "original payload")
				if err := os.Chmod(stage, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(stage, "nested"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(stage, "nested"), retained); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(retained, 0o510); err != nil {
					t.Fatal(err)
				}
				retainedBefore = portableModeSnapshot(t, retained)
				name := filepath.Join(stage, "nested")
				switch replacement {
				case "alias":
					if err := os.Symlink(external, name); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(filepath.Join(external, "SKILL.md"), name); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(name, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(name, "user.md"), []byte("foreign child"), 0o444); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(name, 0o510); err != nil {
						t.Fatal(err)
					}
					replacementBefore = portableModeSnapshot(t, name)
					t.Cleanup(func() {
						if err := os.Chmod(name, 0o700); err != nil {
							t.Error(err)
						}
					})
				}
			}}
			err := publishPortableTree(source, filepath.Join(parent, "output"), nil, "", boundary)
			if stage == "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("nested replacement did not refuse: %v", err)
			}
			assertPortableModeSnapshot(t, external, externalBefore)
			assertPortableModeSnapshot(t, retained, retainedBefore)
			assertCleanupBytes(t, filepath.Join(external, "SKILL.md"), "original payload")
			assertCleanupBytes(t, filepath.Join(external, "nested", "run.sh"), "#!/bin/sh\nexit 0\n")
			if replacement == "directory" {
				assertPortableModeSnapshot(t, filepath.Join(stage, "nested"), replacementBefore)
				assertCleanupBytes(t, filepath.Join(stage, "nested", "user.md"), "foreign child")
			} else {
				assertNoPortableTreeStage(t, parent)
			}
		})
	}
}
