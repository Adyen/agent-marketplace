package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: consumer-config-format/explicit-non-v4-root-versions-are-refused
func TestV4OnlyStagedEditorRoots(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		for _, version := range append(append([]struct{ name, header, code string }{}, v4OnlyVersions...), struct{ name, header, code string }{"empty-file", "", "v4.api-version-required"}) {
			t.Run(source.String()+"/"+version.name, func(t *testing.T) {
				fixture := newV4OnlyFixture(t)
				path := fixture.paths[source]
				before := v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\n")
				staged := filepath.Join(filepath.Dir(path), "staged.yaml")
				body := "sources: {agents: {git: https://example.test/agents.git, ref: main}}\nresources: {agents: [skill/review]}\n"
				if version.name == "empty-file" {
					body = ""
				}
				stagedBefore := v4OnlyWriteFixture(t, staged, version.header+body)
				editor, err := OpenLayerEditor(LayerTarget{Source: source, StagedPath: staged, DeclaringPath: path})
				if version.code != "" {
					v4OnlyAssertDiagnostic(t, err, version.code, path)
				} else {
					if err != nil {
						t.Fatal(err)
					}
					binding, found, err := editor.LookupRegistry(RegistryEntry{URL: "https://example.test/agents.git", Ref: "main"})
					if err != nil || !found {
						t.Fatalf("v4 binding missing: %#v %v", binding, err)
					}
					if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{{Kind: ir.KindCommand, Name: "audit"}}}); err != nil {
						t.Fatal(err)
					}
					candidate, err := editor.Render()
					if err != nil || !candidate.Changed || editor.RenderedV4() == nil || len(editor.RenderedV4().Requests) != 2 {
						t.Fatalf("v4 editor control = %#v %v", candidate, err)
					}
				}
				v4OnlyAssertUnchanged(t, path, before)
				v4OnlyAssertUnchanged(t, staged, stagedBefore)
				assertNoConfigTempResidue(t, filepath.Dir(path))
			})
		}
	}
}

func TestV4OnlyExistingInitialization(t *testing.T) {
	for _, version := range v4OnlyVersions {
		t.Run(version.name, func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			path := fixture.paths[UserPersonal]
			before := v4OnlyWriteFixture(t, path, version.header+"harnesses: [factory]\n")
			_, err := InitializeUserPersonalAtPath(path, []string{"windsurf"})
			if version.code == "" {
				if !errors.Is(err, ErrAlreadyInitialized) {
					t.Fatalf("existing v4 root = %v", err)
				}
			} else {
				v4OnlyAssertDiagnostic(t, err, version.code, path)
			}
			v4OnlyAssertUnchanged(t, path, before)
			assertNoConfigTempResidue(t, filepath.Dir(path))
		})
	}
}

func TestV4OnlyOverlayRoots(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		for _, version := range v4OnlyVersions {
			t.Run(source.String()+"/"+version.name, func(t *testing.T) {
				fixture := newV4OnlyFixture(t)
				path := fixture.paths[source]
				before := v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\n")
				intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo, FileOverlays: map[string][]byte{path: []byte(version.header + v4OnlyRootBody(source))}})
				if version.code == "" {
					if err := intent.Layers[source].Err(); err != nil {
						t.Fatal(err)
					}
				} else {
					v4OnlyAssertDiagnostic(t, intent.Layers[source].Err(), version.code, path)
				}
				v4OnlyAssertUnchanged(t, path, before)
			})
		}
	}
}

// spec: consumer-config-format/explicit-non-v4-fragments-refuse-the-include-tree
func TestV4OnlyDirectWritersValidateIncludeTree(t *testing.T) {
	for _, writer := range []struct {
		name string
		run  func(string) error
	}{
		{"harnesses", func(path string) error { return SetHarnessesInConfig(path, []string{"claude"}) }},
		{"telemetry", func(path string) error { return SetUserTelemetryLevel(path, "off") }},
		{"ref", func(path string) error {
			_, err := UpdateRegistryRefInConfig(path, "https://example.test/agents.git", "main", "release")
			return err
		}},
	} {
		t.Run(writer.name, func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			path := fixture.paths[UserPersonal]
			rootBefore := v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: fragment.yaml\nsources: {agents: {git: https://example.test/agents.git, ref: main}}\n")
			fragment := filepath.Join(filepath.Dir(path), "fragment.yaml")
			fragmentBefore := v4OnlyWriteFixture(t, fragment, "apiVersion: agent-marketplace/v3\nresources: {agents: [skill/from-fragment]}\n")
			v4OnlyAssertDiagnostic(t, writer.run(path), "v4.api-version", fragment)
			v4OnlyAssertUnchanged(t, path, rootBefore)
			v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 2 {
				t.Fatalf("writer residue: %v %v", entries, err)
			}
			v4OnlyWriteFixture(t, fragment, "resources: {agents: [skill/from-fragment]}\n")
			if err := writer.run(path); err != nil {
				t.Fatalf("valid included selection control: %v", err)
			}
		})
	}
}
