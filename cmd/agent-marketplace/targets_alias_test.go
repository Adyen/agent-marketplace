package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func writeTargetAliasDirectory(t *testing.T, root, name string) string {
	t.Helper()
	directory := filepath.Join(root, name)
	writeSkill(t, directory, name+"-skill", name+" target alias fixture", nil)
	return directory
}

func targetAliasLayerConfig(list, source, directory, skill string) string {
	harnessLine := ""
	if list != "" {
		harnessLine = list + "\n"
	}
	return fmt.Sprintf(`apiVersion: agent-marketplace/v4
%ssources:
  %s: {directory: %q}
resources:
  %s: {include: [skill/%s-skill]}
`, harnessLine, source, directory, source, skill)
}

func targetAliasStatusRows(t *testing.T, output string) []string {
	t.Helper()
	sections := strings.SplitN(output, "TARGET HARNESS SET\n", 2)
	if len(sections) != 2 {
		t.Fatalf("status omitted target-layer report:\n%s", output)
	}
	table := strings.SplitN(sections[1], "\n\n", 2)
	if len(table) != 2 {
		t.Fatalf("status target-layer table is incomplete:\n%s", output)
	}
	var rows []string
	for _, line := range strings.Split(table[0], "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			rows = append(rows, fields[0]+"="+fields[1])
		}
	}
	return rows
}

func assertTargetsValidation(t *testing.T, err error, code, file string, fields ...string) {
	t.Helper()
	var validation *config.V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %v, want v4 validation %s", err, code)
	}
	canonical, evalErr := filepath.EvalSymlinks(file)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if validation.File != canonical {
		t.Fatalf("validation file = %q, want %q", validation.File, canonical)
	}
	for _, field := range fields {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name %q", err, field)
		}
	}
}

// spec: consumer-config-format/existing-harnesses-configurations-resolve-unchanged
func TestExistingHarnessesConfigurationsResolveUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name       string
		userList   string
		targetRows []string
	}{
		{name: "absent user list", targetRows: []string{"claude=REPO", "factory=ORG"}},
		{name: "empty harnesses user list", userList: "harnesses: []", targetRows: []string{"claude=REPO", "factory=ORG"}},
		{name: "non-empty target lists at every layer", userList: "harnesses: [factory]", targetRows: []string{"claude=REPO", "factory=USER"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, home, repo := setupV4ConsumerCommand(t)
			orgDirectory := writeTargetAliasDirectory(t, root, "organization")
			userDirectory := writeTargetAliasDirectory(t, root, "user")
			repoDirectory := writeTargetAliasDirectory(t, root, "repository")

			orgPath := filepath.Join(root, "organization.yaml")
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, orgPath, targetAliasLayerConfig("harnesses: [factory]", "organization", orgDirectory, "organization"))
			writeFile(t, filepath.Join(home, ".agents", "config.yaml"), targetAliasLayerConfig(test.userList, "personal", userDirectory, "user"))
			writeConfig(t, config.ProjectConfigPath(repo), targetAliasLayerConfig("harnesses: [claude]", "repository", repoDirectory, "repository"))

			if output, err := executeCommand("sync", "--no-backup"); err != nil {
				t.Fatalf("sync: %v\n%s", err, output)
			}
			status, err := executeCommand("status")
			if err != nil {
				t.Fatalf("status: %v\n%s", err, status)
			}
			gotRows := targetAliasStatusRows(t, status)
			if !reflect.DeepEqual(gotRows, test.targetRows) {
				t.Errorf("status target rows = %v, want base-reader rows %v", gotRows, test.targetRows)
			}
			repositorySkill := filepath.Join(repo, ".claude", "skills", "repository-skill", "SKILL.md")
			if _, err := os.Stat(repositorySkill); err != nil {
				t.Errorf("sync did not materialize repository target control at %s: %v", repositorySkill, err)
			}
			if !strings.Contains(status, "repository-skill") {
				t.Errorf("status omitted repository target control:\n%s", status)
			}
		})
	}
}

