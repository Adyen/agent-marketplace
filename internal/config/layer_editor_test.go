package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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
		name    string
		fixture string
		v4      bool
	}{
		{
			name:    "v2",
			fixture: "apiVersion: agent-marketplace/v2\n# team comment\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    skills: {include: [keep]}\n",
		},
		{
			name:    "v4",
			fixture: "apiVersion: agent-marketplace/v4\n# team comment\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/keep]\n",
			v4:      true,
		},
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	for _, tc := range entries {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			layerEditorWrite(t, path, tc.fixture)

			assertIntent := func(data []byte, wantInclude, wantExclude []string) {
				t.Helper()
				if tc.v4 {
					cfg, err := ParseV4Config(data, path, true)
					if err != nil {
						t.Fatalf("candidate no longer valid v4: %v", err)
					}
					group := cfg.Groups["one"]
					var includes, excludes []string
					for _, r := range group.Include {
						includes = append(includes, r.Locator)
					}
					excludes = append(excludes, group.Exclude...)
					for _, want := range wantInclude {
						found := false
						for _, got := range includes {
							if got == want {
								found = true
							}
						}
						if !found {
							t.Fatalf("v4 include %v missing %q", includes, want)
						}
					}
					for _, want := range wantExclude {
						found := false
						for _, got := range excludes {
							if got == want {
								found = true
							}
						}
						if !found {
							t.Fatalf("v4 exclude %v missing %q", excludes, want)
						}
					}
					for _, unwanted := range wantExclude {
						for _, got := range includes {
							if got == unwanted {
								t.Fatalf("v4 include still carries %q", unwanted)
							}
						}
					}
					return
				}
				cfg, errs := loadV3ConfigWithAPIVersion(data, repoLayer, false, true)
				if len(errs) > 0 {
					t.Fatalf("candidate no longer valid legacy: %v", errs)
				}
				if len(cfg.Registries) != 1 {
					t.Fatalf("legacy registries = %#v", cfg.Registries)
				}
				selector := cfg.Registries[0].SelectorForKind(ir.KindSkill)
				for _, want := range wantInclude {
					if !stringSliceContains(selector.Include, want) {
						t.Fatalf("legacy include %v missing %q", selector.Include, want)
					}
				}
				for _, want := range wantExclude {
					if !stringSliceContains(selector.Exclude, want) {
						t.Fatalf("legacy exclude %v missing %q", selector.Exclude, want)
					}
					if stringSliceContains(selector.Include, want) {
						t.Fatalf("legacy include still carries %q", want)
					}
				}
			}

			editor := openTestEditor(t, RepoCommitted, path, path)
			binding, err := editor.EnsureRegistry(entry)
			if err != nil {
				t.Fatalf("EnsureRegistry: %v", err)
			}
			if tc.v4 && binding.Name != "one" {
				t.Fatalf("v4 binding name = %q", binding.Name)
			}
			if !tc.v4 && binding.Name != "" {
				t.Fatalf("legacy binding name = %q", binding.Name)
			}

			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("include: %v", err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !candidate.Changed || candidate.Mode.Perm() != 0o640 || candidate.Legacy == tc.v4 {
				t.Fatalf("candidate = %#v", candidate)
			}
			if PeekAPIVersion(candidate.Bytes) != PeekAPIVersion([]byte(tc.fixture)) {
				t.Fatalf("schema version changed: %s", candidate.Bytes)
			}
			if !strings.Contains(string(candidate.Bytes), "# team comment") {
				t.Fatalf("comment lost:\n%s", candidate.Bytes)
			}
			if tc.v4 {
				assertIntent(candidate.Bytes, []string{"skill/keep", "skill/alpha"}, nil)
			} else {
				assertIntent(candidate.Bytes, []string{"keep", "alpha"}, nil)
			}

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
			if err != nil {
				t.Fatalf("noop render: %v", err)
			}
			if noop.Changed || string(noop.Bytes) != string(candidate.Bytes) {
				t.Fatalf("idempotent include changed bytes")
			}

			if err := editor.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("exclude: %v", err)
			}
			excluded, err := editor.Render()
			if err != nil {
				t.Fatalf("exclude render: %v", err)
			}
			if tc.v4 {
				assertIntent(excluded.Bytes, []string{"skill/keep"}, []string{"skill/alpha"})
			} else {
				assertIntent(excluded.Bytes, []string{"keep"}, []string{"alpha"})
			}

			if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "alpha")}}); err != nil {
				t.Fatalf("re-enable: %v", err)
			}
			enabled, err := editor.Render()
			if err != nil {
				t.Fatalf("re-enable render: %v", err)
			}
			if tc.v4 {
				assertIntent(enabled.Bytes, []string{"skill/keep", "skill/alpha"}, nil)
			} else {
				assertIntent(enabled.Bytes, []string{"keep", "alpha"}, nil)
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

	t.Run("legacy delegates to the top-level selector", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		entries := []RegistryEntry{{URL: "https://example.test/one.git", Ref: "main", V4Kind: V4SourceGit}}
		if err := editor.SetLayerExclude(loc(ir.KindCollection, "bundle"), entries); err != nil {
			t.Fatalf("legacy set layer exclude: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 || !slices.Contains(cfg.Collections.Exclude, "bundle") {
			t.Fatalf("legacy top-level exclusion missing: %v %#v\n%s", errs, cfg.Collections, candidate.Bytes)
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

func TestLayerEditorLegacySelectionOperations(t *testing.T) {
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}
	renderEntry := func(t *testing.T, editor *LayerEditor, path string) RegistryEntry {
		t.Helper()
		candidate, err := editor.Render()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, repoLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v\n%s", errs, candidate.Bytes)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("registries = %#v", cfg.Registries)
		}
		return cfg.Registries[0]
	}
	base := "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    skills: {include: [keep, go], exclude: [ex]}\n    commands: {include: [c1]}\n    tags: [t1]\n"

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

	t.Run("include under default auto-select stays implicit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    default: true\n    skills: {exclude: [ex]}\n")
		editor := openTestEditor(t, RepoCommitted, path, path)
		binding, err := editor.EnsureRegistry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{loc(ir.KindSkill, "ex")}}); err != nil {
			t.Fatal(err)
		}
		e := renderEntry(t, editor, path)
		if !e.Default || len(e.Skills.Include) != 0 || len(e.Skills.Exclude) != 0 {
			t.Fatalf("auto-select include = %#v", e)
		}
	})

	t.Run("include clears applicable exclude_all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    exclude_all: true\n")
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
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    exclude_all: true\n")
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

