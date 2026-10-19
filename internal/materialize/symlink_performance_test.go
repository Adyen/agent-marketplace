package materialize

import (
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestSymlinkCheckReadsDesiredContentWithoutWriting(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing_symlink", true: "legacy_file_tree"}[legacy], func(t *testing.T) {
			source, output := t.TempDir(), t.TempDir()
			t.Setenv("HOME", source)
			t.Setenv("XDG_CACHE_HOME", source)
			target := filepath.Join(source, ".cache", "agent-marketplace", "compiled", "demo")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			supportTree := filepath.Join(source, "skill")
			if err := os.MkdirAll(filepath.Join(supportTree, "references"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(supportTree, "references", "guide.md"), []byte("support"), 0o644); err != nil {
				t.Fatal(err)
			}
			var payloadReads []string
			supportPayloadReadHook = func(rel string) { payloadReads = append(payloadReads, rel) }
			t.Cleanup(func() { supportPayloadReadHook = nil })
			link := skillLink{name: "demo", target: target, sourceDir: supportTree, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("body")}}}
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
			if !legacy {
				if err := prepareSkillLinkCache(link); err != nil {
					t.Fatal(err)
				}
				payloadReads = nil
			}
			report, err := syncSkillLinks(output, []skillLink{link}, true, false, portableSyncContext{state: state, target: hcc.TargetFactory, reportPrefix: "skills"})
			if err != nil {
				t.Fatalf("check failed: %v", err)
			}
			if legacy {
				if len(payloadReads) == 0 {
					t.Fatal("legacy migration must read desired support content to compare ownership")
				}
				if data, readErr := os.ReadFile(filepath.Join(installed, "SKILL.md")); readErr != nil || string(data) != "body" {
					t.Fatalf("legacy content changed: %q, %v", data, readErr)
				}
				return
			}
			if len(payloadReads) == 0 || len(report.Skipped) != 1 || len(report.Written) != 0 {
				t.Fatalf("unchanged symlink must compare desired payloads without writes: reads=%v report=%+v", payloadReads, report)
			}
			if got, err := os.Readlink(installed); err != nil || got != target {
				t.Fatalf("symlink changed: %q, %v", got, err)
			}
		})
	}
}
