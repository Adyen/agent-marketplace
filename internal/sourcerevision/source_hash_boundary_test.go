package sourcerevision

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: registry-symlink-containment/source-directory-alias-reads-retain-captured-root-authority
func TestBuildUsesCapturedRootForIngestionAndDiscovery(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		for _, layout := range []string{"direct", "unrecognized", "vacant", "collections"} {
			for _, restore := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/restore_%v", version, layout, restore), func(t *testing.T) {
					parent := t.TempDir()
					root, replacement, retained := filepath.Join(parent, "selected"), filepath.Join(parent, "replacement"), filepath.Join(parent, "retained")
					primary := filepath.Join("commands", "shared.md")
					if layout == "unrecognized" || layout == "vacant" {
						primary = filepath.Join("team", "plugin", primary)
					}
					oldData := "---\nname: old-probe\ndescription: captured control\n---\nOld captured body\n"
					newData := "---\nname: new-probe\ndescription: replacement control\n---\nNew replacement body\n"
					writeFixture(t, filepath.Join(root, primary), oldData)
					writeFixture(t, filepath.Join(replacement, primary), newData)
					if layout == "vacant" {
						for _, dir := range []string{root, replacement} {
							writeFixture(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"container"}`)
						}
					}
					if layout == "collections" {
						writeFixture(t, filepath.Join(root, "collections", "pack", "COLLECTION.yaml"), "name: old-pack\nresources:\n  - kind: command\n    name: old-probe\n")
						writeFixture(t, filepath.Join(replacement, "collections", "pack", "COLLECTION.yaml"), "name: new-pack\nresources:\n  - kind: command\n    name: new-probe\n")
					}
					input := Input{SnapshotRoot: root, SourceHarness: "claude", RevisionSchema: version}
					control, err := Build(input)
					wantCount := 1
					if layout == "collections" {
						wantCount = 2
					}
					if err != nil || len(control.Manifest.Resources) != wantCount || len(control.Warnings) != 0 {
						t.Fatalf("captured root positive control failed: %+v %v", control.Manifest, err)
					}
					oldDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(oldData)))
					last := control.Manifest.Resources[len(control.Manifest.Resources)-1]
					if last.Name != "old-probe" || last.ContentDigest != oldDigest {
						t.Fatalf("old identity/raw-byte control failed: %+v", last)
					}
					captured := false
					input.beforeIngest = func(string) {
						captured = true
						if err := os.Rename(root, retained); err != nil {
							t.Fatal(err)
						}
						if err := os.Rename(replacement, root); err != nil {
							t.Fatal(err)
						}
					}
					if restore {
						input.beforeHash = func(string) {
							if err := os.Rename(root, replacement); err != nil {
								t.Fatal(err)
							}
							if err := os.Rename(retained, root); err != nil {
								t.Fatal(err)
							}
						}
					}
					built, err := Build(input)
					if !captured {
						t.Fatal("Build did not reach captured-root ingestion seam")
					}
					if err != nil {
						if !errors.Is(err, ErrCatalog) || !fsboundary.IsSourceRefusal(err) || len(built.Payload) != 0 || len(built.Manifest.Resources) != 0 || built.Catalog != nil {
							t.Fatalf("root replacement did not conservatively refuse: %+v %v", built.Manifest, err)
						}
					} else if !bytes.Equal(built.Payload, control.Payload) || len(built.Warnings) != 0 {
						t.Fatalf("Build mixed replacement identities with captured bytes: got %s, want %s", built.Payload, control.Payload)
					}
					oldRoot, newRoot := retained, root
					if restore {
						oldRoot, newRoot = root, replacement
					}
					for dir, want := range map[string]string{oldRoot: oldData, newRoot: newData} {
						data, err := os.ReadFile(filepath.Join(dir, primary))
						if err != nil || string(data) != want {
							t.Fatalf("revision changed source bytes: %q %v", data, err)
						}
					}
				})
			}
		}
	}
}

func TestBuildWindsurfRulesAreOmittedBeforeHashing(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			rule := filepath.Join(root, ".windsurf", "rules", "policy.mdc")
			writeFixture(t, rule, "---\ndescription: policy control\nalwaysApply: true\n---\nInert policy\n")
			cat, err := catalog.Load(root, "windsurf")
			if err != nil || len(cat.Entries) != 1 || cat.Entries[0].Kind != ir.KindRule || cat.Entries[0].Resource == nil {
				t.Fatalf("Windsurf rule native ingestion control failed: %+v %v", cat, err)
			}
			hit := false
			built, err := Build(Input{SnapshotRoot: root, SourceHarness: "windsurf", RevisionSchema: version, beforeHash: func(string) {
				hit = true
				if err := os.Remove(rule); err != nil {
					t.Fatal(err)
				}
			}})
			if !hit || err != nil || len(built.Manifest.Resources) != 0 || len(built.Warnings) != 1 || built.Warnings[0].Kind != "rule" || built.Warnings[0].Name != "policy" || built.Warnings[0].Reason != "unsupported resource kind" {
				t.Fatalf("unsupported Windsurf rule reached hashing: %+v %v", built.Manifest, err)
			}
		})
	}
}

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestBuildSourceHashRefusesPostIngestHardLink(t *testing.T) {
	snapshot := t.TempDir()
	primary := filepath.Join(snapshot, "skills", "hash-probe", "SKILL.md")
	writeFixture(t, primary, "---\nname: hash-probe\ndescription: hash control\n---\n\ninert primary body\n")
	before, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	result, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude", beforeHash: func(root string) {
		hit = true
		if err := os.Link(primary, filepath.Join(root, "retained-primary.md")); err != nil {
			t.Fatal(err)
		}
	}})
	if !hit {
		t.Fatal("source revision did not reach actual hash phase")
	}
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("unsafe hash payload did not refuse: %+v %v", result, err)
	}
	if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 {
		t.Fatalf("unsafe hash published a manifest: %+v", result)
	}
	if after, err := os.Stat(primary); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unsafe hash changed primary payload: %v", err)
	}
}

func TestBuildSourceHashRefusesPostIngestDisappearance(t *testing.T) {
	for _, version := range []string{"v1", "v2", "v3", "v4"} {
		for _, layout := range []string{"root", "nested"} {
			t.Run(version+"/"+layout, func(t *testing.T) {
				snapshot := t.TempDir()
				bundle := snapshot
				if layout == "nested" {
					bundle = filepath.Join(snapshot, "bundle")
				}
				primary := filepath.Join(bundle, "skills", "hash-probe", "SKILL.md")
				writeFixture(t, primary, "---\nname: hash-probe\ndescription: hash control\n---\n\ninert primary body\n")
				input := Input{SnapshotRoot: snapshot, SourceHarness: "claude", RevisionSchema: version}
				control, err := Build(input)
				if err != nil || len(control.Manifest.Resources) != 1 || control.Manifest.Resources[0].Name != "hash-probe" || len(control.Warnings) != 0 {
					t.Fatalf("primary hash positive control failed: %+v %v", control, err)
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
					t.Fatalf("admitted primary disappearance became fallback or omission: %v %+v %v", hit, result, err)
				}
				if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 || len(result.Warnings) != 0 {
					t.Fatalf("primary disappearance published partial revision: %+v", result)
				}
				if _, err := os.Stat(primary); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Build recreated removed primary: %v", err)
				}
			})
		}
	}
}

func TestBuildRefusesIngestionBoundaryWithValidSibling(t *testing.T) {
	for _, shape := range []string{"root-load-error", "root-detection-refusal", "nested-load-error", "nested-diagnostic", "nested-detection-only"} {
		t.Run(shape, func(t *testing.T) {
			snapshot := t.TempDir()
			refused := snapshot
			if shape != "root-load-error" && shape != "root-detection-refusal" {
				refused = filepath.Join(snapshot, "refused")
			}
			good := filepath.Join(snapshot, "sibling", "commands", "good.md")
			writeFixture(t, good, "---\nname: good\ndescription: sibling control\n---\n\ninert sibling body\n")
			primary := filepath.Join(refused, "hooks", "hooks.json")
			content := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo inert"}]}]}}`
			if shape == "nested-diagnostic" {
				primary = filepath.Join(refused, "skills", "bad", "SKILL.md")
				content = "---\nname: bad\ndescription: refused control\n---\n\ninert primary body\n"
				writeFixture(t, filepath.Join(refused, "commands", "survivor.md"), "---\nname: survivor\ndescription: retained listing control\n---\n\ninert survivor body\n")
			}
			if shape == "root-detection-refusal" {
				primary = filepath.Join(refused, "SKILL.md")
				content = "---\nname: root\ndescription: detection control\n---\n\ninert root body\n"
			}
			if shape == "nested-detection-only" {
				primary = filepath.Join(refused, ".factory-plugin", "plugin.json")
				content = `{"name":"empty"}`
				writeFixture(t, filepath.Join(refused, ".claude", "commands", "note.txt"), "not a resource\n")
			}
			writeFixture(t, primary, content)
			input := Input{SnapshotRoot: snapshot, SourceHarness: "claude"}
			control, err := Build(input)
			wantResources := 2
			switch shape {
			case "root-load-error", "root-detection-refusal", "nested-detection-only":
				wantResources = 1
			case "nested-diagnostic":
				wantResources = 3
			}
			if err != nil || len(control.Manifest.Resources) != wantResources || len(control.Warnings) != 0 {
				t.Fatalf("safe ingestion positive control failed: %+v %v", control, err)
			}
			sibling, err := Build(Input{SnapshotRoot: snapshot, RootPath: "sibling", SourceHarness: "claude"})
			if err != nil || len(sibling.Manifest.Resources) != 1 || sibling.Manifest.Resources[0].Name != "good" {
				t.Fatalf("independent sibling Build control failed: %+v %v", sibling, err)
			}
			before, err := os.Stat(primary)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Link(primary, filepath.Join(t.TempDir(), "retained-primary")); err != nil {
				t.Fatal(err)
			}
			cat, loadErr := catalog.Load(refused, "claude")
			if shape == "nested-diagnostic" || shape == "nested-detection-only" {
				if loadErr != nil || cat == nil || !errors.Is(cat.SourceRefusal(), fsboundary.ErrUnsafeFile) {
					t.Fatalf("collected refusal control failed: %+v %v", cat, loadErr)
				}
				if shape == "nested-diagnostic" && (len(cat.Entries) != 1 || cat.Entries[0].Name != "survivor") || shape == "nested-detection-only" && len(cat.Entries) != 0 {
					t.Fatalf("refused bundle listing control failed: %+v", cat.Entries)
				}
			} else if loadErr == nil || shape != "root-detection-refusal" && !errors.Is(loadErr, fsboundary.ErrUnsafeFile) {
				t.Fatalf("fixture did not reach expected load failure: %+v %v", cat, loadErr)
			}
			result, err := Build(input)
			if !errors.Is(err, ErrCatalog) || !errors.Is(err, fsboundary.ErrUnsafeFile) {
				t.Fatalf("typed ingestion refusal bypassed through valid sibling: %+v %v", result, err)
			}
			if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 || len(result.Warnings) != 0 {
				t.Fatalf("ingestion refusal published partial revision: %+v", result)
			}
			if data, err := os.ReadFile(primary); err != nil || string(data) != content {
				t.Fatalf("Build changed refused primary: %q %v", data, err)
			}
			if after, err := os.Stat(primary); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("Build changed refused primary metadata: %v", err)
			}
		})
	}
}

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestResourceSourceBytesPreservesLegacyFallbackBoundary(t *testing.T) {
	root := t.TempDir()
	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := source.Close(); err != nil {
			t.Fatal(err)
		}
	})
	resource := ir.NewCommand("legacy", "legacy control", "inert command", nil, ir.SourceRef{})
	for _, name := range []string{"", "absent.md", "missing/primary.md"} {
		t.Run(name, func(t *testing.T) {
			data, err := resourceSourceBytes(root, source, catalog.Entry{Kind: ir.KindCommand, Name: "legacy", Resource: resource, Path: name})
			if name != "" {
				if !errors.Is(err, os.ErrNotExist) || data != nil {
					t.Fatalf("missing disk source became canonical fallback: %s, %v", data, err)
				}
				return
			}
			want := `{"Description":"legacy control","Body":"inert command","Ext":{},"Extra":null,"Source":{"File":"","Line":0}}`
			if err != nil || string(data) != want {
				t.Fatalf("inline primary canonical fallback = %s, %v; want %s", data, err, want)
			}
		})
	}
}

func TestBuildSourceHashSupportRefusalIsNotOmission(t *testing.T) {
	snapshot := t.TempDir()
	primary := filepath.Join(snapshot, "skills", "hash-probe", "SKILL.md")
	writeFixture(t, primary, "---\nname: hash-probe\ndescription: hash control\n---\n\ninert primary body\n")
	support := filepath.Join(snapshot, "skills", "hash-probe", "reference.txt")
	writeFixture(t, support, "inert support data")
	control, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude"})
	if err != nil || len(control.Manifest.Resources) != 1 || len(control.Manifest.Resources[0].SupportFiles) != 1 {
		t.Fatalf("native revision support positive control failed: %+v %v", control, err)
	}
	hit := false
	result, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude", beforeHash: func(root string) {
		hit = true
		if err := os.Link(support, filepath.Join(root, "retained-support.txt")); err != nil {
			t.Fatal(err)
		}
	}})
	if !hit || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("unsafe support hash became omission: %v %+v %v", hit, result, err)
	}
	if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 {
		t.Fatalf("support refusal published partial revision: %+v", result)
	}
	if data, err := os.ReadFile(support); err != nil || string(data) != "inert support data" {
		t.Fatalf("support refusal changed payload: %q %v", data, err)
	}
}
