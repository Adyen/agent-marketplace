package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// A broad install writes a broad selection, and a broad selection never enables
// hooks or MCP servers. Listing them as installed would promise a
// materialization the resolver refuses, so `install --all` withholds them and
// says so.
func TestInstallAllWithholdsExecutableKinds(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "executables.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"prometheus-mcp":{"command":"npx","args":["-y","@promo/mcp"]}}}`)
	commitAndPushAll(t, working, "Add a skill and an MCP server")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err != nil {
		t.Fatalf("expected install --all to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --all: %v", err)
	}
	entry := cfg.Registries[0]
	if len(entry.Skills.Include) != 1 || entry.Skills.Include[0] != "deploy-helper" {
		t.Fatalf("expected the skill to be installed, got %#v", entry.Skills.Include)
	}
	if len(entry.MCP.Include) != 0 {
		t.Fatalf("install --all must not select MCP servers, got %#v", entry.MCP.Include)
	}

	if strings.Contains(strings.SplitN(output, "Not installed", 2)[0], "prometheus-mcp") {
		t.Fatalf("withheld MCP server must not be reported as installed:\n%s", output)
	}
	for _, want := range []string{"Not installed", "prometheus-mcp", "install --kind mcp prometheus-mcp"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

// --kind is the deliberate act: it still installs the executable resource.
func TestInstallAllWithKindMCPStillInstalls(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "executables-kind.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"prometheus-mcp":{"command":"npx","args":["-y","@promo/mcp"]}}}`)
	commitAndPushAll(t, working, "Add a skill and an MCP server")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all", "--kind", "mcp")
	if err != nil {
		t.Fatalf("expected install --all --kind mcp to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}
	if got := cfg.Registries[0].MCP.Include; len(got) != 1 || got[0] != "prometheus-mcp" {
		t.Fatalf("expected mcp include [prometheus-mcp], got %#v", got)
	}
}

// An ad-hoc bundle install of an executable kind is a deliberate act, so it
// writes the explicit includes that kind needs. It used to write only
// `default: true` — which never selects an MCP server — and still report the
// server as installed, so the success message promised a materialization the
// resolver would never perform.
func TestInstallFromBundleWithExecutableKindWritesExplicitIncludes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "adhoc-executables.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"prometheus-mcp":{"command":"npx","args":["-y","@promo/mcp"]}}}`)
	commitAndPushAll(t, working, "Add a skill and an MCP server")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", origin, "--ref", "main", "--kind", "mcp")
	if err != nil {
		t.Fatalf("expected install --from --kind mcp to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Installed prometheus-mcp") {
		t.Fatalf("expected the MCP server to be reported as installed, got:\n%s", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}
	entry := cfg.Registries[0]
	if got := entry.MCP.Include; len(got) != 1 || got[0] != "prometheus-mcp" {
		t.Fatalf("expected an explicit mcp include, got %#v", got)
	}
	if entry.Default {
		t.Fatal("default:true selects no MCP server, so an executable-kind install must not rely on it")
	}

	// The report is only true if the server is actually materialized.
	mcpConfig := filepath.Join(repoRoot, ".factory", "mcp.json")
	data, err := os.ReadFile(mcpConfig)
	if err != nil {
		t.Fatalf("expected %s to be materialized: %v", mcpConfig, err)
	}
	if !strings.Contains(string(data), "prometheus-mcp") {
		t.Fatalf("expected the MCP server in %s, got:\n%s", mcpConfig, data)
	}
	if out, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("expected the installed selection to be in sync: %v\n%s", err, out)
	}
}

// writeExecutableCollectionRegistry creates a registry publishing one skill, one
// MCP server, and a collection that carries both.
func writeExecutableCollectionRegistry(t *testing.T, name, collectionName string) string {
	t.Helper()
	origin, working := createBareRepoWithWorkingClone(t, name)
	writeSkill(t, working, "safe-skill", "Safe skill", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"bundled-mcp":{"command":"npx","args":["-y","@bundled/mcp"]}}}`)
	writeCollection(t, working, collectionName, `
name: `+collectionName+`
version: "1.0"
resources:
  - name: safe-skill
  - name: bundled-mcp
    kind: mcp
`)
	commitAndPushAll(t, working, "Add a collection carrying an MCP server")
	return origin
}

// Installing a collection is the deliberate act that vouches for the hooks and
// MCP servers it carries, and that vouch names one registry. The selection is
// therefore written onto the providing registry entry: a top-level name says
// which bundle to expand but not whose, so it enables no executables.
func TestInstallCollectionWritesProviderScopedInclude(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin := writeExecutableCollectionRegistry(t, "collection-trust.git", "dev-bundle")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "dev-bundle")
	if err != nil {
		t.Fatalf("expected install dev-bundle to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}
	if len(cfg.Collections.Include) != 0 {
		t.Fatalf("a top-level collection name vouches for no provider, so none must be written: %#v", cfg.Collections.Include)
	}
	if got := cfg.Registries[0].Collections.Include; len(got) != 1 || got[0] != "dev-bundle" {
		t.Fatalf("expected the collection to be named on the providing entry, got %#v", got)
	}

	assertFactorySkillExists(t, repoRoot, "safe-skill")
	data, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "mcp.json"))
	if err != nil {
		t.Fatalf("expected the installed collection's MCP server to be materialized: %v", err)
	}
	if !strings.Contains(string(data), "bundled-mcp") {
		t.Fatalf("expected bundled-mcp in .factory/mcp.json, got:\n%s", data)
	}
	if out, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("expected the installed collection to be in sync: %v\n%s", err, out)
	}
}

// A legacy top-level `collections: include:` still selects the collection — its
// skills expand — but names no registry, so it cannot say whose executables were
// accepted. The exploit this closes: a personal config carrying such a name, and
// a repository that adds the registry publishing a bundle under it.
func TestTopLevelCollectionIncludeWithholdsExecutables(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin := writeExecutableCollectionRegistry(t, "collection-toplevel.git", "dev-bundle")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - dev-bundle
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("withholding must not fail the sync: %v\n%s", err, output)
	}

	assertFactorySkillExists(t, repoRoot, "safe-skill")
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "mcp.json")); err == nil {
		t.Fatal("a top-level collection name must not enable the bundle's MCP server")
	}
	for _, want := range []string{"bundled-mcp", "was not enabled", "onto the registry entry you meant"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in sync output:\n%s", want, output)
		}
	}
}

// Which registry a collection name refers to may not be guessed when installing
// it also enables the executables it carries.
func TestInstallCollectionRefusesAmbiguousPublisher(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	first := writeExecutableCollectionRegistry(t, "collection-ambiguous-a.git", "dev-bundle")
	second := writeExecutableCollectionRegistry(t, "collection-ambiguous-b.git", "dev-bundle")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+first+`
    ref: main
  - url: `+second+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "dev-bundle")
	if err == nil {
		t.Fatalf("expected the ambiguous collection install to be refused, got:\n%s", output)
	}
	for _, want := range []string{"published by 2 configured registries", "--from"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error, got: %v", want, err)
		}
	}
}

// install --all does not enable executables, but what discovery found out about
// them must still be reported: a name two registries answer to is the ambiguity
// a later 'install --kind' has to resolve, and it used to be dropped along with
// the names themselves.
func TestInstallAllReportsExecutableCollision(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	makeRegistry := func(name, skillName string) string {
		origin, working := createBareRepoWithWorkingClone(t, name)
		writeSkill(t, working, skillName, "Skill "+skillName, nil)
		writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"shared-mcp":{"command":"npx","args":["-y","@shared/mcp"]}}}`)
		commitAndPushAll(t, working, "Add a skill and a shared MCP server name")
		return origin
	}
	first := makeRegistry("collision-a.git", "skill-a")
	second := makeRegistry("collision-b.git", "skill-b")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+first+`
    ref: main
  - url: `+second+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err == nil {
		t.Fatalf("expected the executable collision to be surfaced, got:\n%s", output)
	}
	for _, want := range []string{"shared-mcp", "multiple registries"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error, got: %v", want, err)
		}
	}
}

