package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: registry-symlink-containment/primary-ingestion-refuses-foreign-payload-aliases
func TestLoadPrimarySourceRefusesForeignPayloadAlias(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, name := range []string{"good", "foreign"} {
		if err := os.MkdirAll(filepath.Join(root, "skills", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	good := "---\nname: good\ndescription: good control\n---\n\ninert good body\n"
	if err := os.WriteFile(filepath.Join(root, "skills", "good", "SKILL.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(outside, "SKILL.md")
	data := []byte("---\nname: foreign\ndescription: foreign control\n---\n\nforeign inert body\n")
	if err := os.WriteFile(foreign, data, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(root, "skills", "foreign", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	cat, err := Load(root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	goodFound := false
	for _, entry := range cat.Entries {
		if entry.Name == "foreign" {
			t.Fatal("primary catalog ingestion imported foreign payload")
		}
		goodFound = goodFound || entry.Name == "good"
	}
	if !goodFound {
		t.Fatal("valid sibling primary ingestion positive control missing")
	}
	if len(cat.Diagnostics) == 0 {
		t.Fatal("primary refusal did not retain a diagnostic")
	}
	if _, err := cat.Discover(root, "claude"); !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("source refusal was downgraded during publication discovery: %v", err)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != string(data) {
		t.Fatalf("primary refusal changed external bytes: %q %v", got, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("primary refusal changed external metadata: %v", err)
	}
}

func TestDiscoverSourceRefusalPreservesIndependentSiblingPolicy(t *testing.T) {
	for _, shape := range []string{"root-load-error", "root-detection-refusal", "nested-load-error", "nested-diagnostic", "nested-detection-only"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			refused := root
			if shape != "root-load-error" && shape != "root-detection-refusal" {
				refused = filepath.Join(root, "refused")
			}
			writeDiscoveryFiles(t, root, map[string]string{"sibling/commands/good.md": "---\nname: good\ndescription: sibling control\n---\nGood\n"})
			primary := "hooks/hooks.json"
			content := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo inert"}]}]}}`
			if shape == "nested-diagnostic" {
				primary = "skills/bad/SKILL.md"
				content = "---\nname: bad\ndescription: refused control\n---\nBad\n"
				writeDiscoveryFiles(t, refused, map[string]string{"commands/survivor.md": "---\nname: survivor\ndescription: listing control\n---\nSurvivor\n"})
			}
			if shape == "root-detection-refusal" {
				primary = "SKILL.md"
				content = "---\nname: root\ndescription: detection control\n---\nRoot\n"
			}
			if shape == "nested-detection-only" {
				primary = ".factory-plugin/plugin.json"
				content = `{"name":"empty"}`
				writeDiscoveryFiles(t, refused, map[string]string{".claude/commands/note.txt": "not a resource\n"})
			}
			writeDiscoveryFiles(t, refused, map[string]string{primary: content})
			control, err := Discover(filepath.Join(root, "sibling"), "claude")
			if err != nil || len(control.Entries) != 1 || control.Entries[0].Name != "good" {
				t.Fatalf("sibling discovery positive control failed: %+v %v", control, err)
			}
			if err := os.Link(filepath.Join(refused, primary), filepath.Join(t.TempDir(), "retained-primary")); err != nil {
				t.Fatal(err)
			}
			if shape == "nested-detection-only" && loadSubBundle(refused, "claude").coveredBy(control.Entries, root) {
				t.Fatal("refused detection-only candidate was treated as covered")
			}
			loaded, err := Discover(root, "claude")
			if shape == "root-load-error" || shape == "root-detection-refusal" {
				if !errors.Is(err, fsboundary.ErrUnsafeFile) || len(loaded.Entries) != 0 {
					t.Fatalf("root refusal fell back to sub-bundles: %+v %v", loaded, err)
				}
				return
			}
			if err != nil || len(loaded.Entries) != 1 || loaded.Entries[0].Name != "good" || len(loaded.SubBundles) != 1 {
				t.Fatalf("refused sub-bundle leaked entries or lost sibling: %+v %v", loaded, err)
			}
			if len(loaded.Errors) != 1 || !errors.Is(loaded.Errors[0], fsboundary.ErrUnsafeFile) {
				t.Fatalf("typed sub-bundle refusal was discarded: %+v", loaded.Errors)
			}
		})
	}
}

// spec: registry-symlink-containment/contained-primary-file-aliases-retain-authored-content
func TestLoadPrimarySourceContainedAliasAndHardLink(t *testing.T) {
	for _, shape := range []string{"contained", "hardlink"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "skills", "alias")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(root, "authored.md")
			if err := os.WriteFile(payload, []byte("---\nname: alias\ndescription: authored control\n---\n\ninert authored body\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(payload)
			if err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(dir, "SKILL.md")
			if shape == "contained" {
				err = os.Symlink("../../authored.md", leaf)
			} else {
				err = os.Link(payload, leaf)
			}
			if err != nil {
				t.Fatal(err)
			}
			cat, err := Load(root, "claude")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range cat.Entries {
				found = found || entry.Name == "alias" && entry.Description == "authored control"
			}
			if found != (shape == "contained") {
				t.Fatalf("primary source shape accepted incorrectly: %s %+v %+v", shape, cat.Entries, cat.Diagnostics)
			}
			if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("primary source changed authored control: %v", err)
			}
		})
	}
}
