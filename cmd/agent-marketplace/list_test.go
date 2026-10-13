package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestBuildListRowsUsesLoadedExactCatalogKey(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const exactCatalogKey = "/cache/registry@sha1:0123456789abcdef"
	const registryURL = "https://example.com/agents.git"

	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:        registryURL,
		Ref:        "main",
		CatalogKey: exactCatalogKey,
	}}}
	catalogs := map[string]loadedCatalog{
		exactCatalogKey: {
			Entries: []catalog.Entry{{
				Kind: ir.KindSkill,
				Name: "locked-skill",
				Tags: []string{"exact"},
			}},
		},
	}
	resolved := []resolver.ResolvedArtifact{{
		Entry:       catalogs[exactCatalogKey].Entries[0],
		RegistryURL: registryURL,
		Ref:         "main",
		CatalogKey:  exactCatalogKey,
	}}

	rows := buildListRows(cfg, catalogs, resolved)
	if len(rows) != 1 {
		t.Fatalf("expected one list row, got %d: %#v", len(rows), rows)
	}
	if got, want := rows[0].Status, "enabled"; got != want {
		t.Fatalf("exact catalog entry status = %q, want %q", got, want)
	}
	if got, want := rows[0].Name, "locked-skill"; got != want {
		t.Fatalf("exact catalog entry name = %q, want %q", got, want)
	}
}

func TestListCommandPrintsCatalogWithStatuses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	infraOrigin, infraWorking := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, infraWorking, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
	writeSkill(t, infraWorking, "k8s-app-onboarding", "Kubernetes app onboarding", []string{"k8s", "helm"})
	writeSkill(t, infraWorking, "firewall-rule-lifecycle", "Firewall lifecycle", []string{"network", "security"})
	commitAndPushAll(t, infraWorking, "Add infra skills")

	securityOrigin, securityWorking := createBareRepoWithWorkingClone(t, "example-security-skills.git")
	writeSkill(t, securityWorking, "no-curl-bash", "No curl bash", []string{"security"})
	writeSkill(t, securityWorking, "shell-hardening", "Shell hardening", []string{"security"})
	commitAndPushAll(t, securityWorking, "Add security skills")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+securityOrigin+`
    ref: main
    required: true
    skills:
      - no-curl-bash
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+infraOrigin+`
    ref: main
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}

	header, rows := parseTable(t, output)
	if got, want := strings.Join(header, "|"), "REGISTRY|KIND|NAME|TAGS (metadata)|STATUS|VIA"; got != want {
		t.Fatalf("expected header %q, got %q\nfull output:\n%s", want, got, output)
	}

	sort.Strings(rows)
	wantRows := []string{
		"example-infra-skills|skill|firewall-rule-lifecycle|network,security|available",
		"example-infra-skills|skill|k8s-app-onboarding|k8s,helm|available",
		"example-infra-skills|skill|vm-lifecycle|openstack,puppet|enabled",
		"example-security-skills|skill|no-curl-bash|security|required (org)",
		"example-security-skills|skill|shell-hardening|security|required (org)",
	}
	sort.Strings(wantRows)

	if strings.Join(rows, "\n") != strings.Join(wantRows, "\n") {
		t.Fatalf("expected rows:\n%s\n\ngot:\n%s\n\nfull output:\n%s", strings.Join(wantRows, "\n"), strings.Join(rows, "\n"), output)
	}
}

func TestListCommandDeduplicatesRegistriesDifferingOnlyByDotGitSuffix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "awesome-ai.git")
	writeSkill(t, working, "brainstorming", "Brainstorming", []string{"planning"})
	writeSkill(t, working, "efficient-go", "Efficient Go", []string{"go"})
	commitAndPushAll(t, working, "Add skills")

	// Global config references the bare repo and marks the registry as
	// required. Repo config references the same upstream with a specific
	// skill selection. Both entries should collapse into a single row per
	// skill; required status wins.
	// Note: local bare git repos must use the .git suffix convention;
	// without it, IsLocal() treats the path as a plain directory.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    required: true
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - efficient-go
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}

	_, rows := parseTable(t, output)
	sort.Strings(rows)
	wantRows := []string{
		"awesome-ai|skill|brainstorming|planning|required (org)",
		"awesome-ai|skill|efficient-go|go|required (org)",
	}
	sort.Strings(wantRows)

	if strings.Join(rows, "\n") != strings.Join(wantRows, "\n") {
		t.Fatalf("expected rows:\n%s\n\ngot:\n%s\n\nfull output:\n%s", strings.Join(wantRows, "\n"), strings.Join(rows, "\n"), output)
	}
}

