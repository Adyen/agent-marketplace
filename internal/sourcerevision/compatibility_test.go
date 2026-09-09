package sourcerevision

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestSourceRevisionCompatibilityVectors(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, harness := range []string{"claude", "factory"} {
			t.Run(version+"/"+harness, func(t *testing.T) {
				root := t.TempDir()
				for name, content := range map[string]string{
					"skills/directory/SKILL.md":           "---\nname: named\n---\nbody\n",
					"skills/directory/guide.txt":          "guide\n",
					"skills/broken/SKILL.md":              "---\nname: [broken\n---\nbody\n",
					"commands/review.md":                  "---\nname: review\n---\nReview <this>.\n",
					"agents/helper.md":                    "---\nname: helper\ndescription: Help\n---\nHelp.\n",
					"hooks/hooks.json":                    `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./check.sh"}]}]}}`,
					"hooks/check.sh":                      "echo checked\n",
					"hooks/nested/ignored.sh":             "not emitted\n",
					".mcp.json":                           `{"mcpServers":{"demo":{"command":"demo"}}}`,
					"collections/starter/COLLECTION.yaml": "name: starter\nresources:\n  - kind: skill\n    name: named\n",
				} {
					if harness == "factory" {
						switch name {
						case "agents/helper.md":
							name = "droids/helper.md"
						case ".mcp.json":
							name = "mcp.json"
						}
					}
					writeFixture(t, filepath.Join(root, name), content)
				}
				want, err := os.ReadFile(filepath.Join("testdata", "compatibility-"+version+".json"))
				if err != nil {
					t.Fatal(err)
				}
				want = bytes.TrimSpace(want)
				input := Input{SnapshotRoot: root, SourceHarness: harness, RevisionSchema: version}
				built, err := Build(input)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(built.Payload, want) {
					t.Fatalf("generator %s changed its compatibility vector\ngot: %s\nwant: %s", version, built.Payload, want)
				}
				var manifest sourceRevisionManifest
				if err := json.Unmarshal(want, &manifest); err != nil {
					t.Fatal(err)
				}
				locked := Projection{SourceHarness: harness, RevisionSchema: version, RevisionDigest: manifest.RevisionDigest, Resources: manifest.Resources[1:]}
				if _, err := Verify(Input{SnapshotRoot: root}, locked); err != nil {
					t.Fatalf("verify independently authored %s projection: %v", version, err)
				}
				writeFixture(t, filepath.Join(root, "hooks", "check.sh"), "echo changed\n")
				_, err = Verify(Input{SnapshotRoot: root}, locked)
				if version == "v1" && err != nil {
					t.Fatalf("v1 must retain its legacy skill-only support policy: %v", err)
				}
				if version == "v2" && !errors.Is(err, ErrManifestMismatch) {
					t.Fatalf("v2 hook-support tamper: %v", err)
				}
			})
		}
	}
}

func TestSourceRevisionRejectsUnknownVersionsAndMislabeledGenerators(t *testing.T) {
	for _, version := range []string{"", "v0", "v3"} {
		_, err := VerifyLockedGitRoot("not-opened", lockfile.GitRoot{RevisionSchema: version})
		var coded *lockfile.Error
		if !errors.As(err, &coded) || coded.Code != lockfile.CodeUnsupportedVersion {
			t.Fatalf("verify %q: %v", version, err)
		}
	}
	_, err := Build(Input{SnapshotRoot: "not-opened", RevisionSchema: "v3"})
	var coded *lockfile.Error
	if !errors.As(err, &coded) || coded.Code != lockfile.CodeUnsupportedVersion {
		t.Fatalf("build unknown version: %v", err)
	}
	_, _, err = encodeManifest(sourceRevisionManifest{SchemaVersion: "v1", Generator: "agent-marketplace source-revision/v2"})
	if !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("mislabeled generator: %v", err)
	}
}

func TestCurrentRevisionIncludesHookSupport(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./check.sh"}]}]}}`)
	writeFixture(t, filepath.Join(root, "hooks", "check.sh"), "echo checked\n")
	writeFixture(t, filepath.Join(root, "hooks", "nested", "ignored.sh"), "not emitted\n")
	built, err := Build(Input{SnapshotRoot: root, SourceHarness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Manifest.Resources) != 1 {
		t.Fatalf("resources = %+v", built.Manifest.Resources)
	}
	hook := built.Manifest.Resources[0]
	if hook.Kind != "hook" || len(hook.SupportFiles) != 1 || hook.SupportFiles["check.sh"] == "" {
		t.Fatalf("hook support projection = %+v", hook)
	}
	if built.Manifest.SchemaVersion != "v2" {
		t.Fatalf("corrected hook support requires v2, got %q", built.Manifest.SchemaVersion)
	}
	writeFixture(t, filepath.Join(root, "hooks", "check.sh"), "echo changed\n")
	changed, err := Build(Input{SnapshotRoot: root, SourceHarness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Manifest.Resources[0].ContentDigest != hook.ContentDigest || changed.Manifest.Resources[0].ArchiveDigest == hook.ArchiveDigest || changed.Manifest.RevisionDigest == built.Manifest.RevisionDigest {
		t.Fatal("hook-only support edit must change archive and revision, but not primary content digest")
	}
	if err := os.Remove(filepath.Join(root, "hooks", "check.sh")); err != nil {
		t.Fatal(err)
	}
	removed, err := Build(Input{SnapshotRoot: root, SourceHarness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Manifest.Resources[0].SupportFiles) != 0 || removed.Manifest.RevisionDigest == changed.Manifest.RevisionDigest {
		t.Fatal("removed hook support did not update the inventory")
	}
	if err := os.Symlink("nested/ignored.sh", filepath.Join(root, "hooks", "link.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(Input{SnapshotRoot: root, SourceHarness: "claude"}); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("v2 hook support symlink must fail closed: %v", err)
	}
}
