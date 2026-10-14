package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func TestV4OrderedChildExcludeThenRootIncludeSurvivesLegacyAdapter(t *testing.T) {
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

func TestV4AllSelectionSurvivesLegacyAdapter(t *testing.T) {
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

func TestV4MigrationPublishesAllFilesInOneTransaction(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	repo := filepath.Join(dir, "repo.yaml")
	userBefore := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"
	repoBefore := "apiVersion: agent-marketplace/v2\nregistries: []\n"
	if err := os.WriteFile(user, []byte(userBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo, []byte(repoBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	original := commitV4Migration
	commitV4Migration = func(context.Context, string, []dependencytxn.DesiredFile, ...dependencytxn.Option) (dependencytxn.Result, error) {
		return dependencytxn.Result{}, errors.New("injected publication failure")
	}
	t.Cleanup(func() { commitV4Migration = original })
	_, err := MigrateConfigFilesToV4(MigratePaths{User: user, Repo: repo}, V4MigrationAll, false)
	if err == nil || !strings.Contains(err.Error(), "commit v4 migration transaction") {
		t.Fatalf("error = %v, want injected publication failure", err)
	}
	for path, want := range map[string]string{user: userBefore, repo: repoBefore} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s after failed transaction = %q, %v", path, got, readErr)
		}
		matches, globErr := filepath.Glob(path + ".pre-v4.*.bak")
		if globErr != nil || len(matches) != 0 {
			t.Fatalf("%s backups after failed transaction = %v, %v", path, matches, globErr)
		}
	}
}

func TestV4MigrationDoesNotOverwriteConcurrentBackup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	original := commitV4Migration
	sentinel := []byte("existing backup\n")
	var backupPath string
	commitV4Migration = func(ctx context.Context, stateDir string, files []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		for _, file := range files {
			if strings.Contains(file.Path, ".pre-v4.") {
				backupPath = file.Path
				if err := os.WriteFile(backupPath, sentinel, 0o600); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		return original(ctx, stateDir, files, opts...)
	}
	t.Cleanup(func() { commitV4Migration = original })

	_, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v, want generation conflict", err)
	}
	if backupPath == "" || readConfigString(t, backupPath) != string(sentinel) {
		t.Fatalf("backup %q was overwritten", backupPath)
	}
	if got := readConfigString(t, path); got != string(before) {
		t.Fatalf("config changed after backup collision: %q", got)
	}
}

func TestV4MigrationPreservesExplicitEmptyTagsAndHarnesses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeV2Config(t, path, `apiVersion: agent-marketplace/v2
registries:
  - url: https://example.test/agents.git
    ref: main
    tags: []
    harnesses: []
`)
	if _, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false); err != nil {
		t.Fatal(err)
	}
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
