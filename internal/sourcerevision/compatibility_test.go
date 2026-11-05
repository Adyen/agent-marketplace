package sourcerevision

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestCursorWorkspaceRevisionCompatibility(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			root, files := cursorRevisionFixture(t, ".cursor")
			cat, err := catalog.Load(root, "cursor")
			if err != nil || len(cat.Entries) != len(files) || cat.SourceRefusal() != nil {
				t.Fatalf("Cursor workspace native ingestion control failed: %+v %v", cat, err)
			}
			for _, entry := range cat.Entries {
				rel, err := filepath.Rel(root, entry.Path)
				if err != nil || entry.Resource == nil || files[filepath.ToSlash(rel)] == "" {
					t.Fatalf("Cursor authored source spelling changed: %+v %v", entry, err)
				}
			}
			input := Input{SnapshotRoot: root, SourceHarness: "cursor", RevisionSchema: version}
			built, err := Build(input)
			if err != nil {
				t.Fatal(err)
			}
			unsigned := fmt.Sprintf(`{"schema_version":%q,"generator":%q,"resources":null}`, version, "agent-marketplace source-revision/"+version)
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(unsigned)))
			want := fmt.Sprintf(`{"schema_version":%q,"generator":%q,"resources":null,"revision_digest":%q}`, version, "agent-marketplace source-revision/"+version, digest)
			if string(built.Payload) != want || len(built.Manifest.Resources) != 0 || len(built.Projection().Resources) != 0 {
				t.Fatalf("Cursor %s changed historical empty inventory/bytes: got %s, want %s", version, built.Payload, want)
			}
			wantWarnings := []struct {
				resource string
				file     string
				absent   string
			}{
				{"command/review", "commands/review.md", "commands"},
				{"hook/PreToolUse-command", "hooks/hooks.json", "hooks"},
				{"mcpserver/probe", "mcp.json", "mcp.json"},
				{"skill/cursor-probe", "skills/cursor-probe/SKILL.md", "skills"},
			}
			if len(built.Warnings) != len(wantWarnings) {
				t.Fatalf("Cursor historical omission warnings = %+v, want %v", built.Warnings, wantWarnings)
			}
			for i, warning := range built.Warnings {
				_, missingErr := os.Lstat(filepath.Join(root, wantWarnings[i].absent))
				if !errors.Is(missingErr, os.ErrNotExist) {
					t.Fatalf("legacy source-path absence control failed: %v", missingErr)
				}
				reason := fmt.Sprintf("resource could not be built: resolve %q: %v", filepath.Join(root, filepath.FromSlash(wantWarnings[i].file)), missingErr)
				if warning.Resource() != wantWarnings[i].resource || warning.Reason != reason {
					t.Fatalf("Cursor warning[%d] = %+v, want %s: %s", i, warning, wantWarnings[i].resource, reason)
				}
			}
			if _, err := Verify(input, Projection{SourceHarness: "cursor", RevisionSchema: version, RevisionDigest: digest, Resources: []revisionResource{}}); err != nil {
				t.Fatalf("Cursor independently authored historical projection failed verification: %v", err)
			}
		})
	}
}

