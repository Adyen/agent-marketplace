package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  tools:
    git: file://`+origin+`
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
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  target: {marketplace: `+targetServer.URL+`}
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
	if err == nil || !pipeline.NeedsLockBootstrap([]error{err}) {
		t.Fatalf("install commit without prepared transient state error = %v, want v4 Marketplace lock requirement", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("install commit cache-only revalidation made %d Marketplace requests", got)
	}
}

func TestInstallCommitReuseKeepsUnrelatedMissingLockError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools:\n    git: https://example.test/a.git\n    ref: main\n")
	reuse := &preparedInstallLocks{
		documents: make(map[string]lockfile.Document),
		lockPaths: map[config.Source]string{config.RepoCommitted: filepath.Join(t.TempDir(), "lock.yaml")},
		bindings:  make(map[string][]lockfile.GitRegistry),
	}
	_, _, _, err := prepareRuntimeLocks(context.Background(), t.TempDir(), "", lockMode{reuseInstall: reuse})
	var lockErr *pipeline.LockViewError
	if !errors.As(err, &lockErr) || lockErr.Code != "lock.missing" {
		t.Fatalf("install commit with an unrelated missing lock error = %v, want lock.missing", err)
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

func TestDoctorAcquiresMissingV4OrganizationMarketplaceLockWithoutPublishing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	orgConfig := `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`
	writeFile(t, orgPath, orgConfig)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	for _, args := range [][]string{{"doctor"}, {"doctor", "--repair"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "empty-cache"))
			before := requests.Load()
			output, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, output)
			}
			if !strings.Contains(output, "path: "+orgPath) || strings.Contains(output, "v4.marketplace-lock-required") || strings.Contains(output, "CONFIGURATION ERRORS") {
				t.Fatalf("%v did not diagnose the acquired organization layer:\n%s", args, output)
			}
			if args[len(args)-1] == "--repair" && !strings.Contains(output, "nothing to repair") {
				t.Fatalf("repair did not complete its preview:\n%s", output)
			}
			if requests.Load() == before {
				t.Fatalf("%v did not acquire evidence from Marketplace with an empty cache", args)
			}
			if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
				t.Fatalf("%v wrote organization lock: %v", args, err)
			}
			if got, err := os.ReadFile(orgPath); err != nil || string(got) != orgConfig {
				t.Fatalf("%v changed organization config: %v\n%s", args, err, got)
			}
		})
	}
}

func TestDoctorRepairRefusesOrganizationLockDeployedDuringConfirmation(t *testing.T) {
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
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mcpPath, selection := prepareTwoDevinConflicts(t, repo)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	stdinReader, stdinWriter := io.Pipe()
	watcher := newPromptWatcher()
	done := make(chan error, 1)
	go func() {
		done <- runDoctor(doctorOptions{out: watcher, in: stdinReader, isTTY: true, repair: true, resolve: staticResolve(selection)})
	}()

	select {
	case <-watcher.promptCh:
	case err := <-done:
		t.Fatalf("doctor finished before confirmation: %v\n%s", err, watcher.String())
	}
	lockPath := filepath.Join(authority, "lock.yaml")
	deployed := []byte("present: invalid\n")
	if err := os.WriteFile(lockPath, deployed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stdinWriter.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("doctor did not reject deployed organization lock: %v\n%s", err, watcher.String())
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("doctor changed aggregate after organization lock appeared: %v\n%s", err, after)
	}
	if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(after, deployed) {
		t.Fatalf("doctor changed deployed organization lock: %v\n%s", err, after)
	}
}

func TestDoctorRepairRefusesIncompleteSelectionWhenTransientAcquisitionFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "empty-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
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
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mcpPath, _ := prepareDevinRepairFixture(t, repo)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{out: &out, in: strings.NewReader("y\n"), isTTY: true, repair: true})
	if err == nil || !strings.Contains(out.String(), "acquire transient organization Marketplace lock") {
		t.Fatalf("doctor --repair must report failed acquisition and refuse an incomplete selection: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "v4.marketplace-lock-required") || strings.Contains(out.String(), "remove mcp-server") {
		t.Fatalf("doctor --repair offered an orphan after Marketplace acquisition failed:\n%s", out.String())
	}
	if got, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(got, before) {
		t.Fatalf("failed acquisition changed the aggregate: %v\n%s", err, got)
	}
	if requests.Load() == 0 {
		t.Fatal("doctor did not attempt Marketplace acquisition")
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("doctor created organization lock on failed acquisition: %v", err)
	}
}

func TestDoctorDoesNotReplacePresentV4OrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
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
	lock, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeOrganization,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://different.example", InstanceID: "other",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(authority, "lock.yaml")
	if err := os.WriteFile(lockPath, lock, 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("doctor")
	if err != nil || !strings.Contains(output, "v4.marketplace-lock-mismatch") {
		t.Fatalf("doctor did not report the present mismatched lock: %v\n%s", err, output)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("doctor contacted Marketplace %d times despite a present mismatched lock", got)
	}
	if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, lock) {
		t.Fatalf("doctor modified the present organization lock: %v\n%s", err, got)
	}
}

