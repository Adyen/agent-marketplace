//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type organizationRelockFixture struct {
	authority string
	orgPath   string
	lockPath  string
	alphaBare string
	alphaWork string
	betaBare  string
	betaWork  string
}

func newOrganizationRelockFixture(t *testing.T) organizationRelockFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	alphaBare, alphaWork := createBareRepoWithWorkingClone(t, "relock-alpha.git")
	writeSkill(t, alphaWork, "alpha-skill", "Alpha skill", nil)
	commitAndPushAll(t, alphaWork, "alpha skill")
	betaBare, betaWork := createBareRepoWithWorkingClone(t, "relock-beta.git")
	writeSkill(t, betaWork, "beta-skill", "Beta skill", nil)
	commitAndPushAll(t, betaWork, "beta skill")

	f := organizationRelockFixture{
		authority: canonicalTempDir(t),
		alphaBare: alphaBare,
		alphaWork: alphaWork,
		betaBare:  betaBare,
		betaWork:  betaWork,
	}
	f.orgPath = filepath.Join(f.authority, "config.yaml")
	f.lockPath = filepath.Join(f.authority, "lock.yaml")
	f.writeOrgConfig(t, "main")
	config.SetTestOrganizationConfigPath(t, f.orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())
	return f
}

func (f organizationRelockFixture) writeOrgConfig(t *testing.T, alphaRef string) {
	t.Helper()
	writeConfig(t, f.orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  alpha: {git: file://"+f.alphaBare+", ref: "+alphaRef+"}\n  beta: {git: file://"+f.betaBare+", ref: main}\nresources:\n  alpha: {all: true, required: true}\n  beta: {all: true, required: true}\n")
}

func (f organizationRelockFixture) lockOrganization(t *testing.T) {
	t.Helper()
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("lock --scope organization: %v\n%s", err, output)
	}
}

// moveUpstream advances alpha to a new tagged commit (changing the configured
// ref) and advances beta's main upstream without changing its configured ref.
func (f organizationRelockFixture) moveUpstream(t *testing.T, alphaRef string) string {
	t.Helper()
	writeSkill(t, f.alphaWork, "alpha-skill", "Alpha skill v2", nil)
	commitAndPushAll(t, f.alphaWork, "alpha v2")
	runGit(t, f.alphaWork, "tag", alphaRef)
	runGit(t, f.alphaWork, "push", "origin", alphaRef)
	writeSkill(t, f.betaWork, "beta-skill", "Beta skill v2", nil)
	commitAndPushAll(t, f.betaWork, "beta v2")
	f.writeOrgConfig(t, alphaRef)
	return gitCommit(t, f.alphaBare, alphaRef+"^{commit}")
}

func gitCommit(t *testing.T, repo, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", rev).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

func (f organizationRelockFixture) lockBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.lockPath)
	if err != nil {
		t.Fatalf("read organization lock: %v", err)
	}
	return data
}

func lockRegistryByURL(t *testing.T, data []byte, marker string) lockfile.GitRegistry {
	t.Helper()
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatalf("decode organization lock: %v", err)
	}
	for _, registry := range doc.Registries {
		if strings.Contains(registry.URL, marker) {
			return registry
		}
	}
	t.Fatalf("organization lock has no registry matching %q:\n%s", marker, data)
	return lockfile.GitRegistry{}
}

func requireLockStaleErr(t *testing.T, args []string, err error, output string) {
	t.Helper()
	var lockErr *pipeline.LockViewError
	if !errors.As(err, &lockErr) || lockErr.Code != "lock.stale" {
		t.Fatalf("%s error = %v, want lock.stale\n%s", strings.Join(args, " "), err, output)
	}
}

// spec: deployed-acquisition-locks/plain-organization-lock-relocks-a-changed-ref
func TestOrganizationRelockChangedRefKeepsUnchangedEntries(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOrganizationRelockFixture(t)
	f.lockOrganization(t)
	before := f.lockBytes(t)
	betaBefore := lockRegistryByURL(t, before, "relock-beta")

	alphaCommit := f.moveUpstream(t, "v2")

	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("lock --scope organization after ref change: %v\n%s", err, output)
	}
	after := f.lockBytes(t)
	alphaAfter := lockRegistryByURL(t, after, "relock-alpha")
	if alphaAfter.Ref != "v2" || alphaAfter.Commit.Hex() != alphaCommit {
		t.Fatalf("alpha lock record = ref %q commit %s, want ref v2 at %s", alphaAfter.Ref, alphaAfter.Commit, alphaCommit)
	}
	if betaAfter := lockRegistryByURL(t, after, "relock-beta"); !reflect.DeepEqual(betaAfter, betaBefore) {
		t.Fatalf("unchanged beta record moved: before=%#v after=%#v", betaBefore, betaAfter)
	}
}

