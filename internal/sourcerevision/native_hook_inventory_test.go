package sourcerevision

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const frozenInventorySkill = "---\nname: review\ndescription: Review changes\n---\nBody"

var frozenInventoryShapes = []struct {
	name, harness, skillPath, nativePath, nativeBody string
	handwrittenCount                                 int
	defaultHasHook                                   bool
}{
	{
		name: "factory", harness: "factory", skillPath: ".factory/skills/review/SKILL.md",
		nativePath: ".factory/hooks.json", handwrittenCount: 1, defaultHasHook: true,
		nativeBody: `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo native"}]}]}`,
	},
	{
		name: "cursor", harness: "cursor", skillPath: ".cursor/skills/review/SKILL.md",
		nativePath: ".cursor/hooks.json", handwrittenCount: 0, defaultHasHook: true,
		nativeBody: `{"version":1,"hooks":{"preToolUse":[{"command":"echo native"}]}}`,
	},
	{
		name: "windsurf", harness: "windsurf", skillPath: ".windsurf/skills/review/SKILL.md",
		nativePath: ".devin/hooks.json", handwrittenCount: 1, defaultHasHook: true,
		nativeBody: `{"hooks":{"pre_run_command":[{"command":"echo native"}]}}`,
	},
	{
		name: "claude", harness: "claude", skillPath: ".claude/skills/review/SKILL.md",
		nativePath: ".claude/settings.json", handwrittenCount: 1, defaultHasHook: true,
		nativeBody: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo native"}]}]}}`,
	},
	{
		name: "bare", harness: "claude", skillPath: "skills/review/SKILL.md",
		nativePath: ".claude/settings.json", handwrittenCount: 1, nativeBody: `{"permissions":{"allow":["Bash"]}}`,
	},
}

func writeFrozenInventoryFixture(t *testing.T, skillPath string, native map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{skillPath: frozenInventorySkill}
	for rel, body := range native {
		files[rel] = body
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRevisionFrozenInventoryIgnoresNativeHookFiles(t *testing.T) {
	if lockfile.CurrentRevisionSchema != "v4" {
		t.Fatalf("CurrentRevisionSchema = %q, want v4", lockfile.CurrentRevisionSchema)
	}
	for _, shape := range frozenInventoryShapes {
		for _, harness := range []string{shape.harness, ""} {
			mode := harness
			if mode == "" {
				mode = "auto"
			}
			t.Run(shape.name+"/"+mode, func(t *testing.T) {
				control, err := Build(Input{SnapshotRoot: writeFrozenInventoryFixture(t, shape.skillPath, nil), SourceHarness: harness, RevisionSchema: "v4"})
				if err != nil {
					t.Fatalf("control Build: %v", err)
				}
				resources := control.Projection().Resources
				if len(resources) != shape.handwrittenCount {
					t.Fatalf("control inventory = %#v, want %d resources", resources, shape.handwrittenCount)
				}
				if shape.handwrittenCount == 1 && (resources[0].Kind != "skill" || resources[0].Name != "review") {
					t.Fatalf("control inventory = %#v, want skill review", resources)
				}
				if shape.handwrittenCount == 0 {
					requireSkillReviewWarning(t, control.Warnings)
				}
				with, err := Build(Input{SnapshotRoot: writeFrozenInventoryFixture(t, shape.skillPath, map[string]string{shape.nativePath: shape.nativeBody}), SourceHarness: harness, RevisionSchema: "v4"})
				if err != nil {
					t.Fatalf("native-file Build: %v", err)
				}
				withResources := with.Projection().Resources
				if len(withResources) != len(resources) {
					t.Fatalf("native-file inventory = %#v, want the control inventory size %d", withResources, len(resources))
				}
				if shape.handwrittenCount == 0 {
					requireSkillReviewWarning(t, with.Warnings)
					if len(with.Warnings) != len(control.Warnings) {
						t.Fatalf("native-file warnings = %v, want the control warning count %d", with.Warnings, len(control.Warnings))
					}
				}
				if with.Manifest.RevisionDigest != control.Manifest.RevisionDigest {
					t.Fatalf("native hook file changed the frozen v4 revision digest: %s != %s", with.Manifest.RevisionDigest, control.Manifest.RevisionDigest)
				}
				if with.Catalog != nil {
					t.Fatal("native-file revision returned the digest-only historical catalog as its runtime catalog")
				}
				if !shape.defaultHasHook {
					return
				}
				current, err := catalog.Load(writeFrozenInventoryFixture(t, shape.skillPath, map[string]string{shape.nativePath: shape.nativeBody}), "")
				if err != nil {
					t.Fatalf("public default catalog: %v", err)
				}
				var sawSkill, sawHook bool
				for _, entry := range current.Entries {
					sawSkill = sawSkill || (entry.Kind == ir.KindSkill && entry.Name == "review")
					sawHook = sawHook || entry.Kind == ir.KindHook
				}
				if !sawSkill || !sawHook {
					t.Fatalf("public default catalog skill review = %v, native hook = %v; want both", sawSkill, sawHook)
				}
			})
		}
	}
}

func requireSkillReviewWarning(t *testing.T, warnings []ResourceWarning) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Kind == "skill" && warning.Name == "review" {
			return
		}
	}
	t.Fatalf("warnings = %v, want a structured skill review omission", warnings)
}