// spec: registry-symlink-containment/contained-primary-file-aliases-retain-authored-content
func TestCursorWorkspaceContainedPrimaryAliasRetainsLegacyOmission(t *testing.T) {
	root, files := cursorRevisionFixture(t, ".cursor")
	primary := filepath.Join(root, ".cursor", "skills", "cursor-probe", "SKILL.md")
	authored := filepath.Join(root, "authored-cursor-skill.md")
	if err := os.Rename(primary, authored); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../authored-cursor-skill.md", primary); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(authored)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(root, "cursor")
	if err != nil || len(cat.Entries) != 4 || cat.SourceRefusal() != nil {
		t.Fatalf("contained Cursor primary alias native admission control failed: %+v %v", cat, err)
	}
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		built, err := Build(Input{SnapshotRoot: root, SourceHarness: "cursor", RevisionSchema: version})
		if err != nil || len(built.Manifest.Resources) != 0 || len(built.Warnings) != 4 {
			t.Fatalf("contained Cursor alias changed historical %s omission: %+v %v", version, built, err)
		}
	}
	if data, err := os.ReadFile(authored); err != nil || string(data) != files["skills/cursor-probe/SKILL.md"] {
		t.Fatalf("Cursor alias validation changed authored bytes: %q %v", data, err)
	}
	if after, err := os.Stat(authored); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("Cursor alias validation changed authored metadata: %v", err)
	}
}

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestCursorBareRevisionKeepsRawPrimaryHashes(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			root, files := cursorRevisionFixture(t, "")
			built, err := Build(Input{SnapshotRoot: root, SourceHarness: "cursor", RevisionSchema: version})
			if err != nil || len(built.Manifest.Resources) != 4 || len(built.Warnings) != 0 {
				t.Fatalf("Cursor bare revision control failed: %+v %v", built, err)
			}
			var records []string
			for _, resource := range []struct{ kind, name, file string }{
				{"command", "review", "commands/review.md"},
				{"hook", "PreToolUse-command", "hooks/hooks.json"},
				{"mcpserver", "probe", "mcp.json"},
				{"skill", "cursor-probe", "skills/cursor-probe/SKILL.md"},
			} {
				digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(files[resource.file])))
				records = append(records, fmt.Sprintf(`{"kind":%q,"name":%q,"content_digest":%q,"archive_digest":%q}`, resource.kind, resource.name, digest, digest))
			}
			unsigned := fmt.Sprintf(`{"schema_version":%q,"generator":%q,"resources":[%s]}`, version, "agent-marketplace source-revision/"+version, strings.Join(records, ","))
			want := strings.TrimSuffix(unsigned, "}") + fmt.Sprintf(`,"revision_digest":"sha256:%x"}`, sha256.Sum256([]byte(unsigned)))
			if string(built.Payload) != want {
				t.Fatalf("Cursor %s changed bare raw-payload revision bytes: got %s, want %s", version, built.Payload, want)
			}
			if _, err := Verify(Input{SnapshotRoot: root}, built.Projection()); err != nil {
				t.Fatalf("Cursor bare projection did not verify: %v", err)
			}
		})
	}
}

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestCursorWorkspaceAdmittedPrimaryDisappearanceRefusesRevision(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		for _, name := range []string{"skills/cursor-probe/SKILL.md", "commands/review.md", "hooks/hooks.json", "mcp.json"} {
			t.Run(version+"/"+name, func(t *testing.T) {
				root, _ := cursorRevisionFixture(t, ".cursor")
				primary := filepath.Join(root, ".cursor", filepath.FromSlash(name))
				input := Input{SnapshotRoot: root, SourceHarness: "cursor", RevisionSchema: version}
				control, err := Build(input)
				if err != nil || control.Catalog == nil || len(control.Catalog.Entries) != 4 {
					t.Fatalf("actual Cursor admission positive control failed: %+v %v", control, err)
				}
				hit := false
				input.beforeHash = func(string) {
					hit = true
					if err := os.Remove(primary); err != nil {
						t.Fatal(err)
					}
				}
				result, err := Build(input)
				if !hit || !errors.Is(err, ErrCatalog) || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("actual admitted Cursor primary disappeared without refusal: %+v %v", result, err)
				}
				if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 || len(result.Warnings) != 0 {
					t.Fatalf("Cursor disappearance published partial revision: %+v", result)
				}
				if _, err := os.Stat(primary); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Build recreated actual Cursor primary: %v", err)
				}
			})
		}
	}
}

func cursorRevisionFixture(t *testing.T, prefix string) (string, map[string]string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"skills/cursor-probe/SKILL.md": "---\nname: cursor-probe\ndescription: Cursor control\n---\nInert skill\n",
		"commands/review.md":           "---\nname: review\ndescription: Review control\n---\nInert review\n",
		"hooks/hooks.json":             `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo inert"}]}]}}`,
		"mcp.json":                     `{"mcpServers":{"probe":{"command":"inert"}}}`,
	}
	for name, data := range files {
		writeFixture(t, filepath.Join(root, prefix, filepath.FromSlash(name)), data)
	}
	return root, files
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
		t.Fatalf("current hook support and model provenance require v4, got %q", built.Manifest.SchemaVersion)
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