func TestDoctorReportsOrganizationAcquisitionFailureAcrossAliases(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  first: {marketplace: `+server.URL+`}
  second: {marketplace: `+server.URL+`}
resources:
  first: {include: [tools/skill/review]}
  second: {include: [tools/skill/another]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("doctor")
	if err != nil || !strings.Contains(output, "acquire transient organization Marketplace lock") || !strings.Contains(output, "tools/skill/another") || strings.Contains(output, "v4.marketplace-lock-required") {
		t.Fatalf("doctor masked organization acquisition failure across aliases: %v\n%s", err, output)
	}
	if requests.Load() == 0 {
		t.Fatal("doctor did not attempt acquisition for valid endpoint aliases")
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

	if _, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false); err != nil {
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
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	ws, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	opts := pipeline.ResolveOptions{
		Workspace: ws,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
		Loaders: pipeline.Loaders{
			Org:    config.LoadOrganizationConfig,
			Global: func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) },
			Repo:   func() (*config.Config, error) { return nil, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
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

// spec: deployed-acquisition-locks/selection-edit-reuses-organization-evidence
func TestTransientOrganizationMarketplaceLockSurvivesSelectionConfigEdits(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	requests, userPath, repoPath, ctx, opts := newTransientOrganizationMarketplaceResolveFixture(t)
	if result := pipeline.Resolve(ctx, opts); errors.Join(result.Errors...) != nil {
		t.Fatalf("initial resolve: %v", errors.Join(result.Errors...))
	}
	first := requests.Load()
	if first != 5 {
		t.Fatalf("initial resolve made %d Marketplace requests, want 5", first)
	}

	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# harmless selection-layer edit\n")
	writeConfig(t, repoPath, "apiVersion: agent-marketplace/v4\n")
	result := pipeline.Resolve(ctx, opts)
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatalf("resolve after selection config edits: %v", err)
	}
	if got := requests.Load(); got != first {
		t.Fatalf("selection config edits changed Marketplace request count from %d to %d, want no new requests", first, got)
	}
}

// spec: deployed-acquisition-locks/harness-change-reacquires-organization-evidence
func TestTransientOrganizationMarketplaceLockReacquiresWhenTargetHarnessesChange(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	requests, userPath, _, ctx, opts := newTransientOrganizationMarketplaceResolveFixture(t)
	if result := pipeline.Resolve(ctx, opts); errors.Join(result.Errors...) != nil {
		t.Fatalf("initial resolve: %v", errors.Join(result.Errors...))
	}
	first := requests.Load()
	if first != 5 {
		t.Fatalf("initial resolve made %d Marketplace requests, want 5", first)
	}

	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory, claude]\n")
	result := pipeline.Resolve(ctx, opts)
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatalf("resolve after target harness change: %v", err)
	}
	if got := requests.Load(); got <= first {
		t.Fatalf("target harness change did not reacquire: Marketplace requests changed from %d to %d", first, got)
	}
}

func TestTransientOrganizationMarketplaceLockReacquiresWhenOrganizationConfigChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	requests, _, _, ctx, opts := newTransientOrganizationMarketplaceResolveFixture(t)
	if result := pipeline.Resolve(ctx, opts); errors.Join(result.Errors...) != nil {
		t.Fatalf("initial resolve: %v", errors.Join(result.Errors...))
	}
	first := requests.Load()
	if first != 5 {
		t.Fatalf("initial resolve made %d Marketplace requests, want 5", first)
	}

	orgPath := opts.EffectiveOpts.OrganizationPath
	orgConfig, err := os.ReadFile(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orgPath, append(orgConfig, []byte("# organization policy edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	result := pipeline.Resolve(ctx, opts)
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatalf("resolve after organization config change: %v", err)
	}
	if got := requests.Load(); got <= first {
		t.Fatalf("organization config change did not reacquire: Marketplace requests changed from %d to %d", first, got)
	}
}

func TestTransientOrganizationMarketplaceLockFailsClosedWhenTargetHarnessesChangeDuringAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	mutated := make(chan error, 1)
	var once atomic.Bool
	requests, _, _, ctx, opts := newTransientOrganizationMarketplaceResolveFixtureWithHook(t, func(userPath string) {
		if !once.CompareAndSwap(false, true) {
			return
		}
		mutated <- os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory, claude]\n"), 0o644)
	})

	result := pipeline.Resolve(ctx, opts)
	if err := errors.Join(result.Errors...); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("resolve error = %v, want generation conflict", err)
	}
	if mutationErr := <-mutated; mutationErr != nil {
		t.Fatalf("mutate user config: %v", mutationErr)
	}
	raced := requests.Load()

	result = pipeline.Resolve(ctx, opts)
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatalf("resolve after raced acquisition: %v", err)
	}
	if got := requests.Load(); got <= raced {
		t.Fatalf("resolve after raced acquisition reused its result: Marketplace requests changed from %d to %d", raced, got)
	}
}

