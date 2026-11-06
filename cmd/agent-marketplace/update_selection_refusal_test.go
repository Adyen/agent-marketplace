package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// treeStateMap captures every entry under root as relative path -> identity:
// file mode plus content digest, symlink target, or directory mode. It lets a
// refusal test prove lock, config, and harness outputs are byte-identical with
// unchanged modes.
func treeStateMap(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[rel] = "symlink -> " + target
		case info.IsDir():
			result[rel] = "dir " + info.Mode().String()
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[rel] = fmt.Sprintf("file %s %x", info.Mode(), sha256.Sum256(data))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func updateRefusalWorkspace(t *testing.T) (home, repoRoot string, before func() map[string]string) {
	t.Helper()
	root := canonicalTempDir(t)
	home = filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	repoRoot = filepath.Join(root, "repo")
	return home, repoRoot, func() map[string]string {
		state := make(map[string]string)
		for _, base := range []string{repoRoot, filepath.Join(home, ".agents"), filepath.Join(home, ".factory")} {
			if _, err := os.Lstat(base); err != nil {
				continue
			}
			for rel, identity := range treeStateMap(t, base) {
				state[filepath.Join(base, rel)] = identity
			}
		}
		return state
	}
}

func assertNoPendingDependencyJournal(t *testing.T) {
	t.Helper()
	stateDir, err := lifecycle.DependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("update left a pending dependency transaction: %v", err)
	}
}

func assertTreeStateEqual(t *testing.T, before, after map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		var diff []string
		for path, identity := range after {
			if before[path] != identity {
				diff = append(diff, path)
			}
		}
		for path := range before {
			if _, ok := after[path]; !ok {
				diff = append(diff, path+" (removed)")
			}
		}
		t.Fatalf("update changed lock, config, or harness outputs:\n%s", strings.Join(diff, "\n"))
	}
}

// assertMissFollowedByRemediation checks that the named miss line is directly
// followed by a remediation line.
func assertMissFollowedByRemediation(t *testing.T, combined, name string) {
	t.Helper()
	lines := strings.Split(combined, "\n")
	for i, line := range lines {
		if !strings.Contains(line, name+" was selected but not found in") {
			continue
		}
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  → ") {
			return
		}
		t.Fatalf("miss %s is not followed by a remediation line:\n%s", name, combined)
	}
	t.Fatalf("update refusal did not name %s:\n%s", name, combined)
}

// lockRegistryByURL returns the lock entry for the canonical registry URL.
func lockRegistryWithCanonicalURL(t *testing.T, doc lockfile.Document, canonicalURL string) lockfile.GitRegistry {
	t.Helper()
	for _, registry := range doc.Registries {
		if registry.URL == canonicalURL {
			return registry
		}
	}
	t.Fatalf("lock has no registry %s: %+v", canonicalURL, doc.Registries)
	return lockfile.GitRegistry{}
}

// gitHeadCommit returns the commit HEAD points to in dir in lock OID form.
func gitHeadCommit(t *testing.T, dir string) string {
	t.Helper()
	commit, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return "sha1:" + strings.TrimSpace(string(commit))
}

func assertUpdateRefusedUnchanged(t *testing.T, before, after map[string]string, output string, updateErr error, names ...string) {
	t.Helper()
	if updateErr == nil {
		t.Fatalf("expected update to refuse, got success:\n%s", output)
	}
	combined := output + "\n" + updateErr.Error()
	for _, name := range names {
		assertMissFollowedByRemediation(t, combined, name)
	}
	if !strings.Contains(combined, lifecycle.UpdateSelectionRefusalHint) {
		t.Fatalf("update refusal missing hint:\n%s", combined)
	}
	assertTreeStateEqual(t, before, after)
	assertNoPendingDependencyJournal(t)
}

// spec: resource-selection/renamed-or-removed-direct-selection-refuses-update
func TestUpdateRefusesRenamedDirectSelectionAtomically(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, working string)
	}{
		{
			name: "renamed",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "alpha")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "alpha-renamed", "Alpha v2", nil)
			},
		},
		{
			name: "removed",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "alpha")); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, snapshot := updateRefusalWorkspace(t)

			origin, working := createBareRepoWithWorkingClone(t, "renamed-skill.git")
			writeSkill(t, working, "alpha", "Alpha v1", nil)
			writeSkill(t, working, "beta", "Beta v1", nil)
			commitAndPushAll(t, working, "seed")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/alpha, skill/beta]\n")
			withWorkingDir(t, repoRoot)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			before := snapshot()

			tc.mutate(t, working)
			commitAndPushAll(t, working, tc.name+" alpha")

			output, err := executeCommand("update")
			assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, `skill "alpha"`)
		})
	}
}

