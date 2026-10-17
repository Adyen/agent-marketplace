package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: resource-installation/bulk-user-selection-refuses-repository-relative-sources
func TestBulkInstallUserRefusesRepoRelativeDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, existing := range []bool{false, true} {
		for _, selector := range [][]string{
			{"--tag", "chosen"},
			{"--tag", "chosen", "--kind", "skill"},
			{"--all"},
			{"--all", "--kind", "skill"},
		} {
			name := selector[0]
			if len(selector) > 2 {
				name += "/kind"
			}
			if existing {
				name += "/existing"
			} else {
				name += "/missing"
			}
			t.Run(name, func(t *testing.T) {
				f := newInstallBindingFixture(t)
				writeSkill(t, f.working, "review", "Content A", []string{"chosen"})
				commitAndPushAll(t, f.working, "tagged inventory")
				writeSkill(t, filepath.Join(f.repo, "resources"), "local-only", "Local", []string{"chosen"})
				writeConfig(t, config.ProjectConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: file://"+f.origin+", ref: main}\n  local: {directory: ./resources}\nresources:\n  hosted: [skill/review]\n")
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("seed source state: %v\n%s", err, out)
				}
				userPath := filepath.Join(f.home, ".agents", "config.yaml")
				if existing {
					writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# Preserve user configuration.\n")
					if out, err := executeCommand("lock", "--scope", "user"); err != nil {
						t.Fatalf("seed user lock: %v\n%s", err, out)
					}
				} else if err := os.Remove(userPath); err != nil {
					t.Fatal(err)
				}
				repoBefore := cliFilesystemSnapshot(t, f.repo)
				userBefore := cliFilesystemSnapshot(t, filepath.Join(f.home, ".agents"))
				machineBefore := cliFilesystemSnapshot(t, filepath.Join(f.home, ".factory"))
				output, err := executeCommand(append([]string{"install", "--scope", "user"}, selector...)...)
				var scopeErr *config.GlobalRelativeRegistryError
				if !errors.As(err, &scopeErr) {
					t.Fatalf("expected scope-boundary refusal, got %v\n%s", err, output)
				}
				assertCliFilesystemSnapshot(t, "bulk refusal", f.repo, repoBefore)
				assertCliFilesystemSnapshot(t, "bulk refusal", filepath.Join(f.home, ".agents"), userBefore)
				assertCliFilesystemSnapshot(t, "bulk refusal", filepath.Join(f.home, ".factory"), machineBefore)
				requireNoInstallStaging(t)
			})
		}
	}
}

// spec: resource-installation/bulk-local-selection-honors-the-current-subdirectory
func TestBulkInstallLocalRebasesInheritedDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, selector := range [][]string{{"--all"}, {"--tag", "chosen"}} {
		t.Run(selector[0], func(t *testing.T) {
			f := newInstallBindingFixture(t)
			source := filepath.Join(f.repo, "resources")
			writeSkill(t, source, "review", "Inherited review", []string{"chosen"})
			writeSkill(t, source, "keep", "Keep parent output", nil)
			writeConfig(t, config.ProjectConfigPath(f.repo), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: ./resources}\nresources:\n  local: [skill/keep]\n")
			if out, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed parent: %v\n%s", err, out)
			}
			parentConfig := cliFilesystemSnapshot(t, filepath.Join(f.repo, ".agents"))
			parentOutput := cliFilesystemSnapshot(t, filepath.Join(f.repo, ".factory"))
			subdir := filepath.Join(f.repo, "packages", "app")
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, subdir)
			output, err := executeCommand(append([]string{"install", "--scope", "local"}, selector...)...)
			if err != nil {
				t.Fatalf("bulk local install: %v\n%s", err, output)
			}
			local, err := config.LoadV4ConfigFromPath(config.LocalConfigPath(subdir))
			if err != nil || len(local.Sources) != 1 || len(local.Groups) != 1 {
				t.Fatalf("local configuration = %#v, %v", local, err)
			}
			assertFileExists(t, filepath.Join(subdir, ".factory", "skills", "review", "SKILL.md"))
			assertCliFilesystemSnapshot(t, "bulk local install", filepath.Join(f.repo, ".agents"), parentConfig)
			assertCliFilesystemSnapshot(t, "bulk local install", filepath.Join(f.repo, ".factory"), parentOutput)
			if out, err := executeCommand("sync", "--check"); err != nil {
				t.Fatalf("check local install: %v\n%s", err, out)
			}
			requireNoInstallStaging(t)
		})
	}
}
