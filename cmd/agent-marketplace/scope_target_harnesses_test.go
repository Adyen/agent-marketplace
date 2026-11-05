package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// scopeTargetsFixture configures an organization and a user layer that both
// target Factory, each selecting one skill from the same Git source, and a
// repository whose own skill is selected by the committed configuration.
type scopeTargetsFixture struct {
	home, repo, origin string
}

func newScopeTargetsFixture(t *testing.T, repositoryConfig, orgHarnesses string) scopeTargetsFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "scope-target-skills.git")
	for _, name := range []string{"org-skill", "user-skill", "repo-skill"} {
		writeSkill(t, working, name, name, nil)
	}
	commitAndPushAll(t, working, "Add skills")
	orgConfig := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgConfig, "apiVersion: agent-marketplace/v4\n"+orgHarnesses+"sources:\n  hosted: {git: file://"+origin+", ref: main}\nresources:\n  hosted: [skill/org-skill]\n")
	config.SetTestOrganizationConfigPath(t, orgConfig)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+origin+", ref: main}\nresources:\n  hosted: [skill/user-skill]\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), repositoryConfig)
	withWorkingDir(t, repo)
	requireOrganizationLock(t, orgConfig)
	return scopeTargetsFixture{home: home, repo: repo, origin: origin}
}

func (f scopeTargetsFixture) narrowedRepositoryConfig() string {
	return "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  hosted: {git: file://" + f.origin + ", ref: main}\nresources:\n  hosted: [skill/repo-skill]\n"
}

func assertPathPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got err=%v", path, err)
	}
}

// spec: resource-selection/repository-harness-list-does-not-narrow-machine-scope-output
func TestSyncRepositoryHarnessesDoNotNarrowMachineOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newScopeTargetsFixture(t, "apiVersion: agent-marketplace/v4", "harnesses: [factory]\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, f.home, "org-skill")
	assertFactorySkillExists(t, f.home, "user-skill")

	// The repository now declares a narrower harness list of its own.
	writeFile(t, filepath.Join(f.repo, ".agents", "config.yaml"), f.narrowedRepositoryConfig())
	config.ResetRepoRootCache()
	for _, args := range [][]string{{"sync"}, {"sync", "--check"}} {
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		assertFactorySkillExists(t, f.home, "org-skill")
		assertFactorySkillExists(t, f.home, "user-skill")
		assertPathAbsent(t, filepath.Join(f.home, ".claude", "skills", "org-skill"))
		assertPathAbsent(t, filepath.Join(f.home, ".claude", "skills", "user-skill"))
		assertPathPresent(t, filepath.Join(f.repo, ".claude", "skills", "repo-skill"))
		assertPathAbsent(t, filepath.Join(f.repo, ".factory", "skills", "repo-skill"))
	}

	// status reports both target sets instead of only the repository's.
	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, output)
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*claude\s+REPO \(repository selections\)\s*$`),
		regexp.MustCompile(`(?m)^\s*factory\s+USER \(user and organization selections\)\s*$`),
	} {
		if !want.MatchString(output) {
			t.Fatalf("status target harness set does not match %s:\n%s", want, output)
		}
	}
}

// An organization file without its own harness list inherits the user list,
// never the repository's, so a narrower repository list cannot drop it.
func TestSyncOrganizationWithoutHarnessesUsesUserTargets(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newScopeTargetsFixture(t, "apiVersion: agent-marketplace/v4", "")
	writeFile(t, filepath.Join(f.repo, ".agents", "config.yaml"), f.narrowedRepositoryConfig())
	config.ResetRepoRootCache()
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, f.home, "org-skill")
	assertFactorySkillExists(t, f.home, "user-skill")
	assertPathAbsent(t, filepath.Join(f.home, ".claude", "skills", "org-skill"))
	assertPathPresent(t, filepath.Join(f.repo, ".claude", "skills", "repo-skill"))
}

// spec: resource-selection/machine-scope-teardown-preserves-organization-output-for-machine-targets
func TestDeinitUserInsideNarrowedRepositoryPreservesOrganizationOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newScopeTargetsFixture(t, "apiVersion: agent-marketplace/v4", "harnesses: [factory]\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, f.home, "org-skill")
	assertFactorySkillExists(t, f.home, "user-skill")

	writeFile(t, filepath.Join(f.repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	config.ResetRepoRootCache()
	output, err := executeCommand("deinit", "--scope", "user")
	if err != nil {
		t.Fatalf("deinit --scope user: %v\n%s", err, output)
	}
	assertSkillLinkPresent(t, f.home, "org-skill")
	assertSkillLinkAbsent(t, f.home, "user-skill")
}

// spec: resource-selection/machine-scope-output-falls-back-to-the-repository-list
func TestSyncMachineOutputFallsBackToRepositoryHarnesses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "fallback-skills.git")
	writeSkill(t, working, "user-skill", "user-skill", nil)
	commitAndPushAll(t, working, "Add skill")
	// No organization configuration and no user harness list.
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "absent.yaml"))
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {git: file://"+origin+", ref: main}\nresources:\n  hosted: [skill/user-skill]\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	assertPathPresent(t, filepath.Join(home, ".claude", "skills", "user-skill", "SKILL.md"))
	assertPathAbsent(t, filepath.Join(home, ".factory", "skills", "user-skill"))
}
