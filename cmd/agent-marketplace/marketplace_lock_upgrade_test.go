package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestSyncUpgradesGitOnlyLockWhenMarketplaceSourceIsAdded(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()

	origin, working := createBareRepoWithWorkingClone(t, "tools.git")
	gitSource := "  git-tools:\n    git: file://" + origin + "\n    ref: main\n"
	writeSkill(t, working, "git-review", "Git review", nil)
	commitAndPushAll(t, working, "seed")

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+gitSource)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	gitOnly := readLockDocument(t, lockPath)
	if gitOnly.APIVersion != lockfile.APIVersionV1 || len(gitOnly.Registries) != 1 {
		t.Fatalf("initial lock = %#v, want Git-only v1", gitOnly)
	}

	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+gitSource+
		"  hosted: {marketplace: "+server.URL+"}\nresources:\n  git-tools: {all: true}\n  hosted: {include: [tools/skill/review]}\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync after adding Marketplace source: %v\n%s", err, output)
	}
	upgraded := readLockDocument(t, lockPath)
	if upgraded.APIVersion != lockfile.APIVersionV2 || len(upgraded.Marketplaces) != 1 || len(upgraded.Registries) != 1 || upgraded.Registries[0].Commit != gitOnly.Registries[0].Commit {
		t.Fatalf("upgraded lock = %#v, want v2 retaining Git registry %#v", upgraded, gitOnly.Registries)
	}
	for _, skill := range []string{"git-review", "review"} {
		if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", skill, "SKILL.md")); err != nil {
			t.Fatalf("%s was not materialized: %v", skill, err)
		}
	}

	writeSkill(t, working, "git-review", "Git review v2", nil)
	commitAndPushAll(t, working, "advance")
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update: %v\n%s", err, output)
	}
	updated := readLockDocument(t, lockPath)
	if updated.APIVersion != lockfile.APIVersionV2 || len(updated.Marketplaces) != 1 || len(updated.Registries) != 1 || updated.Registries[0].Commit == gitOnly.Registries[0].Commit {
		t.Fatalf("updated lock = %#v, want advanced Git commit with retained Marketplace records", updated)
	}
}

func TestCommandsAcceptMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
telemetry: {level: "off"}
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}

	for _, tc := range []struct {
		args []string
		// want is required output on success; wantErr is a required error
		// for commands whose correct outcome after loading config is a failure.
		want, wantErr string
	}{
		{args: []string{"gc"}},
		{args: []string{"doctor"}, want: "org managed:"},
		{args: []string{"telemetry", "status"}, want: "off (org)"},
		{args: []string{"search", "review"}, wantErr: "marketplace error 404"},
		{args: []string{"remove", "not-installed"}, wantErr: "is not currently enabled"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			output, err := executeCommand(tc.args...)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q\n%s", err, tc.wantErr, output)
				}
			} else if err != nil || !strings.Contains(output, tc.want) {
				t.Fatalf("rejected missing organization Marketplace lock: %v\n%s", err, output)
			}
			if strings.Contains(output, "marketplace-lock-required") {
				t.Fatalf("reported missing organization Marketplace lock:\n%s", output)
			}
			if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
				t.Fatalf("created organization lock: %v", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("gc removed reachable organization Marketplace artifact: %v", err)
	}
}