func newTransientOrganizationMarketplaceResolveFixture(t *testing.T) (*atomic.Int32, string, string, context.Context, pipeline.ResolveOptions) {
	t.Helper()
	return newTransientOrganizationMarketplaceResolveFixtureWithHook(t, nil)
}

// onRequest runs inside each Marketplace request, before the response, so it
// can edit the user config while an acquisition is in flight.
func newTransientOrganizationMarketplaceResolveFixtureWithHook(t *testing.T, onRequest func(userPath string)) (*atomic.Int32, string, string, context.Context, pipeline.ResolveOptions) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	userPath := filepath.Join(home, ".agents", "config.yaml")
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() {
		requests.Add(1)
		if onRequest != nil {
			onRequest(userPath)
		}
	})
	t.Cleanup(server.Close)

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	repoPath := filepath.Join(repo, ".agents", "config.yaml")
	opts := pipeline.ResolveOptions{
		Workspace: mustWorkspace(t, repo),
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         repoPath,
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
		Loaders: pipeline.Loaders{
			Org:    config.LoadOrganizationConfig,
			Global: func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) },
			Repo:   func() (*config.Config, error) { return nil, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	}
	return &requests, userPath, repoPath, pipeline.WithTransientOrganizationMarketplaceLockCache(context.Background()), opts
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

func TestSyncCheckAcquiresMissingOrganizationMarketplaceLockTransiently(t *testing.T) {
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

	materialized := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	materializedBefore, readErr := os.ReadFile(materialized)
	statBefore, statErr := os.Stat(materialized)
	if readErr != nil || statErr != nil {
		t.Fatalf("seed sync did not materialize review: %v, %v", readErr, statErr)
	}
	stateRoot := filepath.Join(home, "state", "agent-marketplace")
	stateBefore := stateTreeSnapshot(t, stateRoot)
	requests.Store(0)

	output, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check: %v\n%s", err, output)
	}
	if strings.Contains(output, "out of sync") {
		t.Fatalf("sync --check reported drift on synchronized output:\n%s", output)
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("sync --check made no Marketplace requests; transient organization evidence was not acquired")
	}
	if after, readErr := os.ReadFile(orgPath); readErr != nil || string(after) != string(orgConfig) {
		t.Fatalf("sync --check changed organization config: %v", readErr)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --check created organization lock: %v", err)
	}
	materializedAfter, readErr := os.ReadFile(materialized)
	statAfter, statErr := os.Stat(materialized)
	if readErr != nil || statErr != nil || string(materializedAfter) != string(materializedBefore) || !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("sync --check changed materialized output: %v, %v", readErr, statErr)
	}
	if stateAfter := stateTreeSnapshot(t, stateRoot); stateAfter != stateBefore {
		t.Fatalf("sync --check changed durable state:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}

	if err := os.RemoveAll(filepath.Dir(materialized)); err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("sync", "--check")
	if err == nil || !strings.Contains(err.Error(), "out of sync") {
		t.Fatalf("sync --check after deleting output error = %v, want drift\n%s", err, output)
	}
	if strings.Contains(err.Error(), "marketplace-lock-required") || strings.Contains(output, "marketplace-lock-required") {
		t.Fatalf("drift check reported a lock requirement instead of drift: %v\n%s", err, output)
	}
	if after, readErr := os.ReadFile(orgPath); readErr != nil || string(after) != string(orgConfig) {
		t.Fatalf("drift check changed organization config: %v", readErr)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --check created organization lock: %v", err)
	}
	if stateAfter := stateTreeSnapshot(t, stateRoot); stateAfter != stateBefore {
		t.Fatalf("drift check changed durable state:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
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
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
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

func newV4MarketplaceLockServer(t testing.TB) *httptest.Server {
	return newV4MarketplaceLockServerWithHook(t, nil)
}

func newV4MarketplaceLockServerWithHook(t testing.TB, hook func()) *httptest.Server {
	return newV4MarketplaceLockServerWithRevision(t, hook, nil)
}

func newV4MarketplaceLockServerWithRevision(t testing.TB, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, "mp", "review", hook, mutate)
}

func newV4MarketplaceLockServerWithIdentity(t testing.TB, instanceID, resourceName string) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, instanceID, resourceName, nil, nil)
}

func newV4MarketplaceLockServerWithOptions(t testing.TB, instanceID, resourceName string, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	return newV4MarketplaceServer(t, instanceID, resourceName, false, hook, mutate)
}

// newV4MarketplaceServer serves one skill revision. selfCanonical makes the
// well-known document advertise the test server's own address, which endpoint
// installs require; the lock fixtures instead pin a fixed canonical URL.
func newV4MarketplaceServer(t testing.TB, instanceID, resourceName string, selfCanonical bool, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
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
			canonical := "https://example.com"
			if selfCanonical {
				canonical = "http://" + r.Host
			}
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, canonical))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/placeholder-archives/sha256/"):
			digest := "sha256:" + strings.TrimPrefix(r.URL.Path, "/api/v1/placeholder-archives/sha256/")
			placeholder, err := hccartifact.DerivePlaceholderArchive(raw, digest)
			if digest != string(revision.ArchiveDigest) || err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(placeholder)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
}