func TestLayerEditorLegacyRootPathBinding(t *testing.T) {
	const subpath = "  - url: https://example.test/repo.git\n    ref: main\n    path: skills\n    skills: {include: [sub-only]}\n"
	const root = "  - url: https://example.test/repo.git\n    ref: main\n    skills: {include: [root-only]}\n"
	for _, tc := range []struct {
		name        string
		entries     string
		wantInclude []string
	}{
		{name: "sole subpath", entries: subpath, wantInclude: []string{"new-root"}},
		{name: "root after subpath", entries: subpath + root, wantInclude: []string{"root-only", "new-root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			fixture := "apiVersion: agent-marketplace/v2\nregistries:\n" + tc.entries
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
			cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, repoLayer, false, true)
			if len(errs) > 0 {
				t.Fatalf("candidate invalid: %v", errs)
			}
			if len(cfg.Registries) != 2 || cfg.Registries[0].Path != "skills" || !slices.Equal(cfg.Registries[0].Skills.Include, []string{"sub-only"}) {
				t.Fatalf("subpath changed or root missing: %#v", cfg.Registries)
			}
			rootEntry := cfg.Registries[1]
			if rootEntry.Path != "" || !slices.Equal(rootEntry.Skills.Include, tc.wantInclude) {
				t.Fatalf("root selection missing: %#v", cfg.Registries)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != fixture {
				t.Fatalf("editor wrote staged file: %v, %s", err, got)
			}
		})
	}
}

