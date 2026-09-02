package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestListCommandPrintsCatalogWithStatuses(t *testing.T) {
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

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}

	header, rows := parseTable(t, output)
	if got, want := strings.Join(header, "|"), "REGISTRY|KIND|NAME|TAGS (metadata)|STATUS"; got != want {
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

func createBareRepoWithWorkingClone(t testing.TB, bareName string) (string, string) {
	t.Helper()

	baseDir := t.TempDir()
	source := filepath.Join(baseDir, "source")
	origin := filepath.Join(baseDir, bareName)
	working := filepath.Join(baseDir, "working")

	runGit(t, "", "init", "--initial-branch=main", source)
	writeFile(t, filepath.Join(source, ".gitkeep"), "seed\n")
	runGit(t, source, "add", ".gitkeep")
	runGit(t, source, "commit", "-m", "seed")

	runGit(t, "", "clone", "--bare", source, origin)
	runGit(t, "", "clone", origin, working)

	return origin, working
}

func commitAndPushAll(t testing.TB, repoDir, message string) {
	t.Helper()

	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", message)
	runGit(t, repoDir, "push", "origin", "main")
}

func writeSkill(t testing.TB, repoDir, name, description string, tags []string) {
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

func writeConfig(t testing.TB, path, content string) {
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

func ensureGlobalConfig(t testing.TB) {
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

func writeFile(t testing.TB, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func withWorkingDir(t testing.TB, dir string) {
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

func runGit(t testing.TB, dir string, args ...string) {
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

func TestListFromFlagListsResourcesAtAdHocRegistry(t *testing.T) {
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
