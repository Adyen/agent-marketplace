package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func TestStatusV4LocalExclusionsAreSourceScoped(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	for _, identity := range []string{"url", "ref", "path"} {
		t.Run(identity, func(t *testing.T) {
			origin, working := createBareRepoWithWorkingClone(t, "status-source.git")
			writeSkill(t, working, "desired", "Desired", nil)
			writeSkill(t, working, "wanted", "Wanted", nil)
			writeSkill(t, filepath.Join(working, "nested"), "desired", "Nested desired", nil)
			writeSkill(t, filepath.Join(working, "nested"), "wanted", "Nested wanted", nil)
			commitAndPushAll(t, working, "source resources")
			firstSource := "{git: file://" + origin + ", ref: main}"
			secondSource := ""
			switch identity {
			case "url":
				other, clone := createBareRepoWithWorkingClone(t, "status-other.git")
				writeSkill(t, clone, "desired", "Other desired", nil)
				writeSkill(t, clone, "wanted", "Other wanted", nil)
				commitAndPushAll(t, clone, "other resources")
				secondSource = "{git: file://" + other + ", ref: main}"
			case "ref":
				runGit(t, working, "branch", "other")
				runGit(t, working, "push", "origin", "other")
				secondSource = "{git: file://" + origin + ", ref: other}"
			case "path":
				firstSource = "{git: file://" + origin + ", ref: main, path: skills}"
				secondSource = "{git: file://" + origin + ", ref: main, path: nested/skills}"
			}
			repo := t.TempDir()
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources:
  first: `+firstSource+`
  second: `+secondSource+`
resources:
  first: [skill/desired]
  second: [skill/wanted]
`)
			// Both sources contain both names. Only the matching source identity
			// may suppress a selection, irrespective of the layer-local name.
			writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
sources:
  renamed: `+firstSource+`
resources:
  renamed: {exclude: [skill/desired, skill/wanted]}
`)
			withWorkingDir(t, repo)
			requireExactLocks(t)
			output, err := executeCommand("status")
			if err != nil {
				t.Fatalf("status: %v\n%s", err, output)
			}
			requireStatusV4Excluded(t, output, []string{"desired|file://" + origin + "|main"})
			synced := statusSection(output, "SYNCED RESOURCES")
			if !strings.Contains(synced, "wanted") || strings.Contains(synced, "desired") {
				t.Fatalf("source-scoped exclusion changed the wrong desired resources:\n%s", output)
			}
		})
	}
}

func TestStatusV4LocalExclusionsRespectMachineAndRequiredResources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	shared, required, org := t.TempDir(), t.TempDir(), t.TempDir()
	writeSkill(t, shared, "shared", "Shared across scopes", nil)
	writeSkill(t, shared, "machine-only", "Machine only", nil)
	writeSkill(t, required, "mandatory", "Required repository selection", nil)
	writeSkill(t, org, "org-mandatory", "Required organization selection", nil)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  managed: {directory: `+org+`}
resources:
  managed: {include: [skill/org-mandatory], required: true}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  machine: {directory: `+shared+`}
resources:
  machine: [skill/shared, skill/machine-only]
`)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources:
  project: {directory: `+shared+`}
  required: {directory: `+required+`}
resources:
  project: [skill/shared]
  required: {include: [skill/mandatory], required: true}
`)
	writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
sources:
  shared: {directory: `+shared+`}
  required: {directory: `+required+`}
  org: {directory: `+org+`}
resources:
  shared: {exclude: [skill/shared, skill/machine-only]}
  required: {exclude: [skill/mandatory]}
  org: {exclude: [skill/org-mandatory]}
