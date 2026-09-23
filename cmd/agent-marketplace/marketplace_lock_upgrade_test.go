package main

import (
	"os"
	"path/filepath"
	"reflect"
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
	checkMode := func(want os.FileMode) {
		t.Helper()
		info, err := os.Stat(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("lock mode = %04o, want %04o", got, want)
		}
	}
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+gitSource)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	gitOnly := readLockDocument(t, lockPath)
	if gitOnly.APIVersion != lockfile.APIVersionV1 || len(gitOnly.Registries) != 1 {
		t.Fatalf("initial lock = %#v, want Git-only v1", gitOnly)
	}

	checkMode(0o644)
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
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

	checkMode(0o600)
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
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
	checkMode(0o600)
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync after removing Git source: %v\n%s", err, output)
	}
	pruned := readLockDocument(t, lockPath)
	if len(pruned.Registries) != 0 || !reflect.DeepEqual(pruned.Marketplaces, updated.Marketplaces) {
		t.Fatalf("pruned lock = %#v, want only retained Marketplace records", pruned)
	}
	checkMode(0o600)
}

func TestMergeInstallBindingsRetainsLockVersionAndMarketplaceEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	registry := reviewBindingRegistry(t)
	registry.Roots[0].Resources[0].SupportFiles = map[string]lockfile.Digest{}
	digest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	marketplaces := []lockfile.MarketplaceLock{{
		URL: "https://market.example", InstanceID: "mp",
		RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
		Resources:        []lockfile.MarketplaceResourceLock{{RegistryName: "tools", RegistryID: "src", Kind: "skill", Name: "review", Revision: string(digest), ContentDigest: digest, ArchiveDigest: digest, Acquisition: lockfile.MarketplaceAcquisitionMirror}},
	}}
	for _, tc := range []struct {
		name        string
		base        lockfile.Document
		wantVersion string
	}{
		{name: "new", wantVersion: lockfile.APIVersionV1},
		{name: "v1", base: lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo}, wantVersion: lockfile.APIVersionV1},
		{name: "v2 endpoint-only", base: lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "mp"}}}, wantVersion: lockfile.APIVersionV2},
		{name: "v2 Marketplace", base: lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: marketplaces}, wantVersion: lockfile.APIVersionV2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.base.APIVersion != "" {
				if _, err := lockfile.Encode(tc.base); err != nil {
					t.Fatalf("invalid lock fixture: %v", err)
				}
			}
			merged, err := mergeInstallBindings(tc.base, lockfile.ScopeRepo, []lockfile.GitRegistry{registry})
			if err != nil {
				t.Fatal(err)
			}
			if merged.APIVersion != tc.wantVersion || merged.Scope != lockfile.ScopeRepo || !reflect.DeepEqual(merged.Marketplaces, tc.base.Marketplaces) || !reflect.DeepEqual(merged.Registries, []lockfile.GitRegistry{registry}) {
				t.Fatalf("merged lock = %#v, want version %s with Git binding and unchanged Marketplace evidence", merged, tc.wantVersion)
			}
		})
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