func TestListCommandShowsMalformedSkills(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "awesome-ai.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}

	_, rows := parseTable(t, output)
	sort.Strings(rows)
	wantRows := []string{
		"awesome-ai|skill|broken-skill|malformed: ingest.claude.load-skill-failed: failed to load skill \"skills/broken-skill\": bundle: parse frontmatter skills/broken-skill/SKILL.md: yamlfm: yaml parse: yaml: line 1: did not find expected ',' or ']'",
		"awesome-ai|skill|valid-skill|tested|enabled",
	}
	sort.Strings(wantRows)

	if strings.Join(rows, "\n") != strings.Join(wantRows, "\n") {
		t.Fatalf("expected rows:\n%s\n\ngot:\n%s\n\nfull output:\n%s", strings.Join(wantRows, "\n"), strings.Join(rows, "\n"), output)
	}
}

func TestListCommandShowsDriftFooterWhenOutOfSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "awesome-ai.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	commitAndPushAll(t, working, "Add skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	// No sync yet — skill is enabled in config but not on disk
	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "out of sync") {
		t.Fatalf("expected drift footer in list output, got:\n%s", output)
	}

	// Sync, then list again — footer should disappear
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}
	output, err = executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed after sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "out of sync") {
		t.Fatalf("expected no drift footer after sync, got:\n%s", output)
	}
}

func requireExactLocks(t *testing.T) {
	t.Helper()
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("create exact read lock: %v\n%s", err, output)
	}
}

func parseTable(t *testing.T, output string) ([]string, []string) {
	t.Helper()

	splitter := regexp.MustCompile(`\s{2,}`)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and at least one row, got %q", output)
	}

	header := splitter.Split(strings.TrimSpace(lines[0]), -1)
	rows := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip drift footer lines (e.g. "2 skills out of sync — ...")
		if strings.Contains(line, "out of sync") {
			continue
		}

		columns := splitter.Split(line, -1)
		rows = append(rows, strings.Join(columns, "|"))
	}

	return header, rows
}

func createBareRepoWithWorkingClone(t *testing.T, bareName string) (string, string) {
	return createBareRepoWithWorkingCloneOnBranch(t, bareName, "main")
}

func createBareRepoWithWorkingCloneOnBranch(t *testing.T, bareName, branch string) (string, string) {
	t.Helper()

	baseDir := t.TempDir()
	origin := filepath.Join(baseDir, bareName)
	working := filepath.Join(baseDir, "working")

	runGit(t, "", "init", "--initial-branch="+branch, working)
	runGit(t, "", "init", "--bare", "--initial-branch="+branch, origin)
	disableBareRemoteMaintenance(t, origin)

	gitConfig := filepath.Join(working, ".git", "config")
	config, err := os.OpenFile(gitConfig, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open git config %q: %v", gitConfig, err)
	}
	if _, err := fmt.Fprintf(config, "\n[remote \"origin\"]\n\turl = %s\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n[branch \"%s\"]\n\tremote = origin\n\tmerge = refs/heads/%s\n", filepath.ToSlash(origin), branch, branch); err != nil {
		_ = config.Close()
		t.Fatalf("write git config %q: %v", gitConfig, err)
	}
	if err := config.Close(); err != nil {
		t.Fatalf("close git config %q: %v", gitConfig, err)
	}

	return origin, working
}

func disableBareRemoteMaintenance(t *testing.T, bare string) {
	t.Helper()
	runGit(t, "", "-C", bare, "config", "receive.autogc", "false")
	runGit(t, "", "-C", bare, "config", "maintenance.auto", "false")
}

func commitAndPushAll(t *testing.T, repoDir, message string) {
	commitAndPushAllToBranch(t, repoDir, message, "main")
}

func commitAndPushAllToBranch(t *testing.T, repoDir, message, branch string) {
	t.Helper()

	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", message)
	runGit(t, repoDir, "push", "origin", branch)
}