// spec: deployed-acquisition-locks/organization-lock-upgrade-after-a-ref-change-moves-every-entry
func TestOrganizationRelockUpgradeMovesEveryEntry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOrganizationRelockFixture(t)
	f.lockOrganization(t)

	alphaCommit := f.moveUpstream(t, "v2")
	betaCommit := gitCommit(t, f.betaBare, "main^{commit}")

	if output, err := executeCommand("lock", "--scope", "organization", "--upgrade"); err != nil {
		t.Fatalf("lock --scope organization --upgrade after ref change: %v\n%s", err, output)
	}
	after := f.lockBytes(t)
	alphaAfter := lockRegistryByURL(t, after, "relock-alpha")
	if alphaAfter.Ref != "v2" || alphaAfter.Commit.Hex() != alphaCommit {
		t.Fatalf("alpha lock record = ref %q commit %s, want ref v2 at %s", alphaAfter.Ref, alphaAfter.Commit, alphaCommit)
	}
	betaAfter := lockRegistryByURL(t, after, "relock-beta")
	if betaAfter.Commit.Hex() != betaCommit {
		t.Fatalf("beta lock record commit = %s, want upstream %s", betaAfter.Commit, betaCommit)
	}
}

// spec: deployed-acquisition-locks/other-commands-keep-refusing-a-stale-organization-lock
func TestOrganizationRelockOtherCommandsRefuseStaleLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOrganizationRelockFixture(t)
	f.lockOrganization(t)
	f.moveUpstream(t, "v2")
	before := f.lockBytes(t)

	for _, args := range [][]string{
		{"sync"},
		{"sync", "--locked"},
		{"status"},
		{"lock", "--check"},
		{"lock"},
	} {
		output, err := executeCommand(args...)
		requireLockStaleErr(t, args, err, output)
		if got := f.lockBytes(t); string(got) != string(before) {
			t.Fatalf("%s changed organization lock bytes", strings.Join(args, " "))
		}
	}
}

// spec: deployed-acquisition-locks/explicit-organization-lock-refuses-invalid-lock-evidence
func TestOrganizationRelockRefusesInvalidLockEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOrganizationRelockFixture(t)
	f.lockOrganization(t)
	f.moveUpstream(t, "v2")

	for _, test := range []struct {
		name   string
		mutate func(t *testing.T)
	}{
		{name: "malformed", mutate: func(t *testing.T) {
			writeFile(t, f.lockPath, "not: [valid")
		}},
		{name: "wrong scope", mutate: func(t *testing.T) {
			writeFile(t, f.lockPath, "apiVersion: agent-marketplace-lock/v2\nscope: user\n")
		}},
		{name: "untrusted mode", mutate: func(t *testing.T) {
			writeFile(t, f.lockPath, "apiVersion: agent-marketplace-lock/v2\nscope: organization\n")
			if err := os.Chmod(f.lockPath, 0o666); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.mutate(t)
			written := f.lockBytes(t)
			output, err := executeCommand("lock", "--scope", "organization")
			if err == nil {
				t.Fatalf("lock --scope organization accepted %s organization lock:\n%s", test.name, output)
			}
			if got := f.lockBytes(t); string(got) != string(written) {
				t.Fatalf("lock --scope organization replaced %s organization lock", test.name)
			}
		})
	}
}

// spec: deployed-acquisition-locks/failed-re-resolution-leaves-the-organization-lock-unchanged
func TestOrganizationRelockFailedReResolutionKeepsLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newOrganizationRelockFixture(t)
	f.lockOrganization(t)
	f.writeOrgConfig(t, "nonexistent-ref")
	before := f.lockBytes(t)
	info, err := os.Stat(f.lockPath)
	if err != nil {
		t.Fatal(err)
	}

	if output, err := executeCommand("lock", "--scope", "organization"); err == nil {
		t.Fatalf("lock --scope organization with unresolvable ref succeeded:\n%s", output)
	}
	if got := f.lockBytes(t); string(got) != string(before) {
		t.Fatal("failed relock changed organization lock bytes")
	}
	after, err := os.Stat(f.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != info.Mode() {
		t.Fatalf("organization lock mode changed: %v -> %v", info.Mode(), after.Mode())
	}
}
