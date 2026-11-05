package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalDirectoryTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestV4RepositoryDirectorySources(t *testing.T) {
	for _, filename := range []string{"config.yaml", "config.local.yaml"} {
		for _, directory := range []string{".", "./skills", "./resources/skills"} {
			t.Run(filename+"/"+directory, func(t *testing.T) {
				repo := canonicalDirectoryTestRoot(t)
				file := filepath.Join(repo, ".agents", filename)
				writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: "+directory+"}}\nresources: {registry: {all: true}}\n")
				want := filepath.Join(repo, directory)
				cfg, err := LoadV4ConfigFromPath(file)
				if err != nil {
					t.Fatal(err)
				}
				if got := cfg.Sources["registry"].Directory; got != want {
					t.Fatalf("directory = %q, want %q", got, want)
				}
				for _, load := range []func(string) (*Config, error){LoadConfigFromPath, LoadRootConfigFromPath} {
					adapted, err := load(file)
					if err != nil {
						t.Fatal(err)
					}
					if len(adapted.Registries) != 1 || adapted.Registries[0].URL != want {
						t.Fatalf("runtime registries = %#v, want %q", adapted.Registries, want)
					}
				}
			})
		}
	}
}

func TestV4DirectoryPreservesMachineAndFragmentBases(t *testing.T) {
	home := canonicalDirectoryTestRoot(t)
	t.Setenv("HOME", home)
	for _, kind := range []layerKind{userLayer, organizationLayer} {
		file := filepath.Join(home, ".agents", "config.yaml")
		writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ./resources/skills}}\nresources: {registry: {all: true}}\n")
		data, err := readConfigFile(file)
		if err != nil {
			t.Fatal(err)
		}
		li, err := readLayerIntent(file, "", data, kind, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := li.bind(nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := cfg.Registries[0].URL, filepath.Join(home, ".agents", "resources", "skills"); got != want {
			t.Fatalf("machine directory = %q, want %q", got, want)
		}
	}
	repo := canonicalDirectoryTestRoot(t)
	file := filepath.Join(repo, ".agents", "config.yaml")
	writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\ninclude: fragments/sources.yaml\n")
	writeIncludeConfig(t, filepath.Join(repo, ".agents", "fragments", "sources.yaml"), "sources: {registry: {directory: ./resources/skills}}\nresources: {registry: {all: true}}\n")
	cfg, err := LoadConfigFromPath(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Registries[0].URL, filepath.Join(repo, ".agents", "fragments", "resources", "skills"); got != want {
		t.Fatalf("fragment directory = %q, want %q", got, want)
	}
}

func TestV4DirectoryOverlayUsesRepositoryRoot(t *testing.T) {
	repo := canonicalDirectoryTestRoot(t)
	file := filepath.Join(repo, ".agents", "config.yaml")
	writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\n")
	data := []byte("apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ./resources/skills}}\nresources: {registry: {all: true}}\n")
	li, err := readLayerIntent(file, file, data, repoLayer, map[string][]byte{file: data})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := li.bind(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Registries[0].URL, filepath.Join(repo, "resources", "skills"); got != want {
		t.Fatalf("overlay directory = %q, want %q", got, want)
	}
}

func TestV4RepositoryDirectoryContainment(t *testing.T) {
	repo := canonicalDirectoryTestRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "resources"), filepath.Join(repo, "inside")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, ".agents", "config.yaml")
	writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ./inside}}\nresources: {registry: {all: true}}\n")
	if _, err := LoadConfigFromPath(file); err != nil {
		t.Fatalf("in-repository symlink rejected: %v", err)
	}
	for _, directory := range []string{"../outside", "./escape", "./escape/missing"} {
		file := filepath.Join(repo, ".agents", "config.yaml")
		writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: "+directory+"}}\n")
		_, err := LoadConfigFromPath(file)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.directory-path" {
			t.Errorf("directory %q: error = %v, want v4.directory-path", directory, err)
		}
	}
}

func TestV4EditorWithRelativeConfigPathKeepsRepositoryDirectoriesPortable(t *testing.T) {
	root := canonicalDirectoryTestRoot(t)
	repo := filepath.Join(root, "repo")
	file := filepath.Join(repo, ".agents", "config.yaml")
	writeIncludeConfig(t, file, "apiVersion: agent-marketplace/v4\n")
	withWorkingDir(t, root)
	relativeFile := filepath.Join("repo", ".agents", "config.yaml")
	editor, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: relativeFile, DeclaringPath: relativeFile})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: "./resources/skills", V4Kind: V4SourceDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionSelectAll}); err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || strings.Contains(string(candidate.Bytes), repo) || !strings.Contains(string(candidate.Bytes), "directory: ./resources/skills") {
		t.Fatalf("edit did not preserve a portable directory: %s", candidate.Bytes)
	}
}

func TestV4RepositoryDirectoriesRemainPortableAfterRelocation(t *testing.T) {
	for _, directory := range []string{".", "./resources/skills"} {
		for _, subpath := range []string{"", "skills"} {
			t.Run(directory+"/"+subpath, func(t *testing.T) {
				repo := canonicalDirectoryTestRoot(t)
				file := filepath.Join(repo, ".agents", "config.yaml")
				authored := "./" + filepath.ToSlash(filepath.Join(directory, subpath))
				data := "apiVersion: agent-marketplace/v4\nsources: {local: {directory: " + authored + "}}\nresources: {local: {all: true}}\n"
				writeIncludeConfig(t, file, data)
				cfg, err := LoadConfigFromPath(file)
				if err != nil || cfg == nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != filepath.Join(repo, directory, subpath) {
					t.Fatalf("original directory = %#v, %v", cfg, err)
				}
				moved := canonicalDirectoryTestRoot(t)
				movedFile := filepath.Join(moved, ".agents", "config.yaml")
				writeIncludeConfig(t, movedFile, data)
				cfg, err = LoadConfigFromPath(movedFile)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := cfg.Registries[0].URL, filepath.Join(moved, directory, subpath); got != want {
					t.Fatalf("relocated directory = %q, want %q", got, want)
				}
				if got := readConfigString(t, movedFile); got != data || strings.Contains(got, repo) || strings.Contains(got, moved) {
					t.Fatalf("read rewrote portable configuration: %s", got)
				}
			})
		}
	}
}
