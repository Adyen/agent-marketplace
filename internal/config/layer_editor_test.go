package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func layerEditorWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
}

func openTestEditor(t *testing.T, source Source, staged, declaring string) *LayerEditor {
	t.Helper()
	editor, err := OpenLayerEditor(LayerTarget{Source: source, StagedPath: staged, DeclaringPath: declaring})
	if err != nil {
		t.Fatalf("OpenLayerEditor: %v", err)
	}
	return editor
}

func layerEditorErrCode(err error) string {
	var validation *V4ValidationError
	if errors.As(err, &validation) {
		return validation.Code
	}
	return ""
}

func loc(kind ir.ResourceKind, name string) ResourceLocator {
	return ResourceLocator{Kind: kind, Name: name}
}

func TestLayerEditorSchemaTable(t *testing.T) {
	entries := []struct {
		name, fixture string
	}{
		{name: "mapping", fixture: "apiVersion: agent-marketplace/v4\n# team comment\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/keep]}\n"},
		{name: "shorthand", fixture: "apiVersion: agent-marketplace/v4\n# team comment\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/keep]\n"},
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	for _, tc := range entries {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, tc.fixture)
			assertIntent := func(data []byte, wantInclude, wantExclude []string) {
				t.Helper()
				cfg, err := ParseV4Config(data, path, true)
				if err != nil {
					t.Fatalf("candidate no longer valid v4: %v", err)
				}
				group := cfg.Groups["one"]
				var includes []string
				for _, r := range group.Include {
					includes = append(includes, r.Locator)
				}
				if !slices.Equal(includes, wantInclude) || !slices.Equal(group.Exclude, wantExclude) {
					t.Fatalf("include/exclude = %v/%v, want %v/%v", includes, group.Exclude, wantInclude, wantExclude)
				}
			}
			editor := openTestEditor(t, RepoCommitted, path, path)
			binding, err := editor.EnsureRegistry(entry)
			if err != nil || binding.Name != "one" {
				t.Fatalf("EnsureRegistry binding = %#v, %v", binding, err)
			}
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("include: %v", err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !candidate.Changed || candidate.Mode.Perm() != 0o640 {
				t.Fatalf("candidate = %#v", candidate)
			}
			if !strings.HasPrefix(string(candidate.Bytes), "apiVersion: agent-marketplace/v4\n") && !strings.Contains(string(candidate.Bytes), "\napiVersion: agent-marketplace/v4\n") {
				t.Fatalf("schema version changed: %s", candidate.Bytes)
			}
			if !strings.Contains(string(candidate.Bytes), "# team comment") {
				t.Fatalf("comment lost:\n%s", candidate.Bytes)
			}
			assertIntent(candidate.Bytes, []string{"skill/keep", "skill/alpha"}, nil)
			layerEditorWrite(t, path, string(candidate.Bytes))
			editor = openTestEditor(t, RepoCommitted, path, path)
			binding, err = editor.EnsureRegistry(entry)
			if err != nil {
				t.Fatalf("re-ensure: %v", err)
			}
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("reapply include: %v", err)
			}
			noop, err := editor.Render()
			if err != nil || noop.Changed || string(noop.Bytes) != string(candidate.Bytes) {
				t.Fatalf("idempotent include changed bytes: %v", err)
			}
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("exclude: %v", err)
			}
			excluded, err := editor.Render()
			if err != nil {
				t.Fatalf("exclude render: %v", err)
			}
			assertIntent(excluded.Bytes, []string{"skill/keep"}, []string{"skill/alpha"})
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("re-enable: %v", err)
			}
			enabled, err := editor.Render()
			if err != nil {
				t.Fatalf("re-enable render: %v", err)
			}
			assertIntent(enabled.Bytes, []string{"skill/keep", "skill/alpha"}, nil)
		})
	}
}

func TestLayerEditorRefusesUnsupportedSchemas(t *testing.T) {
	for _, tc := range []struct{ name, fixture, code string }{
		{name: "v2", fixture: "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    skills: {include: [keep]}\n", code: "v4.api-version"},
		{name: "v3", fixture: "apiVersion: agent-marketplace/v3\nregistries: []\n", code: "v4.api-version"},
		{name: "versionless", fixture: "sources:\n  one: {git: https://example.test/repo.git, ref: main}\n", code: "v4.api-version-required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, tc.fixture)
			_, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code || !strings.Contains(validation.Path, path) || !strings.Contains(validation.Msg, "apiVersion") {
				t.Fatalf("open = %v, want %s", err, tc.code)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != tc.fixture {
				t.Fatalf("refusal changed bytes: %s, %v", data, err)
			}
		})
	}
}

func TestLayerEditorV4GitIdentityAndAmbiguity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  mainSkills: {git: https://example.test/repo.git, ref: main, path: skills}\n  devSkills: {git: https://example.test/repo.git, ref: dev, path: skills}\n  mainCommands: {git: https://example.test/repo.git, ref: main, path: commands}\n")
	editor := openTestEditor(t, RepoCommitted, path, path)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if binding.Name != "mainSkills" || !binding.root {
		t.Fatalf("binding = %#v", binding)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed {
		t.Fatal("ensure existing source changed bytes")
	}

	ambiguous := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, ambiguous, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n  two: {git: https://example.test/repo.git, ref: main}\n")
	editor = openTestEditor(t, RepoCommitted, ambiguous, ambiguous)
	if _, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}); layerEditorErrCode(err) != "config.edit.ambiguous-source" {
		t.Fatalf("ambiguous error = %v", err)
	}
	candidate, err = editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed {
		t.Fatal("ambiguous ensure changed bytes")
	}
}

func TestLayerEditorV4IncludedSourceRootOverrideAndIncludeOwned(t *testing.T) {
	dir := t.TempDir()
	declaring := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(dir, "fragment.yaml")
	fragmentData := "sources:\n  local: {git: https://example.test/repo.git, ref: main}\nresources:\n  local: [skill/alpha]\n"
	layerEditorWrite(t, fragment, fragmentData)
	rootData := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n"
	layerEditorWrite(t, declaring, rootData)
	staged := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, staged, rootData)

	editor := openTestEditor(t, RepoCommitted, staged, declaring)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if binding.Name != "local" || binding.root {
		t.Fatalf("binding = %#v", binding)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "beta")}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || !strings.Contains(string(candidate.Bytes), "skill/beta") {
		t.Fatalf("root override missing:\n%s", candidate.Bytes)
	}
	if got, err := os.ReadFile(fragment); err != nil || string(got) != fragmentData {
		t.Fatalf("fragment changed: %v", err)
	}
	if err := editor.RemoveSource(binding); layerEditorErrCode(err) != "config.edit.include-owned" {
		t.Fatalf("remove include-owned = %v", err)
	}
}

func TestLayerEditorV4RemoveIncludeOwnership(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}

	t.Run("fragment group refusal is byte-stable", func(t *testing.T) {
		dir := t.TempDir()
		declaring := filepath.Join(dir, "config.yaml")
		fragment := filepath.Join(dir, "fragment.yaml")
		fragmentData := "resources:\n  main: [skill/gone]\n"
		layerEditorWrite(t, fragment, fragmentData)
		rootData := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\n"
		layerEditorWrite(t, declaring, rootData)
		staged := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, staged, rootData)

		editor := openTestEditor(t, RepoCommitted, staged, declaring)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		err = editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "gone")}})
		if layerEditorErrCode(err) != "config.edit.include-owned" {
			t.Fatalf("remove = %v, want include-owned", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != rootData {
			t.Fatalf("refusal changed bytes:\n%s", candidate.Bytes)
		}
	})

	t.Run("root and fragment provenance refuses", func(t *testing.T) {
		dir := t.TempDir()
		declaring := filepath.Join(dir, "config.yaml")
		fragment := filepath.Join(dir, "fragment.yaml")
		fragmentData := "resources:\n  main: [skill/gone]\n"
		layerEditorWrite(t, fragment, fragmentData)
		rootData := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\nresources:\n  main: [skill/gone]\n"
		layerEditorWrite(t, declaring, rootData)
		staged := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, staged, rootData)

		editor := openTestEditor(t, RepoCommitted, staged, declaring)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		err = editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "gone")}})
		if layerEditorErrCode(err) != "config.edit.include-owned" {
			t.Fatalf("remove = %v, want include-owned", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != rootData {
			t.Fatalf("refusal changed bytes:\n%s", candidate.Bytes)
		}
		gotFragment, err := os.ReadFile(fragment)
		if err != nil || string(gotFragment) != fragmentData {
			t.Fatalf("fragment changed:\n%s", gotFragment)
		}
	})

	t.Run("absent locator is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		data := "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\nresources:\n  main: [skill/keep]\n"
		layerEditorWrite(t, path, data)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "gone")}}); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != data {
			t.Fatalf("absent removal changed bytes:\n%s", candidate.Bytes)
		}
	})
}