func TestLayerEditorExistingUserRelativeRegistryCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	const initial = "apiVersion: agent-marketplace/v2\nregistries:\n  - url: ./project-registry\n    skills: {include: [review]}\n"
	entry := RegistryEntry{URL: "./project-registry"}
	for _, tc := range []struct {
		name string
		edit func(*LayerEditor, SourceBinding) error
		want string
	}{
		{name: "disable", edit: func(e *LayerEditor, b SourceBinding) error {
			return e.Apply(b, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{loc(ir.KindSkill, "review")}})
		}, want: "exclude: [review]"},
		{name: "remove", edit: func(e *LayerEditor, b SourceBinding) error { return e.RemoveSource(b) }, want: "apiVersion: agent-marketplace/v2\n"},
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
	const initial = "apiVersion: agent-marketplace/v2\nregistries: []\n"
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
	t.Run("legacy same url different ref", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n  - url: https://example.test/repo.git\n    ref: dev\n    skills: {include: [alpha]}\n")
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
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, repoLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v", errs)
		}
		if len(cfg.Registries) != 1 || cfg.Registries[0].Ref != "main" {
			t.Fatalf("registries = %#v", cfg.Registries)
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

	t.Run("legacy", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    path: skills\n    source_harness: claude\n  - url: https://example.test/repo.git\n    ref: main\n    path: skills\n    source_harness: cursor\n")
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
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, repoLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v", errs)
		}
		if len(cfg.Registries) != 1 || cfg.Registries[0].SourceHarness != "claude" {
			t.Fatalf("wrong entry removed: %#v", cfg.Registries)
		}
	})

	t.Run("legacy unique physical fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    path: skills\n    skills: {include: [alpha]}\n")
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
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, repoLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v", errs)
		}
		if len(cfg.Registries) != 1 || !stringSliceContains(cfg.Registries[0].Skills.Include, "beta") {
			t.Fatalf("fallback did not bind the only entry: %#v", cfg.Registries)
		}
	})
}

func TestLayerEditorClearUnboundExclude(t *testing.T) {
	t.Run("legacy clears only the top-level exclude", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    skills: {exclude: [entry-ex]}\nskills: {exclude: [alpha, keep]}\ncommands: {exclude: [c1]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.ClearUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("clear: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if !candidate.Changed {
			t.Fatal("clear reported no change")
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v\n%s", errs, candidate.Bytes)
		}
		if len(cfg.Skills.Exclude) != 1 || cfg.Skills.Exclude[0] != "keep" {
			t.Fatalf("top-level excludes = %#v", cfg.Skills)
		}
		if len(cfg.Commands.Exclude) != 1 || cfg.Commands.Exclude[0] != "c1" {
			t.Fatalf("unrelated kind touched: %#v", cfg.Commands)
		}
		if len(cfg.Registries) != 1 || len(cfg.Registries[0].Skills.Exclude) != 1 || cfg.Registries[0].Skills.Exclude[0] != "entry-ex" {
			t.Fatalf("registry entry touched: %#v", cfg.Registries)
		}
		firstBytes := candidate.Bytes
		if err := editor.ClearUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if string(candidate.Bytes) != string(firstBytes) {
			t.Fatal("reapply changed bytes")
		}
	})

	t.Run("legacy drops emptied nested keys", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nskills: {exclude: [alpha]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.ClearUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(candidate.Bytes), "skills") {
			t.Fatalf("empty skills key retained:\n%s", candidate.Bytes)
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v2\nskills: {exclude: [alpha]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindSkill, "  "), loc(ir.ResourceKind(99), "alpha")} {
			if err := editor.ClearUnboundExclude(l); err == nil {
				t.Fatalf("accepted locator %#v", l)
			}
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatal("invalid locator mutated bytes")
		}
	})
}