func writeSkill(t *testing.T, repoDir, name, description string, tags []string) {
	t.Helper()

	// Idempotent layout: callers may pass either a bundle root or a path that
	// already ends in "skills". Avoid producing ".../skills/skills/<name>".
	root := repoDir
	if filepath.Base(repoDir) == "skills" {
		root = filepath.Dir(repoDir)
	}
	skillDir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", skillDir, err)
	}

	content := strings.Builder{}
	content.WriteString("---\n")
	content.WriteString("name: " + name + "\n")
	content.WriteString("description: " + description + "\n")
	content.WriteString("metadata:\n")
	content.WriteString("  tags:\n")
	for _, tag := range tags {
		content.WriteString("    - " + tag + "\n")
	}
	content.WriteString("---\n")
	content.WriteString("\n# " + name + "\n")

	writeFile(t, filepath.Join(skillDir, "SKILL.md"), content.String())
}

func writeSubagent(t *testing.T, repoDir, name, description string, tools []string) {
	t.Helper()

	agentsDir := filepath.Join(repoDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", agentsDir, err)
	}

	content := strings.Builder{}
	content.WriteString("---\n")
	content.WriteString("name: " + name + "\n")
	content.WriteString("description: " + description + "\n")
	content.WriteString("tools:\n")
	for _, tool := range tools {
		content.WriteString("  - " + tool + "\n")
	}
	content.WriteString("---\n")
	content.WriteString("\n# " + name + "\n")

	writeFile(t, filepath.Join(agentsDir, name+".md"), content.String())
}

func writeCommand(t *testing.T, repoDir, name, description, command string) {
	t.Helper()

	commandsDir := filepath.Join(repoDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", commandsDir, err)
	}

	content := strings.Builder{}
	content.WriteString("---\n")
	content.WriteString("name: " + name + "\n")
	content.WriteString("description: " + description + "\n")
	content.WriteString("command: " + command + "\n")
	content.WriteString("---\n")
	content.WriteString("\n# " + name + "\n")

	writeFile(t, filepath.Join(commandsDir, name+".md"), content.String())
}

var (
	// harnessesRegex matches a top-level harnesses list in block or inline form.
	harnessesRegex = regexp.MustCompile(`(?m)^harnesses:[ \t]*(?:\[[^\]]*\][ \t]*(?:\n|$)|(?:\n(?:[ \t]*-[^\n]*(?:\n|$))+))?`)
)

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	content = strings.TrimSpace(content)

	if filepath.Base(path) == "config.yaml" {
		if isGlobalConfigPath(path) {
			// Ensure the machine-wide config carries a target harness set.
			if !strings.Contains(content, "apiVersion:") {
				content = "apiVersion: agent-marketplace/v2\n" + content
			}
			if !strings.Contains(content, "harnesses:") {
				harnessesBlock := "harnesses:\n  - factory\n"
				if data, err := os.ReadFile(path); err == nil {
					if _, existing := extractAndStripHarnesses(string(data)); len(existing) > 0 {
						harnessesBlock = "harnesses:\n  - " + strings.Join(existing, "\n  - ") + "\n"
					}
				}
				content = insertAfterFirstLine(content, harnessesBlock)
			}
		} else if isRepoConfigPath(path) {
			if !strings.Contains(content, "harnesses:") {
				ensureGlobalConfig(t)
			}
			if !strings.Contains(content, "apiVersion:") {
				content = "apiVersion: agent-marketplace/v2\n" + content
			}
		} else if !strings.Contains(content, "apiVersion:") {
			// Other config.yaml files (e.g. organization fixtures) get the legacy header.
			content = "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\n" + content
		}
	}

	writeFile(t, path, content+"\n")
}

func isGlobalConfigPath(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Clean(filepath.Dir(path)) == filepath.Clean(filepath.Join(home, ".agents"))
}

func isRepoConfigPath(path string) bool {
	if filepath.Base(path) != "config.yaml" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	dir := filepath.Clean(filepath.Dir(path))
	return filepath.Base(dir) == ".agents" && dir != filepath.Clean(filepath.Join(home, ".agents"))
}

func insertAfterFirstLine(content, insertion string) string {
	parts := strings.SplitN(content, "\n", 2)
	if len(parts) == 1 {
		return content + "\n" + insertion
	}
	return parts[0] + "\n" + insertion + parts[1]
}

func extractAndStripHarnesses(content string) (string, []string) {
	match := harnessesRegex.FindString(content)
	if match == "" {
		return content, nil
	}
	var parsed struct {
		Harnesses []string `yaml:"harnesses"`
	}
	if err := yaml.Unmarshal([]byte(match), &parsed); err != nil {
		return content, nil
	}
	content = harnessesRegex.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)
	return content, parsed.Harnesses
}

