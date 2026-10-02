package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: resource-selection/repository-winner-does-not-displace-a-user-scope-selection
// spec: materialization/machine-scope-hook-command-uses-the-absolute-machine-root
func TestSyncCrossScopeSkillAndHookStayInSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "user-resources.git")
	writeSkill(t, working, "shared", "User selection", nil)
	writeFile(t, filepath.Join(working, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/hooks/user-check.sh"}]}]}}`)
	commitAndPushAll(t, working, "Add user resources")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory, devin]\nsources: {user: {git: 'file://"+origin+"', ref: main}}\nresources: {user: {include: [skill/shared, hook/PreToolUse-command]}}\n")

	repo := filepath.Join(t.TempDir(), "repo")
	local := filepath.Join(repo, "resources")
	writeSkill(t, local, "shared", "Repository selection", nil)
	writeFile(t, filepath.Join(local, "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/hooks/repo-check.sh"}]}]}}`)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared, hook/PreToolUse-command]}}\n")
	withWorkingDir(t, repo)

	assertState := func() {
		t.Helper()
		for root, description := range map[string]string{home: "User selection", repo: "Repository selection"} {
			content, err := os.ReadFile(filepath.Join(root, ".factory", "skills", "shared", "SKILL.md"))
			if err != nil || !strings.Contains(string(content), description) {
				t.Fatalf("skill in %s: %v\n%s", root, err, content)
			}
		}
		if _, err := os.Lstat(filepath.Join(home, ".config", "devin", "skills", "shared")); err != nil {
			t.Fatalf("user Devin skill was pruned: %v", err)
		}
		machineHooks, err := os.ReadFile(filepath.Join(home, ".factory", "hooks", "hooks.json"))
		if err != nil || !strings.Contains(string(machineHooks), filepath.Join(home, ".factory", "hooks", "user-check.sh")) {
			t.Fatalf("machine hook command: %v\n%s", err, machineHooks)
		}
		repoHooks, err := os.ReadFile(filepath.Join(repo, ".factory", "hooks", "hooks.json"))
		if err != nil || !strings.Contains(string(repoHooks), ".factory/hooks/repo-check.sh") {
			t.Fatalf("repository hook command: %v\n%s", err, repoHooks)
		}
	}
	run := func(root string, args ...string) {
		t.Helper()
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		out, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%s in %s: %v\n%s", strings.Join(args, " "), root, err, out)
		}
		if strings.Contains(out, "multiple registries") {
			t.Fatalf("cross-scope selection reported a collision: %s", out)
		}
	}
	for _, first := range []string{repo, home} {
		run(first, "sync")
		if first == repo {
			run(home, "sync")
		} else {
			run(repo, "sync")
		}
		assertState()
		run(repo, "sync", "--check")
		run(home, "sync", "--check")
	}
}

// spec: resource-selection/collection-member-is-not-displaced-by-a-selection-in-the-other-scope
// spec: resource-selection/required-selection-through-a-collection-suppresses-a-repository-selection
func TestSyncCrossScopeCollectionMemberStaysInSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name string
		// organization selects the collection as required organization
		// policy; otherwise user configuration selects it.
		organization bool
	}{
		{name: "user collection member"},
		{name: "required organization collection member", organization: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			origin, working := createBareRepoWithWorkingClone(t, "machine-resources.git")
			writeSkill(t, working, "shared", "Machine collection member", nil)
			writeCollection(t, working, "bundle", "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n")
			commitAndPushAll(t, working, "Add collection")
			selection := "sources: {machine: {git: 'file://" + origin + "', ref: main}}\nresources: {machine: {include: [collection/bundle]"
			userConfig := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			organizationPath := filepath.Join(t.TempDir(), "config.yaml")
			if tc.organization {
				writeFile(t, organizationPath, "apiVersion: agent-marketplace/v4\n"+selection+", required: true}}\n")
			} else {
				userConfig += selection + "}}\n"
			}
			config.SetTestOrganizationConfigPath(t, organizationPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), userConfig)

			repo := filepath.Join(t.TempDir(), "repo")
			writeSkill(t, filepath.Join(repo, "resources"), "shared", "Repository selection", nil)
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n")
			withWorkingDir(t, repo)

			assertState := func() {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(home, ".factory", "skills", "shared", "SKILL.md"))
				if err != nil || !strings.Contains(string(content), "Machine collection member") {
					t.Fatalf("machine collection member: %v\n%s", err, content)
				}
				repoSkill := filepath.Join(repo, ".factory", "skills", "shared")
				if tc.organization {
					if _, err := os.Lstat(repoSkill); !os.IsNotExist(err) {
						t.Fatalf("repository skill shadows the required collection member: %v", err)
					}
					return
				}
				content, err = os.ReadFile(filepath.Join(repoSkill, "SKILL.md"))
				if err != nil || !strings.Contains(string(content), "Repository selection") {
					t.Fatalf("repository skill: %v\n%s", err, content)
				}
			}
			run := func(root string, args ...string) {
				t.Helper()
				if err := os.Chdir(root); err != nil {
					t.Fatal(err)
				}
				out, err := executeCommand(args...)
				if err != nil {
					t.Fatalf("%s in %s: %v\n%s", strings.Join(args, " "), root, err, out)
				}
				// Only the required organization member suppresses the
				// repository skill, and only a run that sees both reports it.
				wantWarning := tc.organization && root == repo
				if got := strings.Contains(out, "multiple registries"); got != wantWarning {
					t.Fatalf("%s in %s: collision warning = %v, want %v\n%s", strings.Join(args, " "), root, got, wantWarning, out)
				}
			}
			for _, first := range []string{repo, home} {
				run(first, "sync")
				if first == repo {
					run(home, "sync")
				} else {
					run(repo, "sync")
				}
				assertState()
				run(repo, "sync", "--check")
				run(home, "sync", "--check")
			}
		})
	}
}