func TestLayerEditorUnboundIncludeAndExclude(t *testing.T) {
	t.Run("legacy removes only the top-level include", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    skills: {include: [entry-inc]}\nskills: {include: [alpha, keep]}\ncommands: {include: [c1]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("remove include: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if !candidate.Changed {
			t.Fatal("remove reported no change")
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v\n%s", errs, candidate.Bytes)
		}
		if len(cfg.Skills.Include) != 1 || cfg.Skills.Include[0] != "keep" {
			t.Fatalf("top-level includes = %#v", cfg.Skills)
		}
		if len(cfg.Commands.Include) != 1 || cfg.Commands.Include[0] != "c1" {
			t.Fatalf("unrelated kind touched: %#v", cfg.Commands)
		}
		if len(cfg.Registries) != 1 || len(cfg.Registries[0].Skills.Include) != 1 || cfg.Registries[0].Skills.Include[0] != "entry-inc" {
			t.Fatalf("registry entry touched: %#v", cfg.Registries)
		}
		firstBytes := candidate.Bytes
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if string(candidate.Bytes) != string(firstBytes) {
			t.Fatal("reapply changed bytes")
		}
	})

	t.Run("legacy drops emptied nested keys", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nskills: {include: [alpha]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(candidate.Bytes), "skills") {
			t.Fatalf("empty skills key retained:\n%s", candidate.Bytes)
		}
	})

	t.Run("legacy shorthand sequence keeps siblings", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v2\nskills: [alpha, keep]\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("remove include: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if !candidate.Changed {
			t.Fatal("remove reported no change")
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v\n%s", errs, candidate.Bytes)
		}
		if len(cfg.Skills.Include) != 1 || cfg.Skills.Include[0] != "keep" {
			t.Fatalf("shorthand includes = %#v", cfg.Skills)
		}
		if strings.Contains(string(candidate.Bytes), "include:") {
			t.Fatalf("shorthand expanded to mapping form:\n%s", candidate.Bytes)
		}
		firstBytes := candidate.Bytes
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if string(candidate.Bytes) != string(firstBytes) {
			t.Fatal("reapply changed bytes")
		}
	})

	t.Run("legacy shorthand drops the emptied kind key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\ncollections: [bundle]\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.RemoveUnboundInclude(loc(ir.KindCollection, "bundle")); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if !candidate.Changed || strings.Contains(string(candidate.Bytes), "collections") || strings.Contains(string(candidate.Bytes), "bundle") {
			t.Fatalf("shorthand key retained:\n%s", candidate.Bytes)
		}
	})

	t.Run("legacy sets and clears the top-level exclude", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\nskills: {exclude: [keep]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.SetUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("set: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("candidate invalid: %v\n%s", errs, candidate.Bytes)
		}
		if len(cfg.Skills.Exclude) != 2 || cfg.Skills.Exclude[0] != "keep" || cfg.Skills.Exclude[1] != "alpha" {
			t.Fatalf("top-level excludes = %#v", cfg.Skills)
		}
		firstBytes := candidate.Bytes
		if err := editor.SetUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("reapply: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if string(candidate.Bytes) != string(firstBytes) {
			t.Fatal("reapply changed bytes")
		}
		if err := editor.ClearUnboundExclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("clear: %v", err)
		}
		candidate, err = editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, errs = loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 || len(cfg.Skills.Exclude) != 1 || cfg.Skills.Exclude[0] != "keep" {
			t.Fatalf("after clear: %v %#v", errs, cfg.Skills)
		}
	})

	t.Run("legacy creates the selector for an unset kind", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.SetUnboundExclude(loc(ir.KindCommand, "audit")); err != nil {
			t.Fatal(err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		cfg, errs := loadV3ConfigWithAPIVersion(candidate.Bytes, localLayer, false, true)
		if len(errs) > 0 || len(cfg.Commands.Exclude) != 1 || cfg.Commands.Exclude[0] != "audit" {
			t.Fatalf("after set: %v %#v", errs, cfg.Commands)
		}
	})

	t.Run("v4 is a byte-stable no-op", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/alpha], exclude: [skill/beta]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		if err := editor.RemoveUnboundInclude(loc(ir.KindSkill, "alpha")); err != nil {
			t.Fatalf("remove include: %v", err)
		}
		if err := editor.SetUnboundExclude(loc(ir.KindSkill, "gamma")); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("expected unsupported-operation for v4 unbound exclude, got %v", err)
		}
		if err := editor.ClearUnboundExclude(loc(ir.KindSkill, "beta")); err != nil {
			t.Fatalf("clear exclude: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatalf("v4 unbound selector mutated:\n%s", candidate.Bytes)
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		fixture := "apiVersion: agent-marketplace/v2\nskills: {include: [alpha]}\n"
		layerEditorWrite(t, path, fixture)
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindSkill, "  "), loc(ir.ResourceKind(99), "alpha")} {
			if err := editor.RemoveUnboundInclude(l); err == nil {
				t.Fatalf("RemoveUnboundInclude accepted locator %#v", l)
			}
			if err := editor.SetUnboundExclude(l); err == nil {
				t.Fatalf("SetUnboundExclude accepted locator %#v", l)
			}
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed || string(candidate.Bytes) != fixture {
			t.Fatal("invalid locator mutated bytes")
		}
	})
}

