package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type localDetailFixture struct {
	registryRoot string
	skillName    string
	commandName  string
	relPaths     map[string]string
	markers      map[string]string
}

func writeLocalDetailFixture(t *testing.T, consumerRoot, harness, suffix string) localDetailFixture {
	t.Helper()
	fx := localDetailFixture{
		registryRoot: filepath.Join(consumerRoot, "registry"),
		skillName:    "local-skill-" + suffix,
		commandName:  "local-command-" + suffix,
		relPaths:     map[string]string{},
		markers:      map[string]string{},
	}
	prefix := ""
	if harness == "factory" {
		prefix = ".factory/"
	}
	fx.relPaths["skill"] = prefix + "skills/" + fx.skillName + "/SKILL.md"
	fx.relPaths["support"] = prefix + "skills/" + fx.skillName + "/support.txt"
	fx.relPaths["command"] = prefix + "commands/" + fx.commandName + ".md"
	fx.markers["skill"] = "SOURCE-SKILL-MARKER-" + suffix
	fx.markers["support"] = "SOURCE-SUPPORT-MARKER-" + suffix
	fx.markers["command"] = "SOURCE-COMMAND-MARKER-" + suffix

	files := map[string]string{
		fx.relPaths["skill"]:   "---\nname: " + fx.skillName + "\ndescription: local fixture\n---\n\n" + fx.markers["skill"] + "\n",
		fx.relPaths["support"]: fx.markers["support"] + "\n",
		fx.relPaths["command"]: "---\nname: " + fx.commandName + "\ndescription: local fixture\n---\n\n" + fx.markers["command"] + "\n",
	}
	for rel, content := range files {
		path := filepath.Join(fx.registryRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	decoy := "CONSUMER-DECOY-MARKER-" + suffix
	for _, rel := range fx.relPaths {
		path := filepath.Join(consumerRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(decoy+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inert := map[string]string{
		".agents/config.yaml": "apiVersion: agent-marketplace/v4\nsources: {}\n",
		".agents/.lock":       "INERT-LOCK-BYTES-" + suffix,
		".claude/skills/" + fx.skillName + "/SKILL.md": "INSTALLED-SENTINEL-" + suffix + "\n",
	}
	for rel, content := range inert {
		path := filepath.Join(consumerRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fx.markers["decoy"] = decoy
	return fx
}

func snapshotTreeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertTreeUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	after := snapshotTreeBytes(t, root)
	if len(after) != len(before) {
		var extra []string
		for rel := range after {
			if _, ok := before[rel]; !ok {
				extra = append(extra, rel)
			}
		}
		t.Fatalf("inspection changed file count: before=%d after=%d extra=%v", len(before), len(after), extra)
	}
	for rel, want := range before {
		if after[rel] != want {
			t.Fatalf("inspection changed %s", rel)
		}
	}
}

func loadLocalCatalog(t *testing.T, consumerRoot, url, harness string) (catalog.LoadResult, map[ir.ResourceKind]catalog.Entry) {
	t.Helper()
	loaded := catalog.LoadRegistries(&config.Config{
		Registries: []config.RegistryEntry{{URL: url, SourceHarness: harness}},
	}, consumerRoot, catalog.LoadModeInspect)
	if len(loaded.Errors) != 0 {
		t.Fatalf("LoadRegistries errors: %v", loaded.Errors)
	}
	entries := map[ir.ResourceKind]catalog.Entry{}
	for _, cat := range loaded.Catalogs {
		for _, entry := range cat.Entries {
			entries[entry.Kind] = entry
		}
	}
	for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand} {
		if _, ok := entries[kind]; !ok {
			t.Fatalf("missing %s entry in %+v", kind, loaded.Catalogs)
		}
	}
	return loaded, entries
}

// spec: tui-navigation/local-disk-resources-remain-browsable-outside-their-registry
// spec: tui-navigation/consumer-files-cannot-substitute-local-source-content
func TestLocalRegistryDetailBrowsingIsSourceAnchored(t *testing.T) {
	for _, tc := range []struct {
		name     string
		harness  string
		relative bool
	}{
		{name: "claude-absolute", harness: "claude"},
		{name: "claude-relative", harness: "claude", relative: true},
		{name: "factory-absolute", harness: "factory"},
		{name: "factory-relative", harness: "factory", relative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumerRoot := t.TempDir()
			fx := writeLocalDetailFixture(t, consumerRoot, tc.harness, tc.name)
			before := snapshotTreeBytes(t, consumerRoot)
			t.Chdir(consumerRoot)

			url := fx.registryRoot
			if tc.relative {
				url = "./registry"
			}
			loaded, entries := loadLocalCatalog(t, consumerRoot, url, tc.harness)

			for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand} {
				entry := entries[kind]
				r := row{Artifact: resolver.ResolvedArtifact{Entry: entry}}
				m := modelWithSelectedRow(t, r)
				m.width = 100
				m.height = 30
				m.enterDetailPane()
				if m.bundle == nil {
					t.Fatalf("%s did not open a bundle; viewport:\n%s", kind, m.detailViewport.View())
				}
				selected := m.detailSelectedPath
				if kind == ir.KindSkill && selected != "SKILL.md" {
					t.Fatalf("skill selected %q, want SKILL.md", selected)
				}
				if kind == ir.KindCommand && selected != filepath.Base(fx.relPaths["command"]) {
					t.Fatalf("command selected %q, want %s", selected, filepath.Base(fx.relPaths["command"]))
				}

				marker := fx.markers["skill"]
				if kind == ir.KindCommand {
					marker = fx.markers["command"]
				}
				preview := renderBundleEntry(context.Background(), newTheme(true), m.bundle, selected, 80)
				if !strings.Contains(preview, marker) {
					t.Fatalf("%s primary preview missing source marker:\n%s", kind, preview)
				}
				if strings.Contains(preview, fx.markers["decoy"]) {
					t.Fatalf("%s primary preview showed consumer decoy:\n%s", kind, preview)
				}

				if kind == ir.KindSkill {
					support := renderBundleEntry(context.Background(), newTheme(true), m.bundle, "support.txt", 80)
					if !strings.Contains(support, fx.markers["support"]) {
						t.Fatalf("support preview missing source marker:\n%s", support)
					}
					if strings.Contains(support, fx.markers["decoy"]) {
						t.Fatalf("support preview showed consumer decoy:\n%s", support)
					}
				}

				snapshots := captureSnapshotDetailSources(
					[]resolver.ResolvedArtifact{{Entry: entry}}, loaded.Catalogs,
				)
				snapshot := snapshots[entry.Path]
				if snapshot == nil || snapshot.err != nil || !strings.Contains(snapshot.content, marker) {
					t.Fatalf("%s snapshot = %+v, want marker %q", kind, snapshot, marker)
				}
				if strings.Contains(snapshot.content, fx.markers["decoy"]) {
					t.Fatalf("%s snapshot captured consumer decoy", kind)
				}
			}
			assertTreeUnchanged(t, consumerRoot, before)
		})
	}
}

// spec: tui-navigation/missing-or-unsafe-local-content-fails-read-only
func TestLocalRegistryDetailFailsReadOnlyAfterDiscovery(t *testing.T) {
	for _, shape := range []string{"disappeared", "hardlink"} {
		t.Run(shape, func(t *testing.T) {
			consumerRoot := t.TempDir()
			fx := writeLocalDetailFixture(t, consumerRoot, "claude", shape)
			before := snapshotTreeBytes(t, consumerRoot)
			t.Chdir(consumerRoot)

			loaded, entries := loadLocalCatalog(t, consumerRoot, fx.registryRoot, "claude")
			entry := entries[ir.KindSkill]
			primary := filepath.Join(fx.registryRoot, filepath.FromSlash(fx.relPaths["skill"]))

			var wantErr error
			primaryRel, err := filepath.Rel(consumerRoot, primary)
			if err != nil {
				t.Fatal(err)
			}
			primaryKey := filepath.ToSlash(primaryRel)
			external := filepath.Join(t.TempDir(), "unsafe-payload")
			var payload []byte
			var beforeMeta os.FileInfo
			if shape == "disappeared" {
				if err := os.Remove(primary); err != nil {
					t.Fatal(err)
				}
				wantErr = os.ErrNotExist
				delete(before, primaryKey)
			} else {
				payload = []byte("UNSAFE-PAYLOAD-MARKER-" + shape + "\n")
				if err := os.WriteFile(external, payload, 0o640); err != nil {
					t.Fatal(err)
				}
				var err error
				beforeMeta, err = os.Stat(external)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(primary); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(external, primary); err != nil {
					t.Fatal(err)
				}
				wantErr = fsboundary.ErrUnsafeFile
			}

			r := row{Artifact: resolver.ResolvedArtifact{Entry: entry}}
			m := modelWithSelectedRow(t, r)
			m.width = 100
			m.height = 30
			m.enterDetailPane()
			if shape == "disappeared" {
				if m.bundle != nil {
					t.Fatal("disappeared primary retained a bundle")
				}
			}
			viewport := m.detailViewport.View()
			if shape == "disappeared" && !strings.Contains(viewport, "Error reading resource") {
				t.Fatalf("missing primary did not fail visibly:\n%s", viewport)
			}

			if m.bundle != nil {
				out := renderBundleEntry(context.Background(), newTheme(true), m.bundle, "SKILL.md", 80)
				if !strings.Contains(out, "Error reading file") {
					t.Fatalf("unsafe preview did not fail visibly:\n%s", out)
				}
				if len(payload) > 0 && strings.Contains(out, string(payload)) {
					t.Fatalf("preview showed unsafe payload bytes:\n%s", out)
				}
				if strings.Contains(out, fx.markers["decoy"]) {
					t.Fatalf("preview showed decoy bytes:\n%s", out)
				}
			}

			snapshots := captureSnapshotDetailSources(
				[]resolver.ResolvedArtifact{{Entry: entry}}, loaded.Catalogs,
			)
			snapshot := snapshots[entry.Path]
			if snapshot == nil || !errors.Is(snapshot.err, wantErr) {
				t.Fatalf("snapshot error = %+v, want %v", snapshot, wantErr)
			}
			if snapshot.content != "" && strings.Contains(snapshot.content, fx.markers["decoy"]) {
				t.Fatal("snapshot captured substituted decoy bytes")
			}
			if len(payload) > 0 && strings.Contains(snapshot.content, string(payload)) {
				t.Fatal("snapshot captured unsafe payload bytes")
			}

			if shape == "hardlink" {
				if data, err := os.ReadFile(external); err != nil || string(data) != string(payload) {
					t.Fatalf("inspection changed external payload: %q %v", data, err)
				}
				if after, err := os.Stat(external); err != nil || !os.SameFile(beforeMeta, after) || !beforeMeta.ModTime().Equal(after.ModTime()) {
					t.Fatalf("inspection changed external payload metadata: %v", err)
				}
				before[primaryKey] = string(payload)
			}
			assertTreeUnchanged(t, consumerRoot, before)
		})
	}
}
