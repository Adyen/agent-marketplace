package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestWritingCommandsCreateMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "update", "lock"} {
		for _, scope := range []lockfile.Scope{lockfile.ScopeUser, lockfile.ScopeRepo} {
			t.Run(command+"/"+string(scope), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				server := newV4MarketplaceLockServer(t)
				defer server.Close()
				repo := t.TempDir()
				configRoot := repo
				if scope == lockfile.ScopeUser {
					configRoot = home
				}
				configPath := filepath.Join(configRoot, ".agents", "config.yaml")
				writeConfig(t, configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
				before, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, repo)

				output, err := executeCommand(command)
				if err != nil {
					t.Fatalf("%s: %v\n%s", command, err, output)
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != string(before) {
					t.Fatalf("%s changed authored config: %v\nbefore:\n%s\nafter:\n%s", command, err, before, after)
				}
				doc := readLockDocument(t, filepath.Join(configRoot, ".agents", "lock.yaml"))
				if doc.APIVersion != lockfile.APIVersionV2 || doc.Scope != scope || len(doc.Marketplaces) != 1 {
					t.Fatalf("%s lock header = %#v", command, doc)
				}
				locked := doc.Marketplaces[0]
				if locked.URL != server.URL || locked.InstanceID != "mp" || len(locked.Resources) != 1 {
					t.Fatalf("%s Marketplace lock = %#v", command, locked)
				}
				resource := locked.Resources[0]
				if resource.RegistryName != "tools" || resource.RegistryID != "src" || resource.Kind != "skill" || resource.Name != "review" || resource.ArchiveDigest == "" {
					t.Fatalf("%s resource lock = %#v", command, resource)
				}
				if command == "sync" && scope == lockfile.ScopeRepo {
					if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
						t.Fatalf("%s did not materialize review: %v", command, err)
					}
				}
			})
		}
	}
}

func TestSyncBootstrapsWritableMarketplaceLockWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()
	repoServer := newV4MarketplaceLockServer(t)
	defer repoServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)

	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  writable: {marketplace: `+repoServer.URL+`}
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != repoServer.URL {
		t.Fatalf("writable Marketplace lock = %#v", doc)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization resource was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync wrote organization lock: %v", err)
	}
}

func TestConfiguredInstallPreparesDependenciesWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)

	origin, working := createBareRepoWithWorkingClone(t, "install-source.git")
	writeSkill(t, working, "git-review", "Git review", nil)
	commitAndPushAll(t, working, "seed install source")
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("install", "git-review")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "git-review", "SKILL.md")); err != nil {
		t.Fatalf("git-review was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization review was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install wrote organization lock: %v", err)
	}
}

func TestMarketplaceInstallRevalidatesAfterConsentWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()
	targetServer := newV4MarketplaceLockServerWithIdentity(t, "target-mp", "target-review")
	defer targetServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [factory]
marketplaces:
  - url: `+targetServer.URL+`
    instance_id: target-mp
    alias: target