func TestLayerEditorUnboundIncludeInherited(t *testing.T) {
	t.Run("root-only selection is not inherited", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\ncollections: {include: [bundle]}\nskills: {include: [alpha]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "bundle"), loc(ir.KindSkill, "alpha")} {
			inherited, err := editor.UnboundIncludeInherited(l)
			if err != nil || inherited {
				t.Fatalf("inherited = %v err = %v for %#v", inherited, err, l)
			}
		}
	})

	t.Run("one-level and nested includes are inherited", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		layerEditorWrite(t, filepath.Join(dir, "nested.yaml"), "collections: {include: [deep-bundle]}\n")
		layerEditorWrite(t, filepath.Join(dir, "shared.yaml"), "include: nested.yaml\nskills: {include: [shared-skill]}\n")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\ninclude: [shared.yaml, shared.yaml]\ncollections: {include: [root-bundle]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "deep-bundle"), loc(ir.KindSkill, "shared-skill")} {
			inherited, err := editor.UnboundIncludeInherited(l)
			if err != nil || !inherited {
				t.Fatalf("inherited = %v err = %v for %#v", inherited, err, l)
			}
		}
		inherited, err := editor.UnboundIncludeInherited(loc(ir.KindCollection, "root-bundle"))
		if err != nil || inherited {
			t.Fatalf("root selection reported inherited: %v %v", inherited, err)
		}
	})

	t.Run("v4 reports false", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [collection/bundle]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		inherited, err := editor.UnboundIncludeInherited(loc(ir.KindCollection, "bundle"))
		if err != nil || inherited {
			t.Fatalf("v4 inherited = %v err = %v", inherited, err)
		}
	})

	t.Run("malformed include rejected at open", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\ninclude: [missing.yaml]\ncollections: {include: [bundle]}\n")
		if _, err := OpenLayerEditor(LayerTarget{Source: RepoPersonal, StagedPath: path, DeclaringPath: path}); err == nil {
			t.Fatal("accepted a missing include")
		}
	})

	t.Run("invalid locators", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		layerEditorWrite(t, path, "apiVersion: agent-marketplace/v2\ncollections: {include: [bundle]}\n")
		editor := openTestEditor(t, RepoPersonal, path, path)
		for _, l := range []ResourceLocator{loc(ir.KindCollection, "  "), loc(ir.ResourceKind(99), "bundle")} {
			if _, err := editor.UnboundIncludeInherited(l); err == nil {
				t.Fatalf("accepted locator %#v", l)
			}
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