// spec: resource-selection/missing-required-selection-leaves-the-lock-unchanged
// spec: resource-selection/required-selection-remediation-does-not-suggest-remove
func TestUpdateRefusesMissingRequiredSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, repoRoot, snapshot := updateRefusalWorkspace(t)

	origin, working := createBareRepoWithWorkingClone(t, "required-skill.git")
	writeSkill(t, working, "required-skill", "Required v1", nil)
	writeSkill(t, working, "kept", "Kept v1", nil)
	commitAndPushAll(t, working, "seed")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/required-skill, skill/kept]\n    required: true\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := snapshot()

	if err := os.RemoveAll(filepath.Join(working, "skills", "required-skill")); err != nil {
		t.Fatal(err)
	}
	commitAndPushAll(t, working, "remove required skill")

	output, err := executeCommand("update")
	if err == nil {
		t.Fatalf("expected update to refuse the missing required selection:\n%s", output)
	}
	combined := output + "\n" + err.Error()
	if strings.Contains(combined, "dependency-committed sync failure") {
		t.Fatalf("update reported a post-commit sync failure instead of a pre-publication refusal:\n%s", combined)
	}
	if !strings.Contains(combined, `skill "required-skill" was selected but not found in`) || !strings.Contains(combined, lifecycle.UpdateSelectionRefusalHint) {
		t.Fatalf("update refusal missing selection name or hint:\n%s", combined)
	}
	if !strings.Contains(combined, lifecycle.UpdateSelectionRequiredLine) || strings.Contains(combined, "agent-marketplace remove --kind skill required-skill") {
		t.Fatalf("required refusal must say remove cannot drop it and must not suggest remove:\n%s", combined)
	}
	assertTreeStateEqual(t, before, snapshot())
	assertNoPendingDependencyJournal(t)

	checkOutput, checkErr := executeCommand("sync", "--check")
	if checkErr != nil {
		t.Fatalf("sync --check after refused update must report the outputs in sync: %v\n%s", checkErr, checkOutput)
	}
}

// spec: resource-selection/missing-collection-or-listed-collection-member-refuses-update
func TestUpdateRefusesMissingCollectionOrListedMember(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name    string
		mutate  func(t *testing.T, working string)
		missing string
	}{
		{
			name: "collection-removed",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "collections", "bundle")); err != nil {
					t.Fatal(err)
				}
			},
			missing: `collection "bundle"`,
		},
		{
			name: "listed-member-renamed",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "beta-renamed", "Beta v2", nil)
			},
			missing: `skill "beta"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, snapshot := updateRefusalWorkspace(t)
			origin, working := createBareRepoWithWorkingClone(t, "collection-"+tc.name+".git")
			writeSkill(t, working, "alpha", "Alpha v1", nil)
			writeSkill(t, working, "beta", "Beta v1", nil)
			writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
skills:
  - name: alpha
  - name: beta
`)
			commitAndPushAll(t, working, "seed")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [collection/bundle]\n")
			withWorkingDir(t, repoRoot)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			before := snapshot()

			tc.mutate(t, working)
			commitAndPushAll(t, working, "mutate collection")

			output, err := executeCommand("update")
			assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, tc.missing)
		})
	}
}

// spec: resource-selection/collection-membership-changes-that-resolve-do-not-refuse-update
func TestUpdateAllowsCollectionMembershipChangesThatResolve(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name     string
		members  string
		mutate   func(t *testing.T, working string)
		material string
	}{
		{
			name:    "member-dropped-from-definition",
			members: "  - name: alpha\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
			},
			material: "alpha",
		},
		{
			name:    "member-renamed-and-definition-follows",
			members: "  - name: alpha\n  - name: beta-renamed\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "beta-renamed", "Beta v2", nil)
			},
			material: "beta-renamed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, _ := updateRefusalWorkspace(t)
			origin, working := createBareRepoWithWorkingClone(t, "membership-"+tc.name+".git")
			writeSkill(t, working, "alpha", "Alpha v1", nil)
			writeSkill(t, working, "beta", "Beta v1", nil)
			commitAndPushAll(t, working, "seed")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [collection/bundle]\n")
			withWorkingDir(t, repoRoot)
			writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
skills:
  - name: alpha
  - name: beta
