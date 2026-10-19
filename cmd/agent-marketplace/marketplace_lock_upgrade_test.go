package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: deployed-acquisition-locks/git-only-lock-gains-marketplace-evidence
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
	if gitOnly.APIVersion != lockfile.APIVersionV2 || len(gitOnly.Registries) != 1 || len(gitOnly.Marketplaces) != 0 {
		t.Fatalf("initial lock = %#v, want Git-only v2", gitOnly)
	}
	checkMode(0o644)

	v1 := gitOnly
	v1.APIVersion = lockfile.APIVersionV1
	v1Bytes, err := lockfile.Encode(v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, v1Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("v1 upgrade sync: %v\n%s", err, output)
	}
	rewritten := readLockDocument(t, lockPath)
	if rewritten.APIVersion != lockfile.APIVersionV2 || len(rewritten.Marketplaces) != 0 || !reflect.DeepEqual(rewritten.Registries, gitOnly.Registries) {
		t.Fatalf("rewritten lock = %#v, want v2 retaining Git registry %#v", rewritten, gitOnly.Registries)
	}
	checkMode(0o600)

	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+gitSource+
		"  hosted: {marketplace: "+server.URL+"}\nresources:\n  git-tools: {all: true}\n  hosted: {include: [tools/skill/review]}\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync after adding Marketplace source: %v\n%s", err, output)
	}
	upgraded := readLockDocument(t, lockPath)
	if upgraded.APIVersion != lockfile.APIVersionV2 || len(upgraded.Marketplaces) != 1 || !reflect.DeepEqual(upgraded.Registries, gitOnly.Registries) {
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
		name string
		base lockfile.Document
	}{
		{name: "new"},
		{name: "v1", base: lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo}},
		{name: "v2 Git-only", base: lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{registry}}},
		{name: "v2 endpoint-only", base: lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "mp"}}}},
		{name: "v2 Marketplace", base: lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: marketplaces}},
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
			if merged.APIVersion != lockfile.APIVersionV2 || merged.Scope != lockfile.ScopeRepo || !reflect.DeepEqual(merged.Marketplaces, tc.base.Marketplaces) || !reflect.DeepEqual(merged.Registries, []lockfile.GitRegistry{registry}) {
				t.Fatalf("merged lock = %#v, want v2 with Git binding and unchanged Marketplace evidence", merged)
			}
		})
	}
}

// spec: deployed-acquisition-locks/check-reports-changes-to-missing-locks
func TestV1LockCheckReportsVersionUpgradeRequiredWithoutWriting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "v1-check.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "seed")

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools:\n    git: file://"+origin+"\n    ref: main\nresources:\n  tools: {all: true}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	doc := readLockDocument(t, lockPath)
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Registries) != 1 {
		t.Fatalf("initial lock = %#v, want Git-only v2", doc)
	}

	doc.APIVersion = lockfile.APIVersionV1
	v1Bytes, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, v1Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("lock", "--check", "--scope", "repo")
	var lifecycle *lockLifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Code != "lock.version-upgrade-required" || lifecycle.Scope != lockfile.ScopeRepo || lifecycle.Path != lockPath {
		t.Fatalf("lock --check on writable v1 = %v, want scoped lock.version-upgrade-required at %s\n%s", err, lockPath, output)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil || string(after) != string(v1Bytes) {
		t.Fatalf("lock --check rewrote the v1 lock: %v\n%s", err, after)
	}
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("locked consumption rejected a matching v1 lock: %v\n%s", err, output)
	}
	userLockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  personal:\n    git: file://"+origin+"\n    ref: main\n")
	output, err = executeCommand("lock", "--check")
	if !errors.As(err, &lifecycle) || lifecycle.Code != "lock.change-required" {
		t.Fatalf("lock --check with v1 and missing user lock = %v, want lock.change-required\n%s", err, output)
	}
	if _, err := os.Lstat(userLockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock --check created user lock: %v", err)
	}
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools:\n    git: file://"+origin+"\n    ref: changed\nresources:\n  tools: {all: true}\n")
	output, err = executeCommand("lock", "--check", "--scope", "repo")
	if !errors.As(err, &lifecycle) || lifecycle.Code != "lock.change-required" {
		t.Fatalf("lock --check with v1 and real drift = %v, want lock.change-required\n%s", err, output)
	}
	after, err = os.ReadFile(lockPath)
	if err != nil || string(after) != string(v1Bytes) {
		t.Fatalf("lock --check with drift rewrote the v1 lock: %v\n%s", err, after)
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
