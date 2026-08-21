package main

import (
	"bytes"
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