`)
			commitAndPushAll(t, working, "add collection")
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			beforeCommit := readLockDocument(t, lockPath).Registries[0].Commit

			tc.mutate(t, working)
			writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.1.0
skills:
`+tc.members)
			commitAndPushAll(t, working, "mutate collection")

			output, err := executeCommand("update")
			if err != nil {
				t.Fatalf("update refused a resolvable membership change: %v\n%s", err, output)
			}
			if got := readLockDocument(t, lockPath).Registries[0].Commit; got == beforeCommit {
				t.Fatal("update did not advance the acquisition lock")
			}
			assertFactorySkillExists(t, repoRoot, tc.material)
		})
	}
}

// spec: resource-selection/all-resources-selection-follows-upstream-renames
func TestUpdateFollowsAllResourcesAcrossRenames(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name      string
		resources string
	}{
		{name: "all", resources: "all: true"},
		{name: "all-with-vanished-exclude", resources: "all: true\n    exclude: [skill/beta]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, _ := updateRefusalWorkspace(t)
			origin, working := createBareRepoWithWorkingClone(t, "all-"+tc.name+".git")
			writeSkill(t, working, "alpha", "Alpha v1", nil)
			writeSkill(t, working, "beta", "Beta v1", nil)
			commitAndPushAll(t, working, "seed")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    "+tc.resources+"\n")
			withWorkingDir(t, repoRoot)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			beforeCommit := readLockDocument(t, lockPath).Registries[0].Commit

			if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
				t.Fatal(err)
			}
			writeSkill(t, working, "beta-renamed", "Beta v2", nil)
			commitAndPushAll(t, working, "rename beta")

			output, err := executeCommand("update")
			if err != nil {
				t.Fatalf("update refused an all-resources rename: %v\n%s", err, output)
			}
			if got := readLockDocument(t, lockPath).Registries[0].Commit; got == beforeCommit {
				t.Fatal("update did not advance the acquisition lock")
			}
			assertFactorySkillExists(t, repoRoot, "beta-renamed")
		})
	}
}

// spec: resource-selection/every-newly-missing-selection-is-named-once
func TestUpdateRefusalNamesEveryNewlyMissingSelectionOnce(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repoRoot, snapshot := updateRefusalWorkspace(t)

	origin, working := createBareRepoWithWorkingClone(t, "multi-missing.git")
	writeSkill(t, working, "shared", "Shared v1", nil)
	writeSkill(t, working, "gone", "Gone v1", nil)
	writeSkill(t, working, "kept", "Kept v1", nil)
	commitAndPushAll(t, working, "seed")

	source := "  registry:\n    git: file://" + origin + "\n    ref: main\n"
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+source+"resources:\n  registry:\n    include: [skill/shared]\n")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n"+source+"resources:\n  registry:\n    include: [skill/shared, skill/gone]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := snapshot()

	if err := os.RemoveAll(filepath.Join(working, "skills", "shared")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(working, "skills", "gone")); err != nil {
		t.Fatal(err)
	}
	commitAndPushAll(t, working, "remove selections")

	output, err := executeCommand("update")
	if err == nil {
		t.Fatalf("expected update to refuse, got success:\n%s", output)
	}
	combined := output + "\n" + err.Error()
	for _, name := range []string{`skill "shared"`, `skill "gone"`} {
		if count := strings.Count(err.Error(), name+" was selected but not found in"); count != 1 {
			t.Fatalf("refusal named %s %d times, want once:\n%s", name, count, combined)
		}
	}
	if !strings.Contains(combined, lifecycle.UpdateSelectionRefusalHint) {
		t.Fatalf("update refusal missing hint:\n%s", combined)
	}
	assertTreeStateEqual(t, before, snapshot())
	assertNoPendingDependencyJournal(t)
}