func ensureGlobalConfig(t *testing.T) {
	t.Helper()
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if _, err := os.Stat(globalPath); err == nil {
		return
	}
	writeFile(t, globalPath, "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q: %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd %q: %v", previous, err)
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Provide git identity via env vars so no config files are written.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestListFromDoesNotAcquireConfiguredMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	registry := t.TempDir()
	writeSkill(t, registry, "local-skill", "Local skill", []string{"local"})

	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("list", "--from", registry)
	if err != nil {
		t.Fatalf("list --from failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "local-skill") {
		t.Fatalf("expected local-skill in list output, got:\n%s", output)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("list --from made %d requests to the configured Marketplace", got)
	}
}

// spec: authored-configuration-reads/allowlist-from-unbindable-layer
func TestListFromHonorsAllowlistFromUnbindableV4Layer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	allowedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not a git endpoint", http.StatusNotFound)
	}))
	defer allowedServer.Close()
	var deniedRequests atomic.Int32
	deniedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deniedRequests.Add(1)
		http.Error(w, "not a git endpoint", http.StatusNotFound)
	}))
	defer deniedServer.Close()

	var marketplaceRequests atomic.Int32
	marketplace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		marketplaceRequests.Add(1)
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer marketplace.Close()

	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nallowed_registries:\n  - "+regexp.QuoteMeta(strings.TrimPrefix(allowedServer.URL, "http://"))+"/\nsources:\n  hosted: {marketplace: "+marketplace.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: [never\\.allowed\\.example]\n")

	withWorkingDir(t, t.TempDir())

	_, err := executeCommand("list", "--from", allowedServer.URL+"/repo.git", "--ref", "main")
	if err == nil || strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("allowlist-permitted --from error = %v, want a fetch failure past allowlist validation", err)
	}
	if !strings.Contains(err.Error(), "clone registry") {
		t.Fatalf("allowlist-permitted --from error = %v, want clone failure proving catalog load was reached", err)
	}

	_, err = executeCommand("list", "--from", deniedServer.URL+"/repo.git", "--ref", "main")
	if err == nil || !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("denied --from error = %v, want blocked by allowed_registries", err)
	}
	if got := deniedRequests.Load(); got != 0 {
		t.Fatalf("denied --from URL reached the network %d times", got)
	}
	if got := marketplaceRequests.Load(); got != 0 {
		t.Fatalf("list --from made %d requests to the configured Marketplace", got)
	}
}

func TestListFromFlagListsResourcesAtAdHocRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "list-from-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", []string{"k8s"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("list", "--from", origin, "--ref", "main")
	if err != nil {
		t.Fatalf("list --from failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle in list output, got:\n%s", output)
	}
	if !strings.Contains(output, "k8s-onboarding") {
		t.Fatalf("expected k8s-onboarding in list output, got:\n%s", output)
	}
	if !strings.Contains(output, "available") {
		t.Fatalf("expected 'available' status in output, got:\n%s", output)
	}
}

func TestListFromFlagWithKindFilter(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "list-from-registry.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("list", "--from", origin, "--ref", "main", "--kind", "subagent")
	if err != nil {
		t.Fatalf("list --from --kind subagent failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "worker-generic") {
		t.Fatalf("expected worker-generic in filtered list output, got:\n%s", output)
	}
	if strings.Contains(output, "deploy-helper") {
		t.Fatalf("expected deploy-helper to be filtered out, got:\n%s", output)
	}
}

func TestListFromFlagRefAndPathRequireFrom(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("list", "--ref", "main")
	if err == nil {
		t.Fatal("expected --ref without --from to fail")
	}
	if !strings.Contains(err.Error(), "--ref requires --from") {
		t.Fatalf("expected --ref requires --from error, got %v", err)
	}

	_, err = executeCommand("list", "--path", "sub/dir")
	if err == nil {
		t.Fatal("expected --path without --from to fail")
	}
	if !strings.Contains(err.Error(), "--path requires --from") {
		t.Fatalf("expected --path requires --from error, got %v", err)
	}
}

func TestListFromFlagBlockedURLReturnsError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
allowed_registries:
  - ^git\.internal\.example\.com/
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("list", "--from", "https://github.com/evil/repo.git")
	if err == nil {
		t.Fatal("expected blocked URL to fail")
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("expected blocked URL error, got %v", err)
	}
}
