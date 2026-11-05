package main

import (
	"encoding/json"
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
	writeFile(t, filepath.Join(working, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/hooks/user-check.sh"}]}]}`)
	writeFile(t, filepath.Join(working, "hooks", "user-check.sh"), "#!/bin/sh\necho user-scope-check\n")
	commitAndPushAll(t, working, "Add user resources")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory, devin]\nsources: {user: {git: 'file://"+origin+"', ref: main, source_harness: factory}}\nresources: {user: {include: [skill/shared, hook/PreToolUse-command]}}\n")

	repo := filepath.Join(t.TempDir(), "repo")
	local := filepath.Join(repo, "resources")
	writeSkill(t, local, "shared", "Repository selection", nil)
	writeFile(t, filepath.Join(local, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/hooks/repo-check.sh"}]}]}`)
	writeFile(t, filepath.Join(local, "hooks", "repo-check.sh"), "#!/bin/sh\necho repo-scope-check\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources, source_harness: factory}}\nresources: {repo: {include: [skill/shared, hook/PreToolUse-command]}}\n")
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
		hookCommand := func(base, file string) string {
			t.Helper()
			data, err := os.ReadFile(filepath.Join(base, ".factory", "hooks.json"))
			if err != nil {
				t.Fatalf("read %s hook file: %v", file, err)
			}
			var document map[string][]struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("decode %s hook file: %v\n%s", file, err, data)
			}
			groups, ok := document["PreToolUse"]
			if !ok || len(document) != 1 || len(groups) != 1 {
				t.Fatalf("%s hook file PreToolUse groups = %v, want exactly one\n%s", file, document["PreToolUse"], data)
			}
			if groups[0].Matcher != "Execute" {
				t.Fatalf("%s hook matcher = %q, want Execute", file, groups[0].Matcher)
			}
			if len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" {
				t.Fatalf("%s hook handlers = %+v, want exactly one command handler", file, groups[0].Hooks)
			}
			return groups[0].Hooks[0].Command
		}
		installedScript := func(base, name, wantContent string) string {
			t.Helper()
			matches, err := filepath.Glob(filepath.Join(base, ".factory", "hook-support", "*", "PreToolUse-command", "hooks", name))
			if err != nil || len(matches) != 1 {
				t.Fatalf("installed %s script matches = %v, %v; want exactly one", name, matches, err)
			}
			data, err := os.ReadFile(matches[0])
			if err != nil || string(data) != wantContent {
				t.Fatalf("installed %s script: %v\n%s", name, err, data)
			}
			return matches[0]
		}
		machineScript := installedScript(home, "user-check.sh", "#!/bin/sh\necho user-scope-check\n")
		machineCommand := hookCommand(home, "machine")
		if want := filepath.ToSlash(machineScript); machineCommand != want {
			t.Fatalf("machine hook command = %q, want installed support path %q", machineCommand, want)
		}
		repoScript := installedScript(repo, "repo-check.sh", "#!/bin/sh\necho repo-scope-check\n")
		repoRel, err := filepath.Rel(repo, repoScript)
		if err != nil {
			t.Fatalf("repository support script is outside the repository: %v", err)
		}
		repoCommand := hookCommand(repo, "repository")
		if want := `"$FACTORY_PROJECT_DIR"/` + filepath.ToSlash(repoRel); repoCommand != want {
			t.Fatalf("repository hook command = %q, want %q", repoCommand, want)
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

// spec: resource-selection/required-collection-member-displaces-an-optional-selection-in-the-same-scope
func TestSyncRequiredCollectionMemberDisplacesUserSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, "org-resources.git")
	writeSkill(t, orgWorking, "shared", "Mandatory collection member", nil)
	writeCollection(t, orgWorking, "bundle", "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n")
	commitAndPushAll(t, orgWorking, "Add collection")
	userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user-resources.git")
	writeSkill(t, userWorking, "shared", "Optional user selection", nil)
	commitAndPushAll(t, userWorking, "Add skill")

	organizationPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, organizationPath, "apiVersion: agent-marketplace/v4\nsources: {org: {git: 'file://"+orgOrigin+"', ref: main}}\nresources: {org: {include: [collection/bundle], required: true}}\n")
	config.SetTestOrganizationConfigPath(t, organizationPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: 'file://"+userOrigin+"', ref: main}}\nresources: {user: {include: [skill/shared]}}\n")

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {}\n")
	withWorkingDir(t, repo)

	run := func(root string, args ...string) {
		t.Helper()
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		out, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%s in %s: %v\n%s", strings.Join(args, " "), root, err, out)
		}
		for _, want := range []string{"multiple registries", "user-resources"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s in %s: output lacks %q\n%s", strings.Join(args, " "), root, want, out)
			}
		}
	}
	for _, first := range []string{repo, home} {
		second := home
		if first == home {
			second = repo
		}
		run(first, "sync")
		run(second, "sync")
		content, err := os.ReadFile(filepath.Join(home, ".factory", "skills", "shared", "SKILL.md"))
		if err != nil || !strings.Contains(string(content), "Mandatory collection member") || strings.Contains(string(content), "Optional user selection") {
			t.Fatalf("machine skill must come from the required collection member: %v\n%s", err, content)
		}
		if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "shared")); !os.IsNotExist(err) {
			t.Fatalf("repository must not install the skill: %v", err)
		}
		run(repo, "sync", "--check")
		run(home, "sync", "--check")
	}
}