func TestReportWithheldResourcesNamesAnInstallCommand(t *testing.T) {
	var out bytes.Buffer
	err := reportWithheldResources(&out, []pipeline.WithheldResource{
		{Kind: ir.KindHook, Name: "pre-commit-scan"},
		{Kind: ir.KindMcpServer, Name: "prometheus-mcp"},
	}, "git@example.com:team/tools.git")
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"hooks and MCP servers are never enabled by a broad install",
		"hook pre-commit-scan",
		"mcp prometheus-mcp",
		"install --kind hook pre-commit-scan --from git@example.com:team/tools.git",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}

	out.Reset()
	if err := reportWithheldResources(&out, nil, ""); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output when nothing was withheld, got %q", out.String())
	}
}

func TestReportDroppedRequiredNamesEveryEntry(t *testing.T) {
	var out bytes.Buffer
	err := reportDroppedRequired(&out, []config.DroppedRequiredRegistry{
		{Layer: "user personal", Path: "/home/dev/.agents/config.yaml", URL: "git@example.com:org/security.git"},
		{Layer: "repository committed", Path: "/repo/.agents/config.yaml", URL: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Removed 'required: true' from 2 registry entries",
		"git@example.com:org/security.git (user personal config: /home/dev/.agents/config.yaml)",
		"<entry without url> (repository committed config: /repo/.agents/config.yaml)",
		config.OrganizationConfigPath(),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}

	out.Reset()
	if err := reportDroppedRequired(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output when nothing was dropped, got %q", out.String())
	}
}