`)
	withWorkingDir(t, repo)
	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, output)
	}
	// The machine-scoped copy must not mask the excluded repository copy.
	// Required and machine-only exclusions have no effect and get no row.
	requireStatusV4Excluded(t, output, []string{"shared|" + shared})
	_, synced := parseTable(t, statusSection(output, "SYNCED RESOURCES"))
	if len(synced) != 4 {
		t.Fatalf("synced rows = %v, want shared, machine-only, mandatory, org-mandatory", synced)
	}
	for _, name := range []string{"shared", "machine-only", "mandatory", "org-mandatory"} {
		if !strings.Contains(strings.Join(synced, "\n"), "|"+name+"|") {
			t.Fatalf("status suppressed immune resource %s:\n%s", name, output)
		}
	}
}

func TestStatusV4LocalExclusionsKeepOrderedAndDynamicSelections(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		name, selection, exclusion string
		wantExcluded               []string
		blockedSurvives            bool
		commandSurvives            bool
	}{
		{name: "all-kind-wildcard", selection: "all: true", exclusion: "skill/*", wantExcluded: []string{"blocked", "other"}, commandSurvives: true},
		{name: "all-source-wildcard", selection: "all: true", exclusion: "*", wantExcluded: []string{"blocked", "build", "other"}},
		{name: "tagged", selection: "tags: [ops]", exclusion: "skill/blocked", wantExcluded: []string{"blocked"}},
		{name: "local-required", selection: "all: true\n    required: true", exclusion: "*", blockedSurvives: true, commandSurvives: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := t.TempDir()
			writeSkill(t, registry, "blocked", "Blocked", []string{"ops"})
			writeSkill(t, registry, "other", "Other", []string{"ops"})
			writeSkill(t, registry, "restored", "Explicitly restored", []string{"ops"})
			writeCommand(t, registry, "build", "Build", "Build the project.")
			repo := t.TempDir()
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
			writeFile(t, filepath.Join(repo, ".agents", "fragment.yaml"), `sources:
  tools: {directory: `+registry+`}
resources:
  tools:
    `+tc.selection+`
    exclude: ["`+tc.exclusion+`", skill/restored]
`)
			writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
include: fragment.yaml
sources:
  tools: {directory: `+registry+`}
resources:
  tools: [skill/restored]
`)
			withWorkingDir(t, repo)
			output, err := executeCommand("status")
			if err != nil {
				t.Fatalf("status: %v\n%s", err, output)
			}
			var want []string
			for _, name := range tc.wantExcluded {
				want = append(want, name+"|"+registry)
			}
			requireStatusV4Excluded(t, output, want)
			synced := statusSection(output, "SYNCED RESOURCES")
			if !strings.Contains(synced, "restored") || strings.Contains(synced, "blocked") != tc.blockedSurvives ||
				strings.Contains(synced, "build") != tc.commandSurvives {
				t.Fatalf("ordered local include/exclude semantics lost:\n%s", output)
			}
		})
	}
}

func TestStatusV4LocalExclusionsPreserveRepositoryExclusions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	registry, repo := t.TempDir(), t.TempDir()
	for _, name := range []string{"repo-excluded", "local-excluded", "kept"} {
		writeSkill(t, registry, name, name, nil)
	}
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources:
  project: {directory: `+registry+`}
resources:
  project: {all: true, exclude: [skill/repo-excluded]}
`)
	writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
sources:
  personal: {directory: `+registry+`}
resources:
  personal: {exclude: [skill/local-excluded]}
`)
	withWorkingDir(t, repo)
	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, output)
	}
	requireStatusV4Excluded(t, output, []string{"local-excluded|" + registry})
	_, rows := parseTable(t, statusSection(output, "SYNCED RESOURCES"))
	if len(rows) != 1 || !strings.Contains(rows[0], "|kept|") {
		t.Fatalf("repository exclusion did not remain effective:\n%s", output)
	}
}

func TestStatusV4LocalExclusionProbeIsReadOnly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	base := t.TempDir()
	home, repo := filepath.Join(base, "home"), filepath.Join(base, "repo")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	server, artifactDownloads := newCountingV4MarketplaceServer(t, "status-probe", "marketplace-kept")
	defer server.Close()
	origin, working := createBareRepoWithWorkingClone(t, "status-read-only.git")
	writeSkill(t, working, "blocked", "Blocked", nil)
	writeSkill(t, working, "kept", "Kept", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"kept-mcp":{"command":"echo","args":["safe"]}}}`)
	commitAndPushAll(t, working, "locked resources")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [gemini]
sources:
  locked: {git: file://`+origin+`, ref: main}
  hosted: {marketplace: `+server.URL+`}
resources:
  locked: {all: true}
  hosted: [tools/skill/marketplace-kept]
`)
	writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
sources:
  same: {git: file://`+origin+`, ref: main}
resources:
  same: {exclude: [skill/blocked]}
`)
	withWorkingDir(t, repo)
	instrumentSyncGit(t, `
case " $* " in
  *" clone "*|*" fetch "*|*" ls-remote "*) printf '%s\n' "$*" >> "$AM_TEST_GIT_LOG" ;;
