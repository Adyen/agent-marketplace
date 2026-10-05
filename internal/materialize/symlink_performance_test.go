package materialize

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestSymlinkCheckOnlyReadsDesiredContentForLegacyMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing_symlink", true: "legacy_file_tree"}[legacy], func(t *testing.T) {
			source, output := t.TempDir(), t.TempDir()
			t.Setenv("HOME", source)
			t.Setenv("XDG_CACHE_HOME", source)
			target := filepath.Join(source, ".cache", "agent-marketplace", "compiled", "demo")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			unreadableTree := filepath.Join(source, "not-a-directory")
			if err := os.WriteFile(unreadableTree, []byte("not a support tree"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := skillLink{name: "demo", target: target, sourceDir: unreadableTree, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("body")}}}
			installed := filepath.Join(output, "demo")
			state := &portableState{entries: make(map[string]portableManifestEntry)}
			if legacy {
				if err := os.Mkdir(installed, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(installed, "SKILL.md"), []byte("body"), 0o644); err != nil {
					t.Fatal(err)
				}
				state.add(hcc.TargetFactory, "skills/demo", "")
			} else if err := os.Symlink(target, installed); err != nil {
				t.Fatal(err)
			}
			report, err := syncSkillLinks(output, []skillLink{link}, true, false, portableSyncContext{state: state, target: hcc.TargetFactory, reportPrefix: "skills"})
			if legacy {
				if !errors.Is(err, syscall.ENOTDIR) {
					t.Fatalf("legacy migration must validate desired content: %v", err)
				}
				if data, readErr := os.ReadFile(filepath.Join(installed, "SKILL.md")); readErr != nil || string(data) != "body" {
					t.Fatalf("legacy content changed: %q, %v", data, readErr)
				}
			} else {
				if err != nil || len(report.Skipped) != 1 || len(report.Written) != 0 {
					t.Fatalf("unchanged symlink must not read unused desired tree: report=%+v err=%v", report, err)
				}
				if got, err := os.Readlink(installed); err != nil || got != target {
					t.Fatalf("symlink changed: %q, %v", got, err)
				}
			}
		})
	}
}