`)
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", "tools/skill/target-review", "--marketplace", "target", "--yes")
	if err != nil {
		t.Fatalf("Marketplace install: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization review was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "target-review", "SKILL.md")); err != nil {
		t.Fatalf("target-review was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install wrote organization lock: %v", err)
	}
}

func TestInstallCommitReuseDoesNotAcquireTransientOrganizationMarketplaceLock(t *testing.T) {
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

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+server.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	reuse := &preparedInstallLocks{
		documents: make(map[string]lockfile.Document),
		lockPaths: make(map[config.Source]string),
		bindings:  make(map[string][]lockfile.GitRegistry),
	}
	_, _, _, err := prepareRuntimeLocks(context.Background(), t.TempDir(), "", lockMode{reuseInstall: reuse})
	if err == nil || !marketplacelock.HasRequiredError([]error{err}) {
		t.Fatalf("install commit without prepared transient state error = %v, want v4 Marketplace lock requirement", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("install commit cache-only revalidation made %d Marketplace requests", got)
	}
}

func TestSyncAcquiresMissingV4OrganizationMarketplaceLockTransiently(t *testing.T) {
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

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	orgConfig := []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(orgPath, orgConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync rejected organization Marketplace config without a deployed lock: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("sync did not materialize organization Marketplace resource: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("consumer created organization lock: %v", err)
	}
	after, err := os.ReadFile(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(orgConfig) {
		t.Fatalf("sync changed organization config:\nbefore:\n%s\nafter:\n%s", orgConfig, after)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("sync made %d Marketplace requests, want one five-request acquisition without a second resolution pass", got)
	}
}

func TestTUISyncReusesTransientOrganizationMarketplaceLock(t *testing.T) {
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

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	if err := runTUIDependencyLifecycle(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization resource was not materialized: %v", err)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("TUI sync made %d Marketplace requests, want one five-request invocation-scoped acquisition", got)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("TUI sync wrote organization lock: %v", err)
	}
}

func TestTransientOrganizationMarketplaceLockIsReusedWhileInputsMatch(t *testing.T) {
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

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	opts := pipeline.ResolveOptions{
		RepoRoot: repo,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
	}
	ctx := pipeline.WithTransientOrganizationMarketplaceLockCache(context.Background())
	for run := range 2 {
		result := pipeline.Resolve(ctx, opts)
		if err := errors.Join(result.Errors...); err != nil {
			t.Fatalf("resolve %d: %v", run+1, err)
		}
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("two matching resolves made %d Marketplace requests, want one five-request invocation-scoped acquisition", got)
	}
}

func TestSyncNeverReplacesPresentOrganizationMarketplaceLockTransiently(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name string
		lock func(*testing.T, string) []byte
	}{
		{
			name: "malformed",
			lock: func(*testing.T, string) []byte {
				return []byte("not: [valid\n")
			},
		},
		{
			name: "mismatched endpoint",
			lock: func(t *testing.T, _ string) []byte {
				t.Helper()
				raw, err := lockfile.Encode(lockfile.Document{
					APIVersion: lockfile.APIVersionV2,
					Scope:      lockfile.ScopeOrganization,
					Marketplaces: []lockfile.MarketplaceLock{{
						URL:        "https://different.example",
						InstanceID: "other",
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			authority := t.TempDir()
			orgPath := filepath.Join(authority, "config.yaml")
			writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
			lockPath := filepath.Join(authority, "lock.yaml")
			before := test.lock(t, server.URL)
			if err := os.WriteFile(lockPath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			withWorkingDir(t, t.TempDir())

			if output, err := executeCommand("sync"); err == nil {
				t.Fatalf("sync unexpectedly replaced a present organization lock:\n%s", output)
			}
			if requests.Load() != 0 {
				t.Fatalf("present invalid lock triggered %d Marketplace requests", requests.Load())
			}
			after, err := os.ReadFile(lockPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("organization lock changed: %q, %v", after, err)
			}
		})
	}
}

func TestSyncOfflineDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--offline"); err == nil {
		t.Fatalf("offline sync unexpectedly acquired a transient organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("offline sync made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("offline sync created organization lock: %v", err)
	}
}

func TestSyncCheckDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--check"); err == nil {
		t.Fatalf("sync --check unexpectedly acquired a transient organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("sync --check made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --check created organization lock: %v", err)
	}
}

func TestLockCheckDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("lock", "--check"); err == nil {
		t.Fatalf("lock --check unexpectedly accepted a missing organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("lock --check made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("lock --check created organization lock: %v", err)
	}
}

func TestSyncTransientOrganizationLockHonorsRevocationOverride(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var coordinate string
	server := newV4MarketplaceLockServerWithRevision(t, nil, func(revision *store.AssessedResourceRevision) {
		coordinate = "mp/src/skill/review@" + string(revision.ContentDigest)
		revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test"}
	})
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--allow-revoked", coordinate); err != nil {
		t.Fatalf("sync with exact revocation override: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync created organization lock: %v", err)
	}
}

func TestSyncWritableMarketplaceLockHonorsRevocationOverride(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var coordinate string
	server := newV4MarketplaceLockServerWithRevision(t, nil, func(revision *store.AssessedResourceRevision) {
		coordinate = "mp/src/skill/review@" + string(revision.ContentDigest)
		revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test"}
	})
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--allow-revoked", coordinate); err != nil {
		t.Fatalf("sync with exact revocation override: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil {
		t.Fatalf("sync did not create writable Marketplace lock: %v", err)
	}
}

func TestSyncFailsClosedWhenOrganizationConfigChangesDuringTransientAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	mutated := make(chan error, 1)
	var once atomic.Bool
	var orgConfig []byte
	server := newV4MarketplaceLockServerWithHook(t, func() {
		if !once.CompareAndSwap(false, true) {
			return
		}
		mutated <- os.WriteFile(orgPath, append(append([]byte(nil), orgConfig...), []byte("# changed concurrently\n")...), 0o644)
	})
	defer server.Close()
	orgConfig = []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(orgPath, orgConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("sync")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("sync error = %v, want generation conflict\n%s", err, output)
	}
	if !once.Load() {
		t.Fatal("transient acquisition did not reach Marketplace")
	}
	if mutationErr := <-mutated; mutationErr != nil {
		t.Fatalf("mutate organization config: %v", mutationErr)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync created organization lock after config race: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".factory", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("sync materialized from raced organization config: %v", err)
	}
}

func TestMissingV4MarketplaceLockHonorsAllowlistBeforeNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync"); err == nil {
		t.Fatalf("sync unexpectedly bypassed Marketplace allowlist:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("disallowed Marketplace received %d requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("disallowed Marketplace created a lock: %v", err)
	}
}

func TestSyncLockedDoesNotBootstrapMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--locked"); err == nil {
		t.Fatalf("sync --locked unexpectedly bootstrapped a lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("sync --locked made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --locked created a lock: %v", err)
	}
}

func TestLockCreatesMissingEndpointOnlyV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	}))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+server.URL+"}\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock endpoint-only source: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "mp" || len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint-only lock = %#v", doc)
	}
}

func newV4MarketplaceLockServer(t *testing.T) *httptest.Server {
	return newV4MarketplaceLockServerWithHook(t, nil)
}

func newV4MarketplaceLockServerWithHook(t *testing.T, hook func()) *httptest.Server {
	return newV4MarketplaceLockServerWithRevision(t, hook, nil)
}

func newV4MarketplaceLockServerWithRevision(t *testing.T, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, "mp", "review", hook, mutate)
}

func newV4MarketplaceLockServerWithIdentity(t *testing.T, instanceID, resourceName string) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, instanceID, resourceName, nil, nil)
}

func newV4MarketplaceLockServerWithOptions(t *testing.T, instanceID, resourceName string, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	raw, proof, archiveDigest, err := hccartifact.EncodeWithProof(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "skill", Name: resourceName},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	if mutate != nil {
		mutate(&revision)
	}
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil {
			hook()
		}
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com"))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifact-proofs/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact-proof.v1.tar+gzip")
			_, _ = w.Write(proof)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
}