// spec: consumer-config-format/root-and-group-targets-resolve-like-harnesses
// spec: consumer-config-format/unknown-target-ids-under-targets-are-refused
func TestTargetsAliasSyncAndStatus(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("root and group aliases", func(t *testing.T) {
		root, home, repo := setupV4ConsumerCommand(t)
		source := writeTargetAliasDirectory(t, root, "alias")
		userPath := filepath.Join(home, ".agents", "config.yaml")
		writeFile(t, userPath, fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [factory]
sources:
  local: {directory: %q}
resources:
  local:
    include: [skill/alias-skill]
    targets: [factory, claude]
`, source))
		if output, err := executeCommand("sync", "--no-backup"); err != nil {
			t.Fatalf("sync: %v\n%s", err, output)
		}
		factory := filepath.Join(home, ".factory", "skills", "alias-skill", "SKILL.md")
		claude := filepath.Join(home, ".claude", "skills", "alias-skill", "SKILL.md")
		if _, err := os.Stat(factory); err != nil {
			t.Fatalf("group target did not resolve to Factory: %v", err)
		}
		if _, err := os.Stat(claude); !os.IsNotExist(err) {
			t.Fatalf("group targets widened the root selection: stat error = %v", err)
		}
		status, err := executeCommand("status")
		if err != nil {
			t.Fatalf("status: %v\n%s", err, status)
		}
		if !strings.Contains(status, "alias-skill") {
			t.Fatalf("status omitted the target alias selection:\n%s", status)
		}
		_ = repo
	})

	t.Run("unknown ID", func(t *testing.T) {
		_, home, repo := setupV4ConsumerCommand(t)
		userPath := filepath.Join(home, ".agents", "config.yaml")
		writeFile(t, userPath, "apiVersion: agent-marketplace/v4\ntargets: [future-tool]\n")
		_, err := executeCommand("sync", "--no-backup")
		assertTargetsValidation(t, err, "v4.harness", userPath, "future-tool")
		if _, err := os.Stat(filepath.Join(home, ".factory", "skills")); !os.IsNotExist(err) {
			t.Fatalf("unknown target created machine output: %v", err)
		}
		if _, err := os.Stat(config.ProjectConfigPath(repo)); !os.IsNotExist(err) {
			t.Fatalf("unknown target created repository config: %v", err)
		}
	})
}

// spec: consumer-config-format/both-spellings-in-one-file-are-refused
func TestTargetsAliasRefusesBeforeSyncWrites(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, groupConflict := range []bool{false, true} {
		name := "root spellings"
		if groupConflict {
			name = "group spellings"
		}
		t.Run(name, func(t *testing.T) {
			root, home, _ := setupV4ConsumerCommand(t)
			origin, working := createBareRepoWithWorkingClone(t, "targets-alias-refusal.git")
			writeSkill(t, working, "review", "Alias refusal control", nil)
			commitAndPushAll(t, working, "seed refusal control")
			userPath := filepath.Join(home, ".agents", "config.yaml")
			base := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			if groupConflict {
				base += "sources:\n  personal:\n    git: file://" + origin + "\n    ref: main\nresources:\n  personal:\n    include: [skill/review]\n    harnesses: [factory]\n"
			} else {
				base = "apiVersion: agent-marketplace/v4\ntargets: [factory]\nharnesses: [factory]\n" +
					"sources:\n  personal:\n    git: file://" + origin + "\n    ref: main\nresources:\n  personal: [skill/review]\n"
			}
			if groupConflict {
				base = strings.Replace(base, "    harnesses: [factory]\n", "    targets: [factory]\n    harnesses: [factory]\n", 1)
			}
			writeFile(t, userPath, base)
			valid := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  personal:\n    git: file://" + origin + "\n    ref: main\nresources:\n  personal: [skill/review]\n"
			writeFile(t, userPath, valid)
			if output, err := executeCommand("sync", "--no-backup"); err != nil {
				t.Fatalf("positive-control sync: %v\n%s", err, output)
			}
			lockPath := filepath.Join(home, ".agents", "lock.yaml")
			lockBefore, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
			installedBefore, err := os.ReadFile(installed)
			if err != nil {
				t.Fatal(err)
			}

			writeFile(t, userPath, base)
			configBefore, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"sync", "--no-backup"}, {"status"}} {
				_, err := executeCommand(args...)
				assertTargetsValidation(t, err, "v4.targets-alias", userPath, "targets", "harnesses")
				configAfter, readErr := os.ReadFile(userPath)
				if readErr != nil || !reflect.DeepEqual(configAfter, configBefore) {
					t.Fatalf("%v changed config bytes: %v", args, readErr)
				}
				lockAfter, readErr := os.ReadFile(lockPath)
				if readErr != nil || !reflect.DeepEqual(lockAfter, lockBefore) {
					t.Fatalf("%v changed lock bytes: %v", args, readErr)
				}
				installedAfter, readErr := os.ReadFile(installed)
				if readErr != nil || !reflect.DeepEqual(installedAfter, installedBefore) {
					t.Fatalf("%v changed installed output: %v", args, readErr)
				}
			}
			_ = root
		})
	}
}

// spec: resource-selection/explicit-empty-user-target-list-selects-no-machine-targets
func TestUserTargetsEmptySyncStatusAndDeinit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, home, repo := setupV4ConsumerCommand(t)
	orgDirectory := writeTargetAliasDirectory(t, root, "organization")
	userDirectory := writeTargetAliasDirectory(t, root, "user")
	repoDirectory := writeTargetAliasDirectory(t, root, "repository")
	orgPath := filepath.Join(root, "organization.yaml")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeFile(t, orgPath, targetAliasLayerConfig("harnesses: [factory]", "organization", orgDirectory, "organization"))
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userPath, targetAliasLayerConfig("targets: []", "personal", userDirectory, "user"))
	writeConfig(t, config.ProjectConfigPath(repo), targetAliasLayerConfig("harnesses: [claude]", "repository", repoDirectory, "repository"))

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	for _, machineSkill := range []string{"organization-skill", "user-skill"} {
		path := filepath.Join(home, ".factory", "skills", machineSkill, "SKILL.md")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("explicit empty user targets installed machine output %s: %v", path, err)
		}
	}
	repoSkill := filepath.Join(repo, ".claude", "skills", "repository-skill", "SKILL.md")
	if _, err := os.Stat(repoSkill); err != nil {
		t.Fatalf("repository target control was not installed: %v", err)
	}
	status, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, status)
	}
	rows := targetAliasStatusRows(t, status)
	if !reflect.DeepEqual(rows, []string{"claude=REPO", "none=configured"}) ||
		!strings.Contains(status, "USER (user and organization selections)") {
		t.Fatalf("status rows for explicit empty user target list = %v, want [claude=REPO none=configured] with the user source\n%s", rows, status)
	}

	if output, err := executeCommand("deinit", "--scope", "user"); err != nil {
		t.Fatalf("deinit --scope user: %v\n%s", err, output)
	}
	if _, err := os.Stat(repoSkill); err != nil {
		t.Fatalf("deinit --scope user removed repository output: %v", err)
	}
	for _, machineSkill := range []string{"organization-skill", "user-skill"} {
		path := filepath.Join(home, ".factory", "skills", machineSkill, "SKILL.md")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("deinit --scope user created machine output %s: %v", path, err)
		}
	}

	writeFile(t, userPath, targetAliasLayerConfig("", "personal", userDirectory, "user"))
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync after deleting targets key: %v\n%s", err, output)
	}
	for _, machineSkill := range []string{"organization-skill", "user-skill"} {
		path := filepath.Join(home, ".factory", "skills", machineSkill, "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("deleting targets key did not restore inherited Factory output %s: %v", path, err)
		}
	}
}
