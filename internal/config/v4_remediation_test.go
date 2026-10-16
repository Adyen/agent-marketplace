package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4OrderedChildExcludeThenRootIncludeSurvivesRuntimeAdapter(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(dir, "fragment.yaml")
	if err := os.WriteFile(fragment, []byte(`sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {exclude: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: fragment.yaml
sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {include: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if len(entry.Skills.Include) != 1 || entry.Skills.Include[0] != "review" {
		t.Fatalf("effective include = %#v", entry.Skills.Include)
	}
	if len(entry.Skills.Exclude) != 0 {
		t.Fatalf("stale exclusion survived later include: %#v", entry.Skills.Exclude)
	}
	if len(entry.V4Decisions) != 2 || entry.V4Decisions[0].Action != V4SelectionExclude || entry.V4Decisions[1].Action != V4SelectionExplicit {
		t.Fatalf("ordered decisions = %#v", entry.V4Decisions)
	}
}

func TestV4RequiredOnlyDoesNotAutoSelectCatalog(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.test/agents.git, ref: main}}
resources: {shared: {required: true}}
`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if entry.Required || entry.Default || len(entry.V4Decisions) != 0 {
		t.Fatalf("required-only adapter = %#v", entry)
	}
	if len(entry.Skills.Include) != 0 && entry.Skills.Include != nil {
		t.Fatalf("unexpected selected skills: %#v", entry.Skills.Include)
	}
}

func TestV4AllSelectionSurvivesRuntimeAdapter(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.test/agents.git, ref: main}}
resources:
  shared:
    include: [skill/keep]
    tags: [platform]
    all: true
    exclude: [skill/blocked]
`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if !entry.V4All || !entry.Default {
		t.Fatalf("all-selection adapter lost v4 all marker: %#v", entry)
	}
	if len(entry.V4Decisions) != 2 || entry.V4Decisions[0].Action != V4SelectionExplicit || entry.V4Decisions[1].Action != V4SelectionExclude {
		t.Fatalf("all-selection decisions = %#v", entry.V4Decisions)
	}
	merged := MergeLayers([]Layer{{Source: RepoCommitted, Config: legacy}})
	if len(merged.Registries) != 1 || !merged.Registries[0].V4All {
		t.Fatalf("all-selection marker lost during layer merge: %#v", merged.Registries)
	}
}

func TestV4DirectoryKindSurvivesGitSuffixedResolvedPath(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "registry.git")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  local: {directory: registry.git}
resources:
  local: [skill/review]
`), filepath.Join(dir, "config.yaml"), true)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if !entry.IsLocal() || entry.V4Kind != V4SourceDirectory {
		t.Fatalf("directory source reclassified: %#v", entry)
	}
	resolved, err := entry.ResolveLocalPath(dir)
	if err != nil || resolved != sourceDir {
		t.Fatalf("resolved directory = %q, %v", resolved, err)
	}
}

func TestV4LocalRejectsAuxiliaryLayerFields(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
logging: {level: debug}
sources: {shared: {git: https://example.test/agents.git, ref: main}}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfig(localLayer)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.layer-field" {
		t.Fatalf("error = %v, want v4.layer-field", err)
	}
}

// spec: upgrade-notice/repository-configuration-cannot-set-the-release-check
func TestV4RepositoryLayersRejectUpdateCheck(t *testing.T) {
	v4, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nupdate_check: {enabled: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []layerKind{repoLayer, localLayer} {
		_, err = v4.ToLegacyConfig(kind)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || validation.Path != "update_check" {
			t.Fatalf("layer %v: error = %v, want v4.layer-field for update_check", kind, err)
		}
	}
}

func TestV4OrganizationRejectsMaterializationLayerField(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
materialization: {mode: file}
sources: {shared: {git: https://example.test/agents.git, ref: main}}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfig(organizationLayer)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || validation.Path != "materialization" {
		t.Fatalf("error = %v, want v4.layer-field for materialization", err)
	}
}

// spec: consumer-config-format/mutations-refuse-before-new-writes-or-prunes
func TestLayerEditorRefusesLegacyRootWithoutChangingFiles(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
			t.Run(source.String()+"/"+version, func(t *testing.T) {
				dir := t.TempDir()
				declaring := filepath.Join(dir, "config.yaml")
				staged := filepath.Join(dir, "staged.yaml")
				before := []byte("apiVersion: " + version + "\nregistries: []\n")
				for _, path := range []string{declaring, staged} {
					if err := os.WriteFile(path, before, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				info, err := os.Stat(staged)
				if err != nil {
					t.Fatal(err)
				}
				backup := filepath.Join(dir, "config.yaml.pre-v4.existing.bak")
				writeIncludeConfig(t, backup, "foreign backup\n")
				_, err = OpenLayerEditor(LayerTarget{Source: source, StagedPath: staged, DeclaringPath: declaring})
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != declaring || validation.Line != 1 {
					t.Fatalf("editor format refusal = %v", err)
				}
				for _, path := range []string{declaring, staged} {
					if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
						t.Fatalf("refusal changed %s: %q %v", path, got, err)
					}
				}
				after, err := os.Stat(staged)
				if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
					t.Fatalf("refusal changed metadata: %v", err)
				}
				if got := readConfigString(t, backup); got != "foreign backup\n" {
					t.Fatalf("foreign backup changed: %q", got)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 3 {
					t.Fatalf("refusal left residue: %v %v", entries, err)
				}
			})
		}
	}
}

func TestV4ReaderPreservesExplicitEmptyTagsAndHarnesses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, path, `apiVersion: agent-marketplace/v4
sources: {agents: {git: https://example.test/agents.git, ref: main}}
resources: {agents: {tags: [], harnesses: []}}
`)
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	group := v4.Groups["agents"]
	if !group.HasTags || group.Tags == nil || len(group.Tags) != 0 || !group.HasHarnesses || group.Harnesses == nil || len(group.Harnesses) != 0 {
		t.Fatalf("empty overrides lost: %#v", group)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if entry.Tags == nil || entry.Harnesses == nil {
		t.Fatalf("empty overrides became nil: tags=%#v harnesses=%#v", entry.Tags, entry.Harnesses)
	}
}

func TestV4EditorRejectsForeignSourceBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := "apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/review]}\n"
	writeIncludeConfig(t, path, before)
	first, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := first.EnsureRegistry(RegistryEntry{URL: "https://example.test/shared.git", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	change := SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{{Kind: ir.KindSkill, Name: "review"}}}
	err = second.Apply(binding, change)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "config.edit.invalid-binding" {
		t.Fatalf("foreign binding accepted: %v", err)
	}
	candidate, err := second.Render()
	if err != nil || candidate.Changed || string(candidate.Bytes) != before {
		t.Fatalf("foreign binding mutated candidate: %#v %v", candidate, err)
	}
	if err := first.Apply(binding, change); err != nil {
		t.Fatal(err)
	}
	candidate, err = first.Render()
	if err != nil || !candidate.Changed || string(candidate.Bytes) == before {
		t.Fatalf("own binding positive control failed: %#v %v", candidate, err)
	}
	view, err := ParseV4Config(candidate.Bytes, path, true)
	if err != nil || len(view.Groups["shared"].Include) != 0 || len(view.Groups["shared"].Exclude) != 1 || view.Groups["shared"].Exclude[0] != "skill/review" {
		t.Fatalf("own binding did not exclude review: view=%#v err=%v", view, err)
	}
	if got := readConfigString(t, path); got != before {
		t.Fatalf("candidate render wrote root: %q", got)
	}
}