// spec: resource-selection/selection-already-missing-before-update-does-not-refuse
func TestUpdateIgnoresSelectionAlreadyMissingAtPriorRevision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("repo-layer", func(t *testing.T) {
		_, repoRoot, _ := updateRefusalWorkspace(t)

		origin, working := createBareRepoWithWorkingClone(t, "pre-stale.git")
		writeSkill(t, working, "kept", "Kept v1", nil)
		commitAndPushAll(t, working, "seed")

		writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/kept, skill/ghost]\n")
		withWorkingDir(t, repoRoot)
		// skill/ghost never resolves: the initial lock pins a revision where the
		// selection was already stale.
		if output, err := executeCommand("sync"); err != nil {
			t.Fatalf("initial sync: %v\n%s", err, output)
		}
		lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
		beforeCommit := readLockDocument(t, lockPath).Registries[0].Commit

		writeSkill(t, working, "kept", "Kept v2", nil)
		commitAndPushAll(t, working, "unrelated change")

		output, err := executeCommand("update")
		if err != nil {
			t.Fatalf("update refused a selection that was already stale: %v\n%s", err, output)
		}
		if got := readLockDocument(t, lockPath).Registries[0].Commit; got == beforeCommit {
			t.Fatal("update did not advance the acquisition lock")
		}

		status, err := executeCommand("status")
		if err != nil {
			t.Fatalf("status after update: %v\n%s", err, status)
		}
		if !strings.Contains(status, "ghost") || !strings.Contains(status, "not found") {
			t.Fatalf("status no longer reports the stale selection as a warning:\n%s", status)
		}
	})
	t.Run("unlocked-organization", func(t *testing.T) {
		_, repoRoot, _ := updateRefusalWorkspace(t)

		orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, "org-registry.git")
		writeSkill(t, orgWorking, "org-kept", "Org kept v1", nil)
		commitAndPushAll(t, orgWorking, "org seed")
		// The organization layer selects skill/org-ghost, which has never
		// resolved. Its Git source stays unlocked (no organization lock), so
		// it resolves identically in the prior and desired passes.
		authority := canonicalTempDir(t)
		orgPath := filepath.Join(authority, "config.yaml")
		writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  org:\n    git: file://"+orgOrigin+"\n    ref: main\nresources:\n  org:\n    include: [skill/org-ghost]\n")
		if err := os.Chmod(authority, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
		config.SetTestOrganizationConfigPath(t, orgPath)

		repoOrigin, repoWorking := createBareRepoWithWorkingClone(t, "repo-registry.git")
		writeSkill(t, repoWorking, "repo-kept", "Repo kept v1", nil)
		commitAndPushAll(t, repoWorking, "repo seed")
		writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+repoOrigin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/repo-kept]\n")
		withWorkingDir(t, repoRoot)
		if output, err := executeCommand("sync"); err != nil {
			t.Fatalf("initial sync: %v\n%s", err, output)
		}
		lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
		beforeCommit := readLockDocument(t, lockPath).Registries[0].Commit

		writeSkill(t, repoWorking, "repo-kept", "Repo kept v2", nil)
		commitAndPushAll(t, repoWorking, "unrelated change")

		output, err := executeCommand("update")
		if err != nil {
			t.Fatalf("update refused a selection that was already stale: %v\n%s", err, output)
		}
		if got := readLockDocument(t, lockPath).Registries[0].Commit; got == beforeCommit {
			t.Fatal("update did not advance the repository lock")
		}
	})
}

// spec: resource-selection/following-the-remediation-completes-a-rename
func TestUpdateFollowsRenameRemediation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, repoRoot, snapshot := updateRefusalWorkspace(t)

	origin, working := createBareRepoWithWorkingClone(t, "remediated-rename.git")
	writeSkill(t, working, "alpha", "Alpha v1", nil)
	writeSkill(t, working, "beta", "Beta v1", nil)
	commitAndPushAll(t, working, "seed")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/alpha, skill/beta]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := snapshot()

	if err := os.RemoveAll(filepath.Join(working, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "alpha-renamed", "Alpha v2", nil)
	commitAndPushAll(t, working, "rename alpha")

	output, err := executeCommand("update")
	assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, `skill "alpha"`)
	remove := "agent-marketplace remove --kind skill alpha"
	if !strings.Contains(err.Error(), remove) {
		t.Fatalf("refusal does not name %q:\n%s", remove, err)
	}

	if output, err := executeCommand("remove", "--kind", "skill", "alpha"); err != nil {
		t.Fatalf("remove named by refusal: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update after remove: %v\n%s", err, output)
	}
	if got, want := readLockDocument(t, lockPath).Registries[0].Commit.String(), gitHeadCommit(t, origin); got != want {
		t.Fatalf("lock commit = %s, want upstream HEAD %s", got, want)
	}
	if output, err := executeCommand("install", "--scope", "repo", "alpha-renamed"); err != nil {
		t.Fatalf("install renamed skill: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "skill/alpha-renamed") {
		t.Fatalf("config does not select the renamed skill:\n%s", raw)
	}
}

// spec: resource-selection/following-the-remediation-excludes-a-missing-collection-member
func TestUpdateFollowsCollectionMemberRemediation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, repoRoot, snapshot := updateRefusalWorkspace(t)

	origin, working := createBareRepoWithWorkingClone(t, "remediated-member.git")
	writeSkill(t, working, "alpha", "Alpha v1", nil)
	writeSkill(t, working, "beta", "Beta v1", nil)
	writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
skills:
  - name: alpha
  - name: beta
