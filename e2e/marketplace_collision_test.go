package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketplaceInstallChecksReconciledCollision(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("APPROVED MARKETPLACE BODY", true))
	for _, tc := range []struct {
		name       string
		user       bool
		collection bool
		retainLock bool
		safeUser   bool
	}{
		{name: "missing_repo_lock"},
		{name: "missing_required_user_lock", user: true},
		{name: "missing_repo_collection_lock", collection: true},
		{name: "retained_repo_lock", retainLock: true},
		{name: "missing_user_lock_safe_winner", user: true, safeUser: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumer := fixture.newConsumer(t, fixture.server.URL)
			url := consumer.CreateBareRepo("git-collider", SkillsOnly(Skill{Name: "keeper", Description: "KEEPER"}))
			mpConfig := fmt.Sprintf("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: %s\n    instance_id: behavior-marketplace\n    alias: behavior\n", fixture.server.URL)
			configure := func(selection string) {
				gitConfig := fmt.Sprintf("registries:\n  - url: %s\n    ref: main\n    source_harness: claude\n%s", url, selection)
				if tc.user {
					if !tc.safeUser && strings.Contains(selection, "default: true") {
						gitConfig += "    required: true\n"
					}
					consumer.WriteGlobalConfig("harnesses: [factory]\n" + gitConfig)
					consumer.WriteRepoConfig(mpConfig)
				} else {
					consumer.WriteRepoConfig(mpConfig + gitConfig)
				}
			}
			configure("    default: true\n")
			consumer.MustRunAM("sync")
			consumer.MustRunAM("list", "--from", url, "--ref", "main")
			ownerRoot := consumer.workspace
			if tc.user {
				ownerRoot = consumer.home
			}
			keeper := filepath.Join(ownerRoot, ".factory", "skills", "keeper", "SKILL.md")
			if raw, err := os.ReadFile(keeper); err != nil || !strings.Contains(string(raw), "KEEPER") {
				t.Fatalf("initial keeper missing: %v", err)
			}
			lockPath := filepath.Join(ownerRoot, ".agents", "lock.yaml")
			lockBytes, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.retainLock {
				if err := os.Rename(lockPath, lockPath+".saved"); err != nil {
					t.Fatal(err)
				}
			}
			work := filepath.Join(t.TempDir(), "collider")
			gitRun(t, "", "clone", "-b", "main", strings.TrimPrefix(url, "file://"), work)
			skillPath := filepath.Join(work, "skills", "skill-one", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(skillPath, []byte("---\nname: skill-one\ndescription: Git collision\n---\nUNREVIEWED GIT BODY\n"), 0644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, work, "add", ".")
			gitRun(t, work, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-m", "introduce colliding resource")
			gitRun(t, work, "push", "origin", "main")
			configPath := filepath.Join(consumer.workspace, ".agents", "config.yaml")
			configBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			configInfo, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			outputs := map[string]string{}
			for _, root := range []string{consumer.home, consumer.workspace} {
				path := filepath.Join(root, ".factory")
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatal(err)
				}
				fingerprint, err := pathFingerprint(path)
				if err != nil {
					t.Fatal(err)
				}
				outputs[path] = fingerprint
			}
			locator := "behavior:behavior-source/skill/skill-one"
			if tc.collection {
				locator = "behavior:behavior-source/collection/behavior-bundle"
			}
			result := consumer.RunAM("install", locator, "--yes", "--scope", "repo")
			if tc.retainLock || tc.safeUser {
				if result.ExitCode != 0 {
					t.Fatalf("locked noncollision refused: %s", result.Combined)
				}
				consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "APPROVED MARKETPLACE BODY")
				if tc.retainLock {
					if got, err := os.ReadFile(lockPath); err != nil || string(got) != string(lockBytes) {
						t.Fatalf("retained lock changed: %v", err)
					}
				} else {
					consumer.AssertFileAbsent("/root/.factory/skills/skill-one")
					consumer.MustRunAM("lock", "--check")
				}
				return
			}
			if result.ExitCode == 0 {
				installed, readErr := os.ReadFile(filepath.Join(ownerRoot, ".factory", "skills", "skill-one", "SKILL.md"))
				t.Fatalf("collision accepted; installed bytes=%q read error=%v output=%s", installed, readErr, result.Combined)
			}
			if !strings.Contains(result.Combined, "marketplace.install-collision") || strings.Contains(result.Combined, "Installed skill-one") {
				t.Fatalf("reconciled collision not refused (exit=%d):\n%s", result.ExitCode, result.Combined)
			}
			if got, err := os.ReadFile(configPath); err != nil || string(got) != string(configBytes) {
				t.Fatalf("Marketplace pin was not rolled back: %v\n%s", err, got)
			}
			if got, err := os.Stat(configPath); err != nil || got.Mode() != configInfo.Mode() {
				t.Fatalf("config permissions changed: %v", err)
			}
			for path, before := range outputs {
				after, err := pathFingerprint(path)
				if err != nil || after != before {
					t.Fatalf("collision refusal changed output %s: %v", path, err)
				}
			}
			consumer.AssertFileAbsent("/workspace/.factory/skills/skill-one")
			consumer.AssertFileAbsent("/workspace/.factory/commands/command-one.md")
			if _, err := os.Stat(lockPath); err != nil {
				t.Fatalf("dependency reconciliation did not run: %v", err)
			}
			consumer.MustRunAM("lock", "--check")
			configure("    skills: [keeper]\n")
			consumer.MustRunAM("install", locator, "--yes", "--scope", "repo")
			consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "APPROVED MARKETPLACE BODY")
			if tc.collection {
				consumer.AssertFileContains("/workspace/.factory/commands/command-one.md", "echo behavior")
			}
		})
	}
}
