package sourcerevision

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
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

func TestBareBundleMCPRevisionCompatibility(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")
	writeFixture(t, filepath.Join(root, "commands", "hi.md"), "---\nname: hi\ndescription: Hi\n---\nHi\n")
	writeFixture(t, filepath.Join(root, "mcp.json"), `{"mcpServers":{"probe":{"command":"npx","args":["-y","x"]}}}`)
	var baseline map[string]string
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		built, err := Build(Input{SnapshotRoot: root, RevisionSchema: version})
		if err != nil {
			t.Fatalf("Build %s: %v", version, err)
		}
		if _, err := Verify(Input{SnapshotRoot: root}, built.Projection()); err != nil {
			t.Fatalf("Verify %s: %v", version, err)
		}
		digests := map[string]string{}
		hasMCP := false
		for _, resource := range built.Manifest.Resources {
			digests[resource.Kind+"/"+resource.Name] = resource.ContentDigest
			if resource.Kind == "mcpserver" && resource.Name == "probe" {
				hasMCP = true
			}
		}
		if version == "v4" {
			if !hasMCP {
				t.Fatalf("v4 resources = %+v, want probe MCP", built.Manifest.Resources)
			}
		} else if hasMCP {
			t.Fatalf("%s unexpectedly contains probe MCP: %+v", version, built.Manifest.Resources)
		}
		if digests["skill/demo"] == "" || digests["command/hi"] == "" {
			t.Fatalf("%s resources = %+v, want exact demo skill and hi command", version, built.Manifest.Resources)
		}
		delete(digests, "mcpserver/probe")
		if baseline == nil {
			baseline = digests
		} else if !maps.Equal(baseline, digests) {
			t.Fatalf("%s skill/command digests changed: baseline=%v got=%v", version, baseline, digests)
		}
	}
}

func TestUnconsumedContainerRevisionCompatibility(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, "group", ".factory-plugin", "plugin.json"), `{"name":"group"}`)
			writeFixture(t, filepath.Join(root, "group", "child", "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")

			baseline, err := Build(Input{SnapshotRoot: root, RevisionSchema: version})
			if err != nil {
				t.Fatalf("Build baseline: %v", err)
			}
			if len(baseline.Manifest.Resources) != 1 || baseline.Manifest.Resources[0].Kind != "skill" || baseline.Manifest.Resources[0].Name != "demo" || baseline.Manifest.Resources[0].ContentDigest == "" {
				t.Fatalf("baseline resources = %+v", baseline.Manifest.Resources)
			}

			writeFixture(t, filepath.Join(root, "group", ".mcp.json"), `{"mcpServers":{"foreign":{"command":"npx"}}}`)
			built, err := Build(Input{SnapshotRoot: root, RevisionSchema: version})
			if err != nil {
				t.Fatalf("Build with audit-only config: %v", err)
			}
			if !bytes.Equal(baseline.Payload, built.Payload) {
				t.Fatalf("audit-only config changed %s payload:\nbaseline: %s\nwith audit: %s", version, baseline.Payload, built.Payload)
			}
			if len(built.Manifest.Resources) != 1 || built.Manifest.Resources[0].Kind != "skill" || built.Manifest.Resources[0].Name != "demo" {
				t.Fatalf("resources = %+v", built.Manifest.Resources)
			}
			if _, err := Verify(Input{SnapshotRoot: root}, baseline.Projection()); err != nil {
				t.Fatalf("Verify baseline projection: %v", err)
			}
		})
	}
}

func TestSourceRevisionRejectsUnknownVersionsAndMislabeledGenerators(t *testing.T) {
	for _, version := range []string{"", "v0", "v999"} {
		_, err := VerifyLockedGitRoot("not-opened", lockfile.GitRoot{RevisionSchema: version})
		var coded *lockfile.Error
		if !errors.As(err, &coded) || coded.Code != lockfile.CodeUnsupportedVersion {
			t.Fatalf("verify %q: %v", version, err)
		}
	}
	_, err := Build(Input{SnapshotRoot: "not-opened", RevisionSchema: "v999"})
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
	if built.Manifest.SchemaVersion != "v4" {
		t.Fatalf("current revision requires v4, got %q", built.Manifest.SchemaVersion)
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