func TestLayerEditorV4DirectorySourceStagingAndUnsafe(t *testing.T) {
	repo := t.TempDir()
	declaring := filepath.Join(repo, ".agents", "config.yaml")
	layerEditorWrite(t, declaring, "apiVersion: agent-marketplace/v4\n")
	staged := filepath.Join(t.TempDir(), "staged-config.yaml")
	layerEditorWrite(t, staged, "apiVersion: agent-marketplace/v4\n")
	inside := filepath.Join(repo, "inside-target")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(repo, "inside")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}

	editor := openTestEditor(t, RepoCommitted, staged, declaring)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "./inside", V4Kind: V4SourceDirectory})
	if err != nil {
		t.Fatalf("ensure inside: %v", err)
	}
	if binding.Entry.V4Kind != V4SourceDirectory || binding.Entry.URL != filepath.Join(repo, "inside") {
		t.Fatalf("directory binding entry = %#v", binding.Entry)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || !strings.Contains(string(candidate.Bytes), "directory: ./inside") {
		t.Fatalf("directory source not bound to declaring root:\n%s", candidate.Bytes)
	}
	if strings.Contains(string(candidate.Bytes), filepath.Dir(staged)) || strings.Contains(string(candidate.Bytes), repo) {
		t.Fatalf("directory source leaked a staged or absolute path:\n%s", candidate.Bytes)
	}

	sub := filepath.Join(inside, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	editor = openTestEditor(t, RepoCommitted, staged, declaring)
	binding, err = editor.EnsureRegistry(RegistryEntry{URL: "./inside", Path: "sub", V4Kind: V4SourceDirectory})
	if err != nil {
		t.Fatalf("ensure inside/sub: %v", err)
	}
	if binding.Entry.URL != filepath.Join(repo, "inside", "sub") {
		t.Fatalf("directory binding entry = %#v", binding.Entry)
	}
	candidate, err = editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || !strings.Contains(string(candidate.Bytes), "directory: ./inside/sub") {
		t.Fatalf("directory source path not retained:\n%s", candidate.Bytes)
	}
	if strings.Contains(string(candidate.Bytes), repo) {
		t.Fatalf("directory source leaked an absolute path:\n%s", candidate.Bytes)
	}

	editor = openTestEditor(t, RepoCommitted, staged, declaring)
	if _, err := editor.EnsureRegistry(RegistryEntry{URL: "./escape", V4Kind: V4SourceDirectory}); layerEditorErrCode(err) != "config.edit.unsafe-directory" {
		t.Fatalf("escape error = %v", err)
	}
	candidate, err = editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed {
		t.Fatal("unsafe directory changed bytes")
	}
	if _, err := editor.EnsureRegistry(RegistryEntry{URL: "../outside", V4Kind: V4SourceDirectory}); layerEditorErrCode(err) != "config.edit.unsafe-directory" {
		t.Fatalf("lexical escape error = %v", err)
	}
}

func TestLayerEditorSetLayerExclude(t *testing.T) {
	t.Run("v4 writes exclusions under every provider and dedupes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		entries := []RegistryEntry{
			{URL: "https://example.test/one.git", Ref: "main", V4Kind: V4SourceGit},
			{URL: "https://example.test/two.git", Ref: "main", V4Kind: V4SourceGit},
			{URL: "https://example.test/one.git", Ref: "main", V4Kind: V4SourceGit},
		}
		if err := editor.SetLayerExclude(loc(ir.KindCollection, "bundle"), entries); err != nil {
			t.Fatalf("set layer exclude: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		layerEditorWrite(t, path, string(candidate.Bytes))
		cfg, err := LoadConfigFromPath(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if len(cfg.Registries) != 2 {
			t.Fatalf("expected two deduped sources, got %d:\n%s", len(cfg.Registries), candidate.Bytes)
		}
		for _, entry := range cfg.Registries {
			if !slices.Contains(entry.Collections.Exclude, "bundle") {
				t.Fatalf("missing source-bound collection exclusion on %#v:\n%s", entry, candidate.Bytes)
			}
		}
	})

	t.Run("v4 without providers refuses without mutation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		err := editor.SetLayerExclude(loc(ir.KindCollection, "bundle"), nil)
		if layerEditorErrCode(err) != "config.edit.unsupported-operation" {
			t.Fatalf("error = %v, want unsupported-operation", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("refusal mutated bytes:\n%s", candidate.Bytes)
		}
	})

	t.Run("v4 rejects a marketplace provider before mutation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		entries := []RegistryEntry{
			{URL: "https://example.test/one.git", Ref: "main", V4Kind: V4SourceGit},
			{URL: "https://mp.example.test", V4Kind: V4SourceMarketplace},
		}
		if err := editor.SetLayerExclude(loc(ir.KindCollection, "bundle"), entries); err == nil {
			t.Fatal("marketplace provider accepted")
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("partial mutation after invalid provider:\n%s", candidate.Bytes)
		}
	})

	t.Run("source-bound exclusion preserves another kind", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/one.git, ref: main}\nresources:\n  one: {include: [command/audit]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		entries := []RegistryEntry{{URL: "https://example.test/one.git", Ref: "main", V4Kind: V4SourceGit}}
		if err := editor.SetLayerExclude(loc(ir.KindCollection, "bundle"), entries); err != nil {
			t.Fatalf("set layer exclude: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		group := cfg.Groups["one"]
		if !slices.Equal(group.Exclude, []string{"collection/bundle"}) || len(group.Include) != 1 || group.Include[0].Locator != "command/audit" {
			t.Fatalf("exclusion or sibling selection = %#v\n%s", group, candidate.Bytes)
		}
	})
}

func TestLayerEditorV4KindDistinctGitFileURL(t *testing.T) {
	repo := t.TempDir()
	declaring := filepath.Join(repo, ".agents", "config.yaml")
	dirPath := filepath.Join(t.TempDir(), "registry.git")
	layerEditorWrite(t, declaring, "apiVersion: agent-marketplace/v4\nsources:\n  dirsrc: {directory: "+filepath.ToSlash(dirPath)+"}\n")

	editor := openTestEditor(t, RepoCommitted, declaring, declaring)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "file://" + filepath.ToSlash(dirPath), Ref: "main", V4Kind: V4SourceGit})
	if err != nil {
		t.Fatalf("ensure git file URL: %v", err)
	}
	if binding.Name == "dirsrc" {
		t.Fatal("git file:// entry bound the directory source")
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	layerEditorWrite(t, declaring, string(candidate.Bytes))
	cfg, err := LoadConfigFromPath(declaring)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var dirs, gits int
	for _, entry := range cfg.Registries {
		switch entry.V4Kind {
		case V4SourceDirectory:
			dirs++
		case V4SourceGit:
			gits++
		}
	}
	if dirs != 1 || gits != 1 {
		t.Fatalf("expected one directory and one git source, got %d directory %d git:\n%s", dirs, gits, candidate.Bytes)
	}
}

