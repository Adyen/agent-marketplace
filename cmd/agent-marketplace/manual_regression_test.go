package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestManualRegressionMatrix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("branch ref gates skill discovery", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origin, working := createBareRepoWithWorkingClone(t, "branch-ref.git")
		writeSkill(t, working, "main-only", "Main branch skill", []string{"main"})
		commitAndPushAll(t, working, "add main-only")

		runGit(t, working, "checkout", "-b", "feature/ref-test")
		writeSkill(t, working, "feature-only", "Feature branch skill", []string{"feature"})
		commitAndPushAll(t, working, "add feature-only")
		runGit(t, working, "push", "origin", "feature/ref-test")
		runGit(t, working, "checkout", "main")

		repoRoot := filepath.Join(t.TempDir(), "repo")
		writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
`)
		withWorkingDir(t, repoRoot)

		if out, err := executeCommand("install", "feature-only"); err == nil || !strings.Contains(err.Error(), "not found in configured registries") {
			t.Fatalf("expected feature-only to fail on main ref, err=%v output=%s", err, out)
		}

		writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: feature/ref-test}
`)
		if out, err := executeCommand("install", "feature-only"); err != nil {
			t.Fatalf("expected feature-only to install on feature ref: %v\n%s", err, out)
		}
		assertFactorySkillExists(t, repoRoot, "feature-only")
	})

	t.Run("multi-registry explicit install binds the registry that provides the skill", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		originA, workingA := createBareRepoWithWorkingClone(t, "registry-a.git")
		writeSkill(t, workingA, "alpha-tool", "Alpha skill", []string{"alpha"})
		commitAndPushAll(t, workingA, "add alpha")

		originB, workingB := createBareRepoWithWorkingClone(t, "registry-b.git")
		writeSkill(t, workingB, "beta-tool", "Beta skill", []string{"beta"})
		commitAndPushAll(t, workingB, "add beta")

		repoRoot := filepath.Join(t.TempDir(), "repo")
		configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
		writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  alpha: {git: file://`+originA+`, ref: main}
  beta: {git: file://`+originB+`, ref: main}
`)
		withWorkingDir(t, repoRoot)

		// With kind-scoped selectors, install writes to the specific registry
		// that provides the skill. alpha-tool exists in registry-a only, so
		// install succeeds and adds it to that entry's skills list.
		out, err := executeCommand("install", "alpha-tool")
		if err != nil {
			t.Fatalf("expected install to succeed when skill is found in at least one registry: %v\n%s", err, out)
		}

		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config after install: %v", err)
		}

		// Read the authored v4 selection through the consumer runtime adapter.
		cfg, err := config.LoadRepoConfig()
		if err != nil {
			t.Fatalf("parse config after install: %v\n%s", err, after)
		}

		// Find the originA registry entry
		var foundOriginA bool
		for _, reg := range cfg.Registries {
			if reg.URL == "file://"+originA {
				foundOriginA = true
				// Verify alpha-tool is in the include list
				found := false
				for _, skill := range reg.Skills.Include {
					if skill == "alpha-tool" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected alpha-tool in originA's skills.include, got: %v", reg.Skills.Include)
				}
				// Verify include list has exactly alpha-tool
				if len(reg.Skills.Include) != 1 || reg.Skills.Include[0] != "alpha-tool" {
					t.Fatalf("expected originA's skills.include to be exactly [alpha-tool], got: %v", reg.Skills.Include)
				}
			}
		}

		if !foundOriginA {
			t.Fatalf("expected to find originA registry in config")
		}

		// Verify originB (if present) doesn't have alpha-tool selector
		for _, reg := range cfg.Registries {
			if reg.URL == "file://"+originB {
				for _, skill := range reg.Skills.Include {
					if skill == "alpha-tool" {
						t.Fatalf("expected alpha-tool NOT to be in originB's skills.include, got: %v", reg.Skills.Include)
					}
				}
			}
		}

		assertFactorySkillExists(t, repoRoot, "alpha-tool")
	})

	t.Run("repo exclude cannot suppress user-layer non-required selection", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		origin, working := createBareRepoWithWorkingClone(t, "precedence.git")
		writeSkill(t, working, "secret-scan", "Secret scanning", []string{"security"})
		commitAndPushAll(t, working, "add security skill")

		writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/secret-scan]
`)

		repoRoot := filepath.Join(t.TempDir(), "repo")
		writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: {exclude: [skill/secret-scan]}
`)
		withWorkingDir(t, repoRoot)

		if out, err := executeCommand("sync"); err != nil {
			t.Fatalf("sync failed: %v\n%s", err, out)
		}

		assertSymlinkTarget(t,
			filepath.Join(homeDir, ".agents", "skills", "secret-scan"),
			filepath.Join(cache.RegistryCachePath(origin, "main"), "secret-scan"),
		)
		assertFactorySkillAbsent(t, repoRoot, "secret-scan")
	})
}