esac`)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed native output and exact locks: %v\n%s", err, output)
	}
	downloadsBefore := artifactDownloads.Load()
	if downloadsBefore == 0 {
		t.Fatal("positive control: sync did not download the selected Marketplace artifact")
	}
	gitLog := os.Getenv("AM_TEST_GIT_LOG")
	if data := readTestFile(t, gitLog); len(data) == 0 {
		t.Fatal("positive control: sync did not exercise acquisition instrumentation")
	}
	if err := os.WriteFile(gitLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Move upstream after locking: the probe must keep the admitted snapshot,
	// not reacquire a newer catalog when restoring a suppressed selection.
	writeSkill(t, working, "new-upstream", "Not locked", nil)
	commitAndPushAll(t, working, "advance upstream")
	store, err := ownership.Open(filepath.Join(repo, ".gemini"))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(repo, ".agents", "config.yaml"),
		filepath.Join(repo, ".agents", "config.local.yaml"),
		filepath.Join(repo, ".agents", "lock.yaml"),
		filepath.Join(repo, ".agents", "lock.local.yaml"),
		filepath.Join(repo, ".gemini", "skills", "kept", "SKILL.md"),
		filepath.Join(repo, ".gemini", "settings.json"),
		store.StateFile(),
	}
	// Check metadata as well as bytes to catch same-byte rewrites.
	before := ownedCommandSnapshot(t, base)
	type snapshot struct {
		data []byte
		info os.FileInfo
	}
	files := make(map[string]snapshot, len(paths))
	for _, path := range paths {
		data, info := statSnapshot(t, path)
		files[path] = snapshot{data: data, info: info}
	}
	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, output)
	}
	requireStatusV4Excluded(t, output, []string{"blocked|file://" + origin + "|main"})
	synced := statusSection(output, "SYNCED RESOURCES")
	if !strings.Contains(synced, "kept-mcp") || !strings.Contains(synced, "marketplace-kept") || strings.Contains(output, "new-upstream") {
		t.Fatalf("status did not use the locked resource set:\n%s", output)
	}
	if data := readTestFile(t, gitLog); len(data) != 0 {
		t.Fatalf("status performed remote acquisition:\n%s", data)
	}
	if got := artifactDownloads.Load(); got != downloadsBefore {
		t.Fatalf("status downloaded %d extra Marketplace artifacts", got-downloadsBefore)
	}
	for path, file := range files {
		assertStatUnchanged(t, "status", path, file.data, file.info)
	}
	if after := ownedCommandSnapshot(t, base); !reflect.DeepEqual(before, after) {
		t.Fatal("status changed configuration, locks, cache, native output, or state")
	}
}

// BenchmarkStatusV4LocalExclusionInputCopy measures only the in-memory probe
// input copy, not catalog discovery, resolution, materialization, or network I/O.
func BenchmarkStatusV4LocalExclusionInputCopy(b *testing.B) {
	cfg := &config.Config{}
	for _, source := range []config.Source{config.Organization, config.UserPersonal, config.RepoCommitted, config.RepoPersonal} {
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL: "https://example.test/agents.git", Ref: "main", Source: source,
			V4Kind: config.V4SourceGit, V4All: true,
			Skills: config.KindSelector{Exclude: []string{"blocked"}},
			V4Decisions: []config.V4SelectionDecision{{
				Action: config.V4SelectionExclude, Exclusion: "skill/blocked",
			}},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, changed := statusWithoutLocalExclusions(cfg); !changed {
			b.Fatal("missing personal-local exclusion")
		}
	}
}

func requireStatusV4Excluded(t *testing.T, output string, want []string) {
	t.Helper()
	body := statusSection(output, "LOCALLY EXCLUDED")
	if len(want) == 0 {
		if body != "" {
			t.Fatalf("ineffective exclusions were reported as locally excluded:\n%s", output)
		}
		return
	}
	if body == "" {
		t.Fatalf("missing LOCALLY EXCLUDED section:\n%s", output)
	}
	header, rows := parseTable(t, body)
	if !reflect.DeepEqual(header, []string{"NAME", "REGISTRY", "REF"}) || !reflect.DeepEqual(rows, want) {
		t.Fatalf("LOCALLY EXCLUDED header=%v rows=%v, want rows=%v\n%s", header, rows, want, output)
	}
}