`)
	commitAndPushAll(t, working, "seed")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [collection/bundle]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := snapshot()

	if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "beta-renamed", "Beta v2", nil)
	commitAndPushAll(t, working, "rename beta")

	output, err := executeCommand("update")
	assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, `skill "beta"`)
	remove := "agent-marketplace remove --kind skill beta"
	if !strings.Contains(err.Error(), remove) {
		t.Fatalf("refusal does not name %q:\n%s", remove, err)
	}

	if output, err := executeCommand("remove", "--kind", "skill", "beta"); err != nil {
		t.Fatalf("remove named by refusal: %v\n%s", err, output)
	}
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update after remove: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if got, want := readLockDocument(t, lockPath).Registries[0].Commit.String(), gitHeadCommit(t, origin); got != want {
		t.Fatalf("lock commit = %s, want upstream HEAD %s", got, want)
	}
}

// spec: resource-selection/unloadable-prior-revision-is-named-in-the-refusal
func TestUpdateRefusalNamesUnloadablePriorRevision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repoRoot, snapshot := updateRefusalWorkspace(t)

	origin, working := createBareRepoWithWorkingClone(t, "rewritten-history.git")
	writeSkill(t, working, "alpha", "Alpha v1", nil)
	commitAndPushAll(t, working, "seed")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/alpha, skill/ghost]\n")
	withWorkingDir(t, repoRoot)
	// skill/ghost never resolves, so the locked revision already lacks it.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := snapshot()

	runGit(t, working, "checkout", "--orphan", "rewritten")
	runGit(t, working, "rm", "-rf", "--quiet", ".")
	writeSkill(t, working, "alpha", "Alpha v2", nil)
	runGit(t, working, "add", ".")
	runGit(t, working, "commit", "-m", "rewritten history")
	runGit(t, working, "push", "--force", "origin", "HEAD:main")
	runGit(t, "", "-C", origin, "reflog", "expire", "--expire=now", "--all")
	runGit(t, "", "-C", origin, "gc", "--prune=now")
	if err := os.RemoveAll(filepath.Join(home, ".cache", "agent-marketplace")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("update")
	assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, `skill "ghost"`)
	lockNote := "could not load the previously locked revision for " + filepath.Join(repoRoot, ".agents", "lock.yaml") + " ("
	if !strings.Contains(err.Error(), lockNote) {
		t.Fatalf("refusal does not name the lock whose previous revision could not be loaded:\n%s", err)
	}
}

// spec: resource-selection/targeted-update-of-another-registry-proceeds
func TestUpdateRegistryTargetProceedsPastRefusal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, repoRoot, snapshot := updateRefusalWorkspace(t)

	originA, workingA := createBareRepoWithWorkingClone(t, "registry-a.git")
	writeSkill(t, workingA, "alpha", "Alpha v1", nil)
	commitAndPushAll(t, workingA, "seed a")
	originB, workingB := createBareRepoWithWorkingClone(t, "registry-b.git")
	writeSkill(t, workingB, "beta", "Beta v1", nil)
	commitAndPushAll(t, workingB, "seed b")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  first:\n    git: file://"+originA+"\n    ref: main\n  second:\n    git: file://"+originB+"\n    ref: main\nresources:\n  first:\n    include: [skill/alpha]\n  second:\n    include: [skill/beta]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	canonicalA, err := lifecycle.CanonicalRegistryURL("file://" + originA)
	if err != nil {
		t.Fatal(err)
	}
	canonicalB, err := lifecycle.CanonicalRegistryURL("file://" + originB)
	if err != nil {
		t.Fatal(err)
	}
	lockA := lockRegistryWithCanonicalURL(t, readLockDocument(t, lockPath), canonicalA)
	before := snapshot()

	if err := os.RemoveAll(filepath.Join(workingA, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, workingA, "alpha-renamed", "Alpha v2", nil)
	commitAndPushAll(t, workingA, "rename alpha")
	writeSkill(t, workingB, "beta", "Beta v2", nil)
	commitAndPushAll(t, workingB, "update beta")

	output, err := executeCommand("update")
	assertUpdateRefusedUnchanged(t, before, snapshot(), output, err, `skill "alpha"`)

	if output, err := executeCommand("update", "--registry", "file://"+originB); err != nil {
		t.Fatalf("targeted update of the second registry: %v\n%s", err, output)
	}
	lock := readLockDocument(t, lockPath)
	gotB := lockRegistryWithCanonicalURL(t, lock, canonicalB)
	if got, want := gotB.Commit.String(), gitHeadCommit(t, originB); got != want {
		t.Fatalf("second registry lock commit = %s, want upstream HEAD %s", got, want)
	}
	if got := lockRegistryWithCanonicalURL(t, lock, canonicalA); got.Commit != lockA.Commit {
		t.Fatalf("first registry lock changed from %s to %s", lockA.Commit, got.Commit)
	}
}