func TestLayerEditorV4SelectionOperations(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	base := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one:\n    include:\n      - skill/keep: {as: keepname}\n      - skill/go\n    exclude: [skill/ex]\n    tags: [t1]\n    required: true\n    harnesses: [factory]\n"
	renderGroup := func(t *testing.T, editor *LayerEditor) V4ResourceGroup {
		t.Helper()
		candidate, err := editor.Render()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		cfg, err := ParseV4Config(candidate.Bytes, "config.yaml", true)
		if err != nil {
			t.Fatalf("candidate invalid: %v\n%s", err, candidate.Bytes)
		}
		return cfg.Groups["one"]
	}
	groupLocators := func(g V4ResourceGroup) []string {
		var out []string
		for _, r := range g.Include {
			out = append(out, r.Locator)
		}
		return out
	}

	t.Run("include appends scalar and preserves options", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "new")}}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		got := groupLocators(group)
		if len(got) != 3 || got[0] != "skill/keep" || got[1] != "skill/go" || got[2] != "skill/new" {
			t.Fatalf("include = %v", got)
		}
		if !group.Include[0].Options.HasAs || group.Include[0].Options.As != "keepname" {
			t.Fatalf("locator options lost: %#v", group.Include[0])
		}
		if group.Required == nil || !*group.Required || !group.HasHarnesses || len(group.Exclude) != 1 {
			t.Fatalf("unrelated group fields changed: %#v", group)
		}
	})

	t.Run("include rejects wildcard exclusion", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/*]}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); layerEditorErrCode(err) != "v4.edit-exclusion" {
			t.Fatalf("wildcard error = %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed {
			t.Fatal("rejected include changed bytes")
		}
	})

	t.Run("exclude removes options locator and appends exclusion", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "keep"), loc(ir.KindSkill, "go")}}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if len(group.Include) != 0 {
			t.Fatalf("include retained: %v", groupLocators(group))
		}
		if len(group.Exclude) != 3 {
			t.Fatalf("exclude = %v", group.Exclude)
		}
	})

	t.Run("exclude shorthand preserves sibling include", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/keep, skill/go]\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "go")}}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if got := groupLocators(group); !slices.Equal(got, []string{"skill/keep"}) {
			t.Fatalf("include = %v", got)
		}
		if !slices.Equal(group.Exclude, []string{"skill/go"}) {
			t.Fatalf("exclude = %v", group.Exclude)
		}
	})

	t.Run("remove include does not add exclusion", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "go")}}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if len(group.Include) != 1 || group.Include[0].Locator != "skill/keep" {
			t.Fatalf("include = %v", groupLocators(group))
		}
		if len(group.Exclude) != 1 || group.Exclude[0] != "skill/ex" {
			t.Fatalf("exclude = %v", group.Exclude)
		}
	})

	t.Run("clear exclude removes exact and refuses suppressed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionClearExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "ex")}}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if len(group.Exclude) != 0 {
			t.Fatalf("exclude = %v", group.Exclude)
		}

		wild := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, wild, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/*, skill/ex]}\n")
		editor = openTestEditor(t, RepoCommitted, wild, wild)
		binding, err = editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionClearExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "ex")}}); layerEditorErrCode(err) != "v4.edit-exclusion" {
			t.Fatalf("suppressed clear = %v", err)
		}
	})

	t.Run("select all sets all and drops star exclusion", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/keep], exclude: ['*', skill/ex], tags: [t1], required: true}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionSelectAll}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if group.All == nil || !*group.All {
			t.Fatalf("all = %#v", group.All)
		}
		if len(group.Exclude) != 1 || group.Exclude[0] != "skill/ex" {
			t.Fatalf("exclude = %v", group.Exclude)
		}
		if len(group.Include) != 1 || !group.HasTags || group.Required == nil {
			t.Fatalf("selection fields lost: %#v", group)
		}
	})

	t.Run("exclude all clears selection fields", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/keep], tags: [t1], all: true, required: true, harnesses: [factory]}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExcludeAll}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if len(group.Exclude) != 1 || group.Exclude[0] != "*" {
			t.Fatalf("exclude = %v", group.Exclude)
		}
		if len(group.Include) != 0 || group.HasTags || group.HasAll {
			t.Fatalf("selection fields retained: %#v", group)
		}
		if group.Required == nil || !*group.Required || !group.HasHarnesses {
			t.Fatalf("non-selection fields lost: %#v", group)
		}
	})

	t.Run("tag add", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionAddTag, Tag: "t2"}); err != nil {
			t.Fatal(err)
		}
		group := renderGroup(t, editor)
		if len(group.Tags) != 2 || group.Tags[0] != "t1" || group.Tags[1] != "t2" {
			t.Fatalf("tags = %v", group.Tags)
		}
	})
}

func TestLayerEditorRuntimeSelectionOperations(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	renderEntry := func(t *testing.T, editor *LayerEditor, path string) RegistryEntry {
		t.Helper()
		candidate, err := editor.Render()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		v4, err := ParseV4Config(candidate.Bytes, path, true)
		if err != nil {
			t.Fatalf("candidate invalid: %v\n%s", err, candidate.Bytes)
		}
		cfg, err := v4.ToLegacyConfig(repoLayer)
		if err != nil {
			t.Fatalf("runtime adapter: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("registries = %#v", cfg.Registries)
		}
		return cfg.Registries[0]
	}
	base := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/keep, skill/go, command/c1], exclude: [skill/ex], tags: [t1]}\n"

	t.Run("include exclude remove and clear", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "new")}}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if !stringSliceContains(e.Skills.Include, "new") {
			t.Fatalf("include = %v", e.Skills.Include)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "go")}}); err != nil {
			t.Fatal(err)
		}
		e = renderEntry(t, editor, path)
		if stringSliceContains(e.Skills.Include, "go") || !stringSliceContains(e.Skills.Exclude, "go") {
			t.Fatalf("exclude = %#v", e.Skills)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionClearExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "ex")}}); err != nil {
			t.Fatal(err)
		}
		e = renderEntry(t, editor, path)
		if stringSliceContains(e.Skills.Exclude, "ex") || !stringSliceContains(e.Skills.Exclude, "go") {
			t.Fatalf("clear exclude = %#v", e.Skills)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "keep")}}); err != nil {
			t.Fatal(err)
		}
		e = renderEntry(t, editor, path)
		if stringSliceContains(e.Skills.Include, "keep") || !stringSliceContains(e.Commands.Include, "c1") {
			t.Fatalf("remove include = %#v", e)
		}
	})

	t.Run("include under all preserves all and clears exact exclusion", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {all: true, exclude: [skill/ex]}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "ex")}}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if !e.Default || !slices.Equal(e.Skills.Include, []string{"ex"}) || len(e.Skills.Exclude) != 0 {
			t.Fatalf("auto-select include = %#v", e)
		}
	})

	t.Run("include clears applicable exclude_all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: ['*']}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if e.ExcludeAll || !stringSliceContains(e.Skills.Include, "alpha") {
			t.Fatalf("exclude_all include = %#v", e)
		}
	})

	t.Run("select all and exclude all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: ['*']}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionSelectAll}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if !e.Default || e.ExcludeAll {
			t.Fatalf("select all = %#v", e)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExcludeAll}); err != nil {
			t.Fatal(err)
		}
		e = renderEntry(t, editor, path)
		if !e.ExcludeAll || e.Default || e.Tags != nil || len(e.Skills.Include) != 0 {
			t.Fatalf("exclude all = %#v", e)
		}
	})

	t.Run("exclude all clears per-kind includes and tags", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionExcludeAll}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if !e.ExcludeAll || len(e.Skills.Include) != 0 || len(e.Commands.Include) != 0 || e.Tags != nil {
			t.Fatalf("exclude all = %#v", e)
		}
	})

	t.Run("tag add dedupes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, base)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionAddTag, Tag: "t1"}); err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionAddTag, Tag: "t2"}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if len(e.Tags) != 2 || e.Tags[0] != "t1" || e.Tags[1] != "t2" {
			t.Fatalf("tags = %v", e.Tags)
		}
	})
}

func TestLayerEditorRejectsInvalidBindingsAndOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/keep]\n"
	layerEditorWrite(t, path, fixture)
	editor := openTestEditor(t, RepoCommitted, path, path)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}

	fabricated := SourceBinding{Name: "one", Entry: binding.Entry}
	if err := editor.Apply(fabricated, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "x")}}); layerEditorErrCode(err) != "config.edit.invalid-binding" {
		t.Fatalf("fabricated binding = %v", err)
	}
	if err := editor.RemoveSource(fabricated); layerEditorErrCode(err) != "config.edit.invalid-binding" {
		t.Fatalf("fabricated remove = %v", err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "x")}, Tag: "t"}); layerEditorErrCode(err) != "config.edit.unsupported-operation" {
		t.Fatalf("mixed fields = %v", err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionSelectAll, Locators: []ResourceLocator{loc(ir.KindSkill, "x")}}); layerEditorErrCode(err) != "config.edit.unsupported-operation" {
		t.Fatalf("select all with locators = %v", err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "good"), loc(ir.KindSkill, "bad/name")}}); err == nil {
		t.Fatal("invalid locator batch accepted")
	}
	if _, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/market", V4Kind: V4SourceMarketplace}); layerEditorErrCode(err) != "config.edit.unsupported-operation" {
		t.Fatalf("marketplace ensure = %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != fixture {
		t.Fatalf("rejected operations changed bytes:\n%s", candidate.Bytes)
	}
}

func TestLayerEditorV4RootPathBinding(t *testing.T) {
	for _, withRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing root=%v", withRoot), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			fixture := "apiVersion: agent-marketplace/v4\nsources:\n  sub: {git: https://example.test/repo.git, ref: main, path: skills}\n"
			if withRoot {
				fixture += "  root: {git: https://example.test/repo.git, ref: main}\n"
			}
			fixture += "resources:\n  sub: [skill/sub-only]\n"
			wantInclude := []string{"skill/new-root"}
			if withRoot {
				fixture += "  root: [skill/root-only]\n"
				wantInclude = []string{"skill/root-only", "skill/new-root"}
			}
			layerEditorWrite(t, path, fixture)
			editor := openTestEditor(t, RepoCommitted, path, path)
			binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"})
			if err != nil {
				t.Fatalf("ensure root: %v", err)
			}
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "new-root")}}); err != nil {
				t.Fatal(err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := ParseV4(candidate.Bytes)
			if err != nil {
				t.Fatalf("candidate invalid: %v", err)
			}
			if len(cfg.Sources) != 2 || cfg.Sources["sub"].Path != "skills" || len(cfg.Groups["sub"].Include) != 1 || cfg.Groups["sub"].Include[0].Locator != "skill/sub-only" {
				t.Fatalf("subpath changed or root missing: %#v %#v", cfg.Sources, cfg.Groups)
			}
			var gotInclude []string
			for _, request := range cfg.Groups[binding.Name].Include {
				gotInclude = append(gotInclude, request.Locator)
			}
			if cfg.Sources[binding.Name].Path != "" || !slices.Equal(gotInclude, wantInclude) {
				t.Fatalf("root selection missing: %#v %#v", cfg.Sources, cfg.Groups)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != fixture {
				t.Fatalf("editor wrote staged file: %v, %s", err, got)
			}
		})
	}
}

func TestLayerEditorExistingUserRelativeRegistryCleanup(t *testing.T) {
	t.Setenv("HOME", canonicalDirectoryTestRoot(t))
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	const initial = "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: ./project-registry}\nresources:\n  local: [skill/review]\n"
	entry := RegistryEntry{URL: "./project-registry", V4Kind: V4SourceDirectory}
	for _, tc := range []struct {
		name string
		edit func(*LayerEditor, SourceBinding) error
		want string
	}{
		{name: "disable", edit: func(e *LayerEditor, b SourceBinding) error {
			return e.Apply(b, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "review")}})
		}, want: "skill/review"},
		{name: "remove", edit: func(e *LayerEditor, b SourceBinding) error { return e.RemoveSource(b) }, want: "apiVersion: agent-marketplace/v4\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layerEditorWrite(t, path, initial)
			editor := openTestEditor(t, UserPersonal, path, path)
			binding, found, err := editor.LookupRegistry(entry)
			if err != nil || !found {
				t.Fatalf("lookup existing registry: found=%v err=%v", found, err)
			}
			if err := tc.edit(editor, binding); err != nil {
				t.Fatal(err)
			}
			candidate, err := editor.Render()
			if err != nil || !candidate.Changed || !strings.Contains(string(candidate.Bytes), tc.want) || (tc.name == "remove" && strings.Contains(string(candidate.Bytes), "project-registry")) {
				t.Fatalf("cleanup candidate: changed=%v bytes=%q err=%v", candidate.Changed, candidate.Bytes, err)
			}
			cfg, err := ParseV4(candidate.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "disable" && (len(cfg.Groups["local"].Include) != 0 || !slices.Equal(cfg.Groups["local"].Exclude, []string{"skill/review"})) {
				t.Fatalf("disable retained selection or lost exclusion: %#v", cfg.Groups)
			}
			if tc.name == "remove" && (len(cfg.Sources) != 0 || len(cfg.Groups) != 0) {
				t.Fatalf("remove retained source or group: %#v %#v", cfg.Sources, cfg.Groups)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != initial {
				t.Fatalf("editor wrote config: %q err=%v", data, err)
			}
		})
	}
	layerEditorWrite(t, path, initial)
	editor := openTestEditor(t, UserPersonal, path, path)
	if _, err := editor.EnsureRegistry(entry); err == nil {
		t.Fatal("enabling existing relative user registry succeeded")
	} else {
		var boundary *GlobalRelativeRegistryError
		if !errors.As(err, &boundary) {
			t.Fatalf("enable error = %v, want GlobalRelativeRegistryError", err)
		}
	}
	if candidate, err := editor.Render(); err != nil || candidate.Changed || string(candidate.Bytes) != initial {
		t.Fatalf("rejected enable changed config: %#v err=%v", candidate, err)
	}
}

func TestLayerEditorRejectsNewUserRelativeRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	const initial = "apiVersion: agent-marketplace/v4\nsources: {}\n"
	layerEditorWrite(t, path, initial)
	editor := openTestEditor(t, UserPersonal, path, path)
	if _, err := editor.EnsureRegistry(RegistryEntry{URL: "./project-registry"}); err == nil {
		t.Fatal("created relative user registry")
	} else {
		var boundary *GlobalRelativeRegistryError
		if !errors.As(err, &boundary) {
			t.Fatalf("creation error = %v, want GlobalRelativeRegistryError", err)
		}
	}
	if candidate, err := editor.Render(); err != nil || candidate.Changed || string(candidate.Bytes) != initial {
		t.Fatalf("rejected creation changed config: %#v err=%v", candidate, err)
	}
}

func TestLayerEditorRemoveSourcePreservesSiblings(t *testing.T) {
	t.Run("same url different ref preserves sibling selection", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\n  dev: {git: https://example.test/repo.git, ref: dev}\nresources:\n  main: [skill/keep]\n  dev: [skill/alpha]\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.RemoveSource(binding); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil {
			t.Fatalf("candidate invalid: %v", err)
		}
		if len(cfg.Sources) != 1 || cfg.Sources["main"].Ref != "main" || len(cfg.Groups) != 1 || len(cfg.Groups["main"].Include) != 1 || cfg.Groups["main"].Include[0].Locator != "skill/keep" {
			t.Fatalf("sibling source/selection = %#v %#v", cfg.Sources, cfg.Groups)
		}
	})

	t.Run("v4 same url different ref", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\n  dev: {git: https://example.test/repo.git, ref: dev}\nresources:\n  dev: [skill/alpha]\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.RemoveSource(binding); err != nil {
			t.Fatal(err)
		}
		if err := editor.RemoveSource(binding); err != nil {
			t.Fatalf("repeated remove = %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4Config(candidate.Bytes, path, true)
		if err != nil {
			t.Fatalf("candidate invalid: %v", err)
		}
		if len(cfg.Sources) != 1 {
			t.Fatalf("sources = %#v", cfg.Sources)
		}
		if _, ok := cfg.Sources["main"]; !ok || len(cfg.Groups) != 0 {
			t.Fatalf("sibling source lost: %#v %#v", cfg.Sources, cfg.Groups)
		}
	})
}

func TestLayerEditorBindingsAreEditorScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/keep]\n"
	layerEditorWrite(t, path, fixture)
	first := openTestEditor(t, RepoCommitted, path, path)
	second := openTestEditor(t, RepoCommitted, path, path)
	binding, err := first.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "x")}}); layerEditorErrCode(err) != "config.edit.invalid-binding" {
		t.Fatalf("cross-editor apply = %v", err)
	}
	if err := second.RemoveSource(binding); layerEditorErrCode(err) != "config.edit.invalid-binding" {
		t.Fatalf("cross-editor remove = %v", err)
	}
	candidate, err := second.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != fixture {
		t.Fatalf("second editor mutated:\n%s", candidate.Bytes)
	}
}

func TestLayerEditorSourceHarnessIdentity(t *testing.T) {
	t.Run("v4", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  claudeSrc: {git: https://example.test/repo.git, ref: main, path: skills, source_harness: claude}\n  cursorSrc: {git: https://example.test/repo.git, ref: main, path: skills, source_harness: cursor}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills", SourceHarness: "cursor"})
		if err != nil {
			t.Fatalf("ensure cursor: %v", err)
		}
		if binding.Name != "cursorSrc" {
			t.Fatalf("binding = %#v", binding)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed {
			t.Fatal("exact harness binding changed bytes")
		}
		if _, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills"}); layerEditorErrCode(err) != "config.edit.ambiguous-source" {
			t.Fatalf("empty harness across harnessed sources = %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed {
			t.Fatal("ambiguous harness ensure changed bytes")
		}
	})

	t.Run("v4 unique physical fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main, path: skills}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills", SourceHarness: "claude"})
		if err != nil {
			t.Fatalf("ensure fallback: %v", err)
		}
		if binding.Name != "one" {
			t.Fatalf("binding = %#v", binding)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatal("fallback ensure changed bytes")
		}
	})

	t.Run("remove only the matching source harness", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  claudeSrc: {git: https://example.test/repo.git, ref: main, path: skills, source_harness: claude}\n  cursorSrc: {git: https://example.test/repo.git, ref: main, path: skills, source_harness: cursor}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills", SourceHarness: "cursor"})
		if err != nil {
			t.Fatalf("ensure cursor: %v", err)
		}
		if err := editor.RemoveSource(binding); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil {
			t.Fatalf("candidate invalid: %v", err)
		}
		if len(cfg.Sources) != 1 || cfg.Sources["claudeSrc"].SourceHarness != "claude" {
			t.Fatalf("wrong entry removed: %#v", cfg.Sources)
		}
	})

	t.Run("unique physical fallback appends to existing group", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main, path: skills}\nresources:\n  one: [skill/alpha]\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills", SourceHarness: "claude"})
		if err != nil {
			t.Fatalf("ensure fallback: %v", err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "beta")}}); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil {
			t.Fatalf("candidate invalid: %v", err)
		}
		group := cfg.Groups["one"]
		if len(cfg.Sources) != 1 || len(group.Include) != 2 || group.Include[0].Locator != "skill/alpha" || group.Include[1].Locator != "skill/beta" {
			t.Fatalf("fallback did not bind the only entry: %#v %#v", cfg.Sources, cfg.Groups)
		}
	})
}

func TestLayerEditorClearBoundExclude(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	t.Run("clears only the bound exclude", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n  sibling: {git: https://example.test/other.git, ref: main}\nresources:\n  one: {exclude: [skill/alpha, skill/keep, command/c1]}\n  sibling: {exclude: [skill/entry-ex]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.ClearInstallExclude(entry, loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("clear: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil || !candidate.Changed {
			t.Fatalf("clear candidate: %#v, %v", candidate, err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cfg.Groups["one"].Exclude, []string{"skill/keep", "command/c1"}) || !slices.Equal(cfg.Groups["sibling"].Exclude, []string{"skill/entry-ex"}) || len(cfg.Sources) != 2 {
			t.Fatalf("unrelated kind or source touched: %#v %#v", cfg.Sources, cfg.Groups)
		}
		firstBytes := candidate.Bytes
		// Repeat a supported edit against the persisted candidate, as consumers
		// do on the next operation; clearing it again must be a byte-stable no-op.
		layerEditorWrite(t, path, string(candidate.Bytes))
		editor = openTestEditor(t, RepoPersonal, path, path)
		if err := editor.ClearInstallExclude(entry, loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil || candidate.Changed || string(candidate.Bytes) != string(firstBytes) {
			t.Fatalf("reapply changed bytes: %v", err)
		}
	})

	t.Run("clears last exclusion without removing the source", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/alpha]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.ClearInstallExclude(entry, loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil || !candidate.Changed || strings.Contains(string(candidate.Bytes), "skill/alpha") {
			t.Fatalf("last exclusion retained: %s, %v", candidate.Bytes, err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil || len(cfg.Sources) != 1 || len(cfg.Groups["one"].Exclude) != 0 {
			t.Fatalf("empty exclusion or source = %#v, %v", cfg, err)
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/alpha]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindSkill, "  "), loc(ir.ResourceKind(99), "alpha")} {
			if err := editor.ClearInstallExclude(entry, l); layerEditorErrCode(err) != "v4.locator" {
				t.Fatalf("locator %#v error = %v", l, err)
			}
		}
		candidate, err := editor.Render()
		if err != nil || candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("invalid locator mutated bytes: %v", err)
		}
	})
}

func TestLayerEditorBoundIncludeAndExclude(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	for _, tc := range []struct {
		name, group string
		locator     ResourceLocator
		wantInclude []string
		shorthand   bool
	}{
		{name: "mapping keeps sibling kind and source", group: "{include: [skill/alpha, skill/keep, command/c1]}", locator: loc(ir.KindSkill, "alpha"), wantInclude: []string{"skill/keep", "command/c1"}},
		{name: "mapping removes last selection", group: "{include: [skill/alpha]}", locator: loc(ir.KindSkill, "alpha")},
		{name: "shorthand keeps sibling", group: "[skill/alpha, skill/keep]", locator: loc(ir.KindSkill, "alpha"), wantInclude: []string{"skill/keep"}, shorthand: true},
		{name: "shorthand removes last collection", group: "[collection/bundle]", locator: loc(ir.KindCollection, "bundle"), shorthand: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n  sibling: {git: https://example.test/other.git, ref: main}\nresources:\n  one: " + tc.group + "\n  sibling: [skill/entry-inc]\n"
			layerEditorWrite(t, path, fixture)
			editor := openTestEditor(t, RepoPersonal, path, path)
			binding, err := editor.EnsureRegistry(entry)
			if err != nil {
				t.Fatal(err)
			}
			change := SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{tc.locator}}
			if err := editor.Apply(binding, change); err != nil {
				t.Fatalf("remove: %v", err)
			}
			candidate, err := editor.Render()
			if err != nil || !candidate.Changed {
				t.Fatalf("remove candidate: %#v, %v", candidate, err)
			}
			cfg, err := ParseV4(candidate.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, request := range cfg.Groups["one"].Include {
				got = append(got, request.Locator)
			}
			if !slices.Equal(got, tc.wantInclude) || len(cfg.Sources) != 2 || len(cfg.Groups["sibling"].Include) != 1 || cfg.Groups["sibling"].Include[0].Locator != "skill/entry-inc" || len(cfg.Groups["one"].Exclude) != 0 {
				t.Fatalf("wrong include or unrelated kind/source touched: %#v %#v", cfg.Sources, cfg.Groups)
			}
			if tc.shorthand && strings.Contains(string(candidate.Bytes), "include:") {
				t.Fatalf("shorthand expanded:\n%s", candidate.Bytes)
			}
			firstBytes := candidate.Bytes
			if err := editor.Apply(binding, change); err != nil {
				t.Fatalf("reapply: %v", err)
			}
			candidate, err = editor.Render()
			if err != nil || string(candidate.Bytes) != string(firstBytes) {
				t.Fatalf("reapply changed bytes: %v", err)
			}
		})
	}

	t.Run("sets and clears only the bound exclude", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/keep]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		change := SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}
		if err := editor.Apply(binding, change); err != nil {
			t.Fatalf("set: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil || !slices.Equal(cfg.Groups["one"].Exclude, []string{"skill/keep", "skill/alpha"}) {
			t.Fatalf("after set: %#v, %v", cfg, err)
		}
		firstBytes := candidate.Bytes
		if err := editor.Apply(binding, change); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil || string(candidate.Bytes) != string(firstBytes) {
			t.Fatalf("reapply changed bytes: %v", err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionClearExclude, Locators: change.Locators}); err != nil {
			t.Fatalf("clear: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = ParseV4(candidate.Bytes)
		if err != nil || !slices.Equal(cfg.Groups["one"].Exclude, []string{"skill/keep"}) {
			t.Fatalf("after clear: %#v, %v", cfg, err)
		}
	})

	t.Run("creates a group for an unset kind", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.SetLayerExclude(loc(ir.KindCommand, "audit"), []RegistryEntry{entry}); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := ParseV4(candidate.Bytes)
		if err != nil || len(cfg.Sources) != 1 || len(cfg.Groups) != 1 || len(cfg.Requests) != 0 {
			t.Fatalf("after set: %#v, %v", cfg, err)
		}
		for _, group := range cfg.Groups {
			if !slices.Equal(group.Exclude, []string{"command/audit"}) {
				t.Fatalf("created exclusions = %v", group.Exclude)
			}
		}
	})

	t.Run("missing provider is byte-stable and source-free writes refuse", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/alpha], exclude: [skill/beta]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		missing := RegistryEntry{URL: "https://example.test/missing.git", Ref: "main"}
		if _, found, err := editor.LookupRegistry(missing); err != nil || found {
			t.Fatalf("missing provider lookup: found=%v err=%v", found, err)
		}
		if err := editor.ClearInstallExclude(missing, loc(ir.KindSkill, "beta")); err != nil {
			t.Fatalf("clear absent provider: %v", err)
		}
		if err := editor.SetLayerExclude(loc(ir.KindSkill, "gamma"), nil); layerEditorErrCode(err) != "config.edit.unsupported-operation" {
			t.Fatalf("source-free exclusion = %v", err)
		}
		candidate, err := editor.Render()
		if err != nil || candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("missing/source-free operation mutated bytes: %s, %v", candidate.Bytes, err)
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/alpha]\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range []ResourceLocator{loc(ir.KindSkill, "  "), loc(ir.ResourceKind(99), "alpha")} {
			for _, operation := range []SelectionOperation{SelectionRemoveInclude, SelectionExclude} {
				if err := editor.Apply(binding, SelectionChange{Operation: operation, Locators: []ResourceLocator{l}}); layerEditorErrCode(err) != "v4.locator" {
					t.Fatalf("operation %v locator %#v error = %v", operation, l, err)
				}
			}
		}
		candidate, err := editor.Render()
		if err != nil || candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("invalid locator mutated bytes: %v", err)
		}
	})
}

func TestLayerEditorBoundIncludeInherited(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	t.Run("root-only selections are removable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [collection/bundle, skill/alpha]\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "bundle"), loc(ir.KindSkill, "alpha")} {
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{l}}); err != nil {
				t.Fatalf("root selection removal for %#v: %v", l, err)
			}
		}
		candidate, err := editor.Render()
		if err != nil || !candidate.Changed || len(editor.RenderedV4().Requests) != 0 {
			t.Fatalf("root removal candidate = %#v, %v", candidate, err)
		}
	})

	t.Run("one-level and nested includes retain ownership", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		nested := filepath.Join(dir, "nested.yaml")
		shared := filepath.Join(dir, "shared.yaml")
		const nestedData = "resources:\n  one: [collection/deep-bundle]\n"
		const sharedData = "include: nested.yaml\nresources:\n  one: [skill/shared-skill]\n"
		const rootData = "apiVersion: agent-marketplace/v4\ninclude: [shared.yaml, shared.yaml]\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [collection/root-bundle]\n"
		layerEditorWrite(t, nested, nestedData)
		layerEditorWrite(t, shared, sharedData)
		layerEditorWrite(t, path, rootData)
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if len(editor.RenderedV4().Requests) != 3 {
			t.Fatalf("non-vacuity: effective requests = %#v", editor.RenderedV4().Requests)
		}
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "deep-bundle"), loc(ir.KindSkill, "shared-skill")} {
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{l}}); layerEditorErrCode(err) != "config.edit.include-owned" {
				t.Fatalf("inherited removal for %#v = %v", l, err)
			}
		}
		candidate, err := editor.Render()
		if err != nil || candidate.Changed || string(candidate.Bytes) != rootData {
			t.Fatalf("refusal changed root: %s, %v", candidate.Bytes, err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindCollection, "root-bundle")}}); err != nil {
			t.Fatalf("root selection reported inherited: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil || !candidate.Changed {
			t.Fatalf("root removal candidate: %#v, %v", candidate, err)
		}
		var locators []string
		for _, request := range editor.RenderedV4().Requests {
			locators = append(locators, request.Locator)
		}
		if !slices.Equal(locators, []string{"collection/deep-bundle", "skill/shared-skill"}) {
			t.Fatalf("inherited selections changed: %v", locators)
		}
		for file, want := range map[string]string{path: rootData, shared: sharedData, nested: nestedData} {
			if data, err := os.ReadFile(file); err != nil || string(data) != want {
				t.Fatalf("editor changed %s: %s, %v", file, data, err)
			}
		}
	})

	t.Run("mapping root selection is removable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [collection/bundle]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{loc(ir.KindCollection, "bundle")}}); err != nil {
			t.Fatal(err)
		}
		if candidate, err := editor.Render(); err != nil || !candidate.Changed || len(editor.RenderedV4().Requests) != 0 {
			t.Fatalf("mapping removal = %#v, %v", candidate, err)
		}
	})

	t.Run("missing include rejected at open", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		const original = "apiVersion: agent-marketplace/v4\ninclude: [missing.yaml]\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [collection/bundle]\n"
		layerEditorWrite(t, path, original)
		if _, err := OpenLayerEditor(LayerTarget{Source: RepoPersonal, StagedPath: path, DeclaringPath: path}); err == nil {
			t.Fatal("accepted a missing include")
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != original {
			t.Fatalf("missing include refusal changed root: %s, %v", data, err)
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		const original = "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [collection/bundle]\n"
		layerEditorWrite(t, path, original)
		editor := openTestEditor(t, RepoPersonal, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "  "), loc(ir.ResourceKind(99), "bundle")} {
			if err := editor.Apply(binding, SelectionChange{Operation: SelectionRemoveInclude, Locators: []ResourceLocator{l}}); layerEditorErrCode(err) != "v4.locator" {
				t.Fatalf("invalid locator %#v error = %v", l, err)
			}
		}
		if candidate, err := editor.Render(); err != nil || candidate.Changed || string(candidate.Bytes) != original {
			t.Fatalf("invalid locator changed root: %#v, %v", candidate, err)
		}
	})
}

func TestLayerEditorRemoveMarketplacePinsIncludeProvenance(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rootRequest   bool
		childRequest  bool
		wantErrorCode string
		wantRequests  int
	}{
		{name: "root and fragment", rootRequest: true, childRequest: true, wantErrorCode: "config.edit.include-owned", wantRequests: 1},
		{name: "root only", rootRequest: true, wantRequests: 0},
		{name: "fragment only", childRequest: true, wantErrorCode: "config.edit.include-owned", wantRequests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			child := filepath.Join(dir, "child.yaml")
			fragment := "resources: {}\n"
			if tc.childRequest {
				fragment = "resources:\n  mp: {include: [src/skill/review]}\n"
			}
			layerEditorWrite(t, child, fragment)
			original := "apiVersion: agent-marketplace/v4\ninclude: child.yaml\nsources:\n  mp: {marketplace: https://mp.example.test}\n"
			if tc.rootRequest {
				original += "resources:\n  mp: {include: [src/skill/review]}\n"
			}
			layerEditorWrite(t, path, original)
			editor := openTestEditor(t, RepoPersonal, path, path)
			binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
			if err != nil || !found {
				t.Fatalf("lookup: found=%v err=%v", found, err)
			}
			if len(editor.RenderedV4().Requests) != 1 || editor.RenderedV4().Requests[0].Locator != "src/skill/review" {
				t.Fatalf("initial effective requests: %#v", editor.RenderedV4().Requests)
			}
			pin := testMarketplacePin()
			pin.RegistrySlug = "src"
			pin.Revision = pin.Digest
			pin.Direct = true
			err = editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{pin})
			if got := layerEditorErrCode(err); got != tc.wantErrorCode {
				t.Fatalf("remove error = %v, code = %q, want %q", err, got, tc.wantErrorCode)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Changed != (tc.wantErrorCode == "") || len(editor.RenderedV4().Requests) != tc.wantRequests {
				t.Fatalf("changed=%v effective requests=%#v; config:\n%s", candidate.Changed, editor.RenderedV4().Requests, candidate.Bytes)
			}
			if tc.wantErrorCode != "" && string(candidate.Bytes) != original {
				t.Fatalf("rejected removal mutated root: %s", candidate.Bytes)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != original {
				t.Fatalf("root on disk changed: %q, %v", got, err)
			}
			if got, err := os.ReadFile(child); err != nil || string(got) != fragment {
				t.Fatalf("fragment on disk changed: %q, %v", got, err)
			}
		})
	}
}

// layerEditorEditFile applies one selection change through the layer editor and
// publishes the candidate, so a test can reload the file between edits.
func layerEditorEditFile(t *testing.T, path string, entry RegistryEntry, op SelectionOperation, locator ResourceLocator) bool {
	t.Helper()
	editor := openTestEditor(t, RepoCommitted, path, path)
	binding, err := editor.EnsureRegistry(entry)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: op, Locators: []ResourceLocator{locator}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !candidate.Changed {
		return false
	}
	if err := os.WriteFile(path, candidate.Bytes, candidate.Mode); err != nil {
		t.Fatal(err)
	}
	return true
}

func TestLayerEditorV4PreservesMappingsOptionsCommentsAndKinds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	source := filepath.Join(dir, "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin] # targets\nsources:\n  local: {directory: "+source+"}\nresources:\n  local:\n    include:\n      - skill/beta: {as: beta}\n      - command/alpha\n    tags: [platform]\n    all: true\n    harnesses: [factory]\n    exclude: [skill/sibling]\n")
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	if !layerEditorEditFile(t, path, entry, SelectionInclude, loc(ir.KindSkill, "alpha")) {
		t.Fatal("include did not change the document")
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changed), "# targets") {
		t.Fatalf("comment lost:\n%s", changed)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 || len(loaded.Groups) != 1 {
		t.Fatalf("source/group counts = %d/%d", len(loaded.Sources), len(loaded.Groups))
	}
	group := loaded.Groups["local"]
	foundBeta, foundCommand, foundAlpha := false, false, false
	for _, request := range group.Include {
		switch request.Locator {
		case "skill/beta":
			foundBeta = request.Options.HasAs && request.Options.As == "beta"
		case "command/alpha":
			foundCommand = true
		case "skill/alpha":
			foundAlpha = true
		}
	}
	if !foundBeta || !foundCommand || !foundAlpha || !group.HasTags || !group.HasAll || !group.HasHarnesses || len(group.Exclude) != 1 || group.Exclude[0] != "skill/sibling" {
		t.Fatalf("unrelated resource policy changed: %#v", group)
	}

	layerEditorEditFile(t, path, entry, SelectionExclude, loc(ir.KindSkill, "alpha"))
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range loaded.Groups["local"].Include {
		if request.Locator == "skill/alpha" {
			t.Fatal("disabled alpha remained included")
		}
	}

	layerEditorEditFile(t, path, entry, SelectionInclude, loc(ir.KindSkill, "alpha"))
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups["local"].Include) != 3 {
		t.Fatalf("re-enabled include count = %d", len(loaded.Groups["local"].Include))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v err %v", info.Mode().Perm(), err)
	}
}

func TestLayerEditorV4KeepsSymlinkSourceIdentities(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	real := filepath.Join(dir, "version-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	currentA := filepath.Join(dir, "current-a")
	currentB := filepath.Join(dir, "current-b")
	if err := os.Symlink(real, currentA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, currentB); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {directory: "+currentA+"}\n  two: {directory: "+currentB+"}\nresources:\n  two: [skill/beta]\n")
	entry := RegistryEntry{URL: currentA, V4Kind: V4SourceDirectory}

	layerEditorEditFile(t, path, entry, SelectionInclude, loc(ir.KindSkill, "alpha"))
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 2 || loaded.Sources["one"].Directory != currentA || loaded.Sources["two"].Directory != currentB {
		t.Fatalf("source identities = %#v", loaded.Sources)
	}
	seen := map[string]bool{}
	for _, request := range loaded.Requests {
		seen[request.SourceName+"/"+request.Locator] = true
	}
	if len(seen) != 2 || !seen["one/skill/alpha"] || !seen["two/skill/beta"] {
		t.Fatalf("requests = %#v", loaded.Requests)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if layerEditorEditFile(t, path, entry, SelectionInclude, loc(ir.KindSkill, "alpha")) {
		t.Fatal("repeat include rewrote the document")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("repeat include changed bytes: %v\n%s", err, after)
	}
}

func TestLayerEditorV4CreatesSeparateSymlinkBinding(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	real := filepath.Join(dir, "version-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	currentA := filepath.Join(dir, "current-a")
	currentB := filepath.Join(dir, "current-b")
	if err := os.Symlink(real, currentA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, currentB); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {directory: "+currentA+"}\nresources:\n  one: [skill/beta]\n")

	layerEditorEditFile(t, path, RegistryEntry{URL: currentB, V4Kind: V4SourceDirectory}, SelectionInclude, loc(ir.KindSkill, "alpha"))
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	newName := ""
	for name, source := range loaded.Sources {
		if source.Directory == currentB {
			newName = name
		}
	}
	if newName == "" || len(loaded.Sources) != 2 || loaded.Sources["one"].Directory != currentA {
		t.Fatalf("sources = %#v", loaded.Sources)
	}
	oneBeta, newAlpha := false, false
	for _, request := range loaded.Requests {
		if request.SourceName == "one" {
			if request.Locator != "skill/beta" {
				t.Fatalf("one source request = %#v", request)
			}
			oneBeta = true
		}
		if request.SourceName == newName {
			if request.Locator != "skill/alpha" {
				t.Fatalf("new source request = %#v", request)
			}
			newAlpha = true
		}
	}
	if !oneBeta || !newAlpha {
		t.Fatalf("requests did not preserve separate selections: %#v", loaded.Requests)
	}
}

func TestLayerEditorRemoveMarketplacePinDirectWithSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  mp: {marketplace: https://mp.example.test}\nresources:\n  mp: {include: [src/skill/review]}\n")
	editor := openTestEditor(t, RepoPersonal, path, path)
	binding, found, err := editor.LookupMarketplaceSource("https://mp.example.test")
	if err != nil || !found {
		t.Fatalf("lookup marketplace source: found=%v err=%v", found, err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	pin := MarketplaceResourcePin{
		RegistryID:   "src",
		RegistrySlug: "src",
		Kind:         "skill",
		Name:         "review",
		Revision:     digest,
		Digest:       digest,
		Direct:       true,
		SelectedBy:   MarketplaceSelectionsFrom([]MarketplaceSelection{{RegistryID: "src", Name: "bundle", Revision: digest}}),
	}
	if err := editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
		t.Fatalf("remove direct+selected pin: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || strings.Contains(string(candidate.Bytes), "src/skill/review") {
		t.Fatalf("direct locator survived removal:\n%s", candidate.Bytes)
	}
	if !strings.Contains(string(candidate.Bytes), "marketplace:") {
		t.Fatalf("endpoint source dropped:\n%s", candidate.Bytes)
	}
}

func marketplaceEditorReadIntent(t *testing.T, home, userPath string) Intent {
	t.Helper()
	t.Setenv("HOME", home)
	SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-organization.yaml"))
	repo := t.TempDir()
	intent := ReadIntent(context.Background(), LoadEffectiveOptions{
		RepoRoot:       repo,
		LocalConfigDir: repo,
		GlobalPath:     userPath,
		RepoPath:       ProjectConfigPath(repo),
		LocalPath:      LocalConfigPath(repo),
	})
	if len(intent.Errors) > 0 || intent.Layers[UserPersonal].Err() != nil {
		t.Fatalf("read effective user intent: %v / %v", intent.Errors, intent.Layers[UserPersonal].Err())
	}
	return intent
}

func marketplaceEditorSortedSources(sources []SourceDescription) []SourceDescription {
	out := append([]SourceDescription(nil), sources...)
	sort.Slice(out, func(i, j int) bool {
		left := strings.Join([]string{string(out[i].Kind), out[i].Location, out[i].Ref, out[i].Path, out[i].SourceHarness, out[i].InstanceID}, "\x00")
		right := strings.Join([]string{string(out[j].Kind), out[j].Location, out[j].Ref, out[j].Path, out[j].SourceHarness, out[j].InstanceID}, "\x00")
		return left < right
	})
	return out
}

func marketplaceEditorSortedSelections(selections []ExplicitSelection) []ExplicitSelection {
	out := append([]ExplicitSelection(nil), selections...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func assertMarketplaceEditorGroup(t *testing.T, groups map[string]V4ResourceGroup, name string, include, exclude, harnesses []string) {
	t.Helper()
	group, ok := groups[name]
	if !ok || group.SourceName != name {
		t.Fatalf("resource group %q missing from effective config: %#v", name, group)
	}
	var locators []string
	for _, request := range group.Include {
		locators = append(locators, request.Locator)
	}
	if !slices.Equal(locators, include) || !slices.Equal(group.Exclude, exclude) ||
		!slices.Equal(group.Harnesses, harnesses) || group.HasHarnesses != (len(harnesses) > 0) {
		t.Fatalf("resource group %q policy = %#v, want include=%v exclude=%v harnesses=%v", name, group, include, exclude, harnesses)
	}
}

func TestLayerEditorRemoveMarketplaceRegistrationV4RootOnly(t *testing.T) {
	home := t.TempDir()
	rootDir := filepath.Join(home, ".agents")
	path := filepath.Join(rootDir, "config.yaml")
	fragmentPath := filepath.Join(rootDir, "fragment.yaml")
	localDir := filepath.Join(home, "local-registry")
	fragmentDir := filepath.Join(home, "included-registry")
	for _, dir := range []string{localDir, fragmentDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	targetURL := "http://127.0.0.1:12345/services/catalog"
	otherURL := "http://127.0.0.1:12346/other"
	root := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, claude] # preserve targets\nallowed_marketplaces: []\nlogging: {level: debug}\nsources:\n  target: {marketplace: " + targetURL + "}\n  other: {marketplace: " + otherURL + "} # preserve sibling\n  local: {directory: " + localDir + "}\nresources:\n  local:\n    include: [skill/keep]\n    exclude: [skill/drop]\n    harnesses: [devin]\n"
	fragment := "sources:\n  fragment: {directory: " + fragmentDir + "}\nresources:\n  fragment:\n    include: [skill/from-fragment]\n    harnesses: [claude]\n"
	layerEditorWrite(t, path, root)
	layerEditorWrite(t, fragmentPath, fragment)
	beforeIntent := marketplaceEditorReadIntent(t, home, path)
	beforeUser := beforeIntent.Layers[UserPersonal]
	wantBeforeSources := marketplaceEditorSortedSources([]SourceDescription{
		{Kind: V4SourceMarketplace, Location: targetURL},
		{Kind: V4SourceMarketplace, Location: otherURL},
		{Kind: V4SourceDirectory, Location: localDir},
		{Kind: V4SourceDirectory, Location: fragmentDir},
	})
	if got := marketplaceEditorSortedSources(beforeUser.DescribeSources()); !reflect.DeepEqual(got, wantBeforeSources) {
		t.Fatalf("effective v4 sources before removal = %#v, want %#v", got, wantBeforeSources)
	}
	wantSelections := marketplaceEditorSortedSelections([]ExplicitSelection{
		{Kind: ir.KindSkill, Name: "keep"},
		{Kind: ir.KindSkill, Name: "from-fragment"},
	})
	if got := marketplaceEditorSortedSelections(beforeUser.ExplicitSelections()); !reflect.DeepEqual(got, wantSelections) {
		t.Fatalf("unrelated v4 selections before removal = %#v, want %#v", got, wantSelections)
	}

	editor := openTestEditor(t, UserPersonal, path, path)
	entry := MarketplaceEntry{URL: targetURL}
	if err := editor.RemoveMarketplaceRegistration(entry); err != nil {
		t.Fatalf("remove endpoint-only v4 registration: %v", err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || strings.Contains(string(candidate.Bytes), targetURL) {
		t.Fatalf("v4 registration candidate = %#v\n%s", candidate, candidate.Bytes)
	}
	if !strings.Contains(string(candidate.Bytes), "# preserve targets") || !strings.Contains(string(candidate.Bytes), "# preserve sibling") {
		t.Fatalf("v4 removal lost unrelated comments:\n%s", candidate.Bytes)
	}
	if err := os.WriteFile(path, candidate.Bytes, candidate.Mode); err != nil {
		t.Fatal(err)
	}
	afterIntent := marketplaceEditorReadIntent(t, home, path)
	afterUser := afterIntent.Layers[UserPersonal]
	wantAfterSources := marketplaceEditorSortedSources([]SourceDescription{
		{Kind: V4SourceMarketplace, Location: otherURL},
		{Kind: V4SourceDirectory, Location: localDir},
		{Kind: V4SourceDirectory, Location: fragmentDir},
	})
	if got := marketplaceEditorSortedSources(afterUser.DescribeSources()); !reflect.DeepEqual(got, wantAfterSources) {
		t.Fatalf("effective v4 sources after removal = %#v, want %#v", got, wantAfterSources)
	}
	if !reflect.DeepEqual(beforeUser.Settings(), afterUser.Settings()) ||
		!reflect.DeepEqual(beforeUser.ExplicitSelections(), afterUser.ExplicitSelections()) {
		t.Fatalf("unrelated v4 settings or selections changed: before=%#v/%#v after=%#v/%#v", beforeUser.Settings(), beforeUser.ExplicitSelections(), afterUser.Settings(), afterUser.ExplicitSelections())
	}
	if len(beforeUser.v4.Groups) != 2 || len(afterUser.v4.Groups) != 2 {
		t.Fatalf("unrelated v4 resource groups changed: before=%#v after=%#v", beforeUser.v4.Groups, afterUser.v4.Groups)
	}
	for _, groups := range []map[string]V4ResourceGroup{beforeUser.v4.Groups, afterUser.v4.Groups} {
		assertMarketplaceEditorGroup(t, groups, "local", []string{"skill/keep"}, []string{"skill/drop"}, []string{"devin"})
		assertMarketplaceEditorGroup(t, groups, "fragment", []string{"skill/from-fragment"}, nil, []string{"claude"})
	}
	if len(afterUser.v4.Sources) != len(beforeUser.v4.Sources)-1 {
		t.Fatalf("effective v4 source count after removal = %d, want %d", len(afterUser.v4.Sources), len(beforeUser.v4.Sources)-1)
	}
	if _, ok := afterUser.v4.Sources["target"]; ok {
		t.Fatal("target Marketplace source remains after removal")
	}
	for name, beforeSource := range beforeUser.v4.Sources {
		if name == "target" {
			continue
		}
		afterSource, ok := afterUser.v4.Sources[name]
		if !ok || beforeSource.Name != afterSource.Name || beforeSource.Kind != afterSource.Kind ||
			beforeSource.URL != afterSource.URL || beforeSource.Git != afterSource.Git ||
			beforeSource.Directory != afterSource.Directory || beforeSource.Marketplace != afterSource.Marketplace ||
			beforeSource.Ref != afterSource.Ref || beforeSource.Path != afterSource.Path ||
			beforeSource.SourceHarness != afterSource.SourceHarness || beforeSource.CanonicalURL != afterSource.CanonicalURL {
			t.Fatalf("unrelated v4 source %q changed: before=%#v after=%#v", name, beforeSource, afterSource)
		}
	}
	staleEditor := openTestEditor(t, UserPersonal, path, path)
	if err := staleEditor.RemoveMarketplaceRegistration(entry); layerEditorErrCode(err) != "config.edit.invalid-binding" {
		t.Fatalf("repeated stale v4 registration removal = %v, want config.edit.invalid-binding", err)
	}
	repeated, err := staleEditor.Render()
	if err != nil || repeated.Changed || string(repeated.Bytes) != string(candidate.Bytes) {
		t.Fatalf("repeated stale removal changed candidate: %v\n%s", err, repeated.Bytes)
	}
	if got, err := os.ReadFile(fragmentPath); err != nil || got == nil || string(got) != fragment {
		t.Fatalf("v4 removal changed included fragment: %q (err=%v)", got, err)
	}
}

func marketplaceEditorSortedEntries(entries []MarketplaceEntry) []MarketplaceEntry {
	out := append([]MarketplaceEntry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		left := out[i].URL + "\x00" + out[i].InstanceID
		right := out[j].URL + "\x00" + out[j].InstanceID
		return left < right
	})
	return out
}

func TestLayerEditorRemoveMarketplaceRegistrationRefusesV4Groups(t *testing.T) {
	targetURL := "http://127.0.0.1:12345/services/catalog"
	for _, tc := range []struct {
		name              string
		include           string
		rootSource        bool
		rootResource      string
		fragment          string
		wantGroup         bool
		wantInclude       bool
		wantExclude       bool
		wantHarnessNarrow bool
		wantCode          string
	}{
		{
			name:         "root include",
			rootSource:   true,
			rootResource: "    include: [tools/skill/keep]\n",
			wantGroup:    true,
			wantInclude:  true,
			wantCode:     "config.edit.source-in-use",
		},
		{
			name:         "exclude only",
			rootSource:   true,
			rootResource: "    exclude: [tools/skill/blocked]\n",
			wantGroup:    true,
			wantExclude:  true,
			wantCode:     "config.edit.source-in-use",
		},
		{
			name:              "narrowing only",
			rootSource:        true,
			rootResource:      "    harnesses: [claude]\n",
			wantGroup:         true,
			wantHarnessNarrow: true,
			wantCode:          "config.edit.source-in-use",
		},
		{
			name:        "included resource group",
			include:     "include: fragment.yaml\n",
			rootSource:  true,
			fragment:    "resources:\n  target: {exclude: [tools/skill/blocked]}\n",
			wantGroup:   true,
			wantExclude: true,
			wantCode:    "config.edit.source-in-use",
		},
		{
			name:     "included-only source",
			include:  "include: fragment.yaml\n",
			fragment: "sources:\n  target: {marketplace: " + targetURL + "}\n",
			wantCode: "config.edit.include-owned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			fragmentPath := filepath.Join(dir, "fragment.yaml")
			root := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n" + tc.include
			if tc.rootSource {
				root += "sources:\n  target: {marketplace: " + targetURL + "}\n"
			}
			if tc.rootResource != "" {
				root += "resources:\n  target:\n" + tc.rootResource
			}
			if tc.fragment != "" {
				layerEditorWrite(t, fragmentPath, tc.fragment)
			}
			layerEditorWrite(t, path, root)
			editor := openTestEditor(t, UserPersonal, path, path)
			if tc.wantGroup {
				group, ok := editor.loaded.Groups["target"]
				if !ok {
					t.Fatal("protected Marketplace resource group is absent from effective input")
				}
				if tc.wantInclude && len(group.Include) == 0 {
					t.Fatalf("include control has no effective include: %#v", group)
				}
				if tc.wantExclude && len(group.Exclude) == 0 {
					t.Fatalf("exclude control has no effective exclusion: %#v", group)
				}
				if tc.wantHarnessNarrow && !group.HasHarnesses {
					t.Fatalf("narrowing control has no harness policy: %#v", group)
				}
			}
			if tc.name == "included-only source" {
				source, ok := editor.loaded.Sources["target"]
				if !ok || source.File == editor.canonical {
					t.Fatalf("include-owned control is not nonvacuous: %#v", source)
				}
			}
			err := editor.RemoveMarketplaceRegistration(MarketplaceEntry{URL: targetURL})
			if got := layerEditorErrCode(err); got != tc.wantCode {
				t.Fatalf("remove error = %v, code = %q, want %q", err, got, tc.wantCode)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Changed || string(candidate.Bytes) != root {
				t.Fatalf("rejected Marketplace removal changed candidate:\n%s", candidate.Bytes)
			}
			if current, err := os.ReadFile(path); err != nil || string(current) != root {
				t.Fatalf("rejected Marketplace removal changed root: %q (err=%v)", current, err)
			}
			if tc.fragment != "" {
				if current, err := os.ReadFile(fragmentPath); err != nil || current == nil || string(current) != tc.fragment {
					t.Fatalf("rejected Marketplace removal changed fragment: %q (err=%v)", current, err)
				}
			}
		})
	}
}

func TestLayerEditorRemoveMarketplaceRegistrationRejectsV4Identity(t *testing.T) {
	targetURL := "http://127.0.0.1:12345/services/catalog"
	for _, tc := range []struct {
		name     string
		root     string
		wantCode string
	}{
		{
			name:     "ambiguous canonical endpoint",
			root:     "apiVersion: agent-marketplace/v4\nsources:\n  first: {marketplace: " + targetURL + "/}\n  second: {marketplace: " + targetURL + "}\n",
			wantCode: "config.edit.ambiguous-source",
		},
		{
			name:     "unknown endpoint",
			root:     "apiVersion: agent-marketplace/v4\nsources:\n  other: {marketplace: http://127.0.0.1:12346/other}\n",
			wantCode: "config.edit.invalid-binding",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, tc.root)
			editor := openTestEditor(t, UserPersonal, path, path)
			err := editor.RemoveMarketplaceRegistration(MarketplaceEntry{URL: targetURL})
			if got := layerEditorErrCode(err); got != tc.wantCode {
				t.Fatalf("remove error = %v, code = %q, want %q", err, got, tc.wantCode)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Changed || string(candidate.Bytes) != tc.root {
				t.Fatalf("rejected v4 Marketplace removal changed candidate:\n%s", candidate.Bytes)
			}
		})
	}
}
