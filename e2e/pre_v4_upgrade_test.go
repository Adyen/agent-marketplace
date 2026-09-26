package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preV4ReleaseArch pins the last stable release before v4 (v3.1.1); only a
// linux-amd64 asset was published.
var preV4ReleaseArch = map[string]string{
	"3e66fd4d88c03282e7505c60ee912c6f9d0f66a7db91b0d8512e6090993ab059": "amd64",
}

func upgradeImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("AM_E2E_UPGRADE_IMAGE")
	if image == "" {
		t.Fatal("AM_E2E_UPGRADE_IMAGE must name a local container image that provides git")
	}
	return image
}

func TestPreV4ReleaseUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	root := world.root
	candidate := world.buildCandidate(arch, "e2e")

	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "workspace")
	registry := filepath.Join(root, "registry")
	for _, dir := range []string{home, workspace, filepath.Join(registry, "skills", "release-review"), filepath.Join(registry, "skills", "after-upgrade"), filepath.Join(workspace, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const skill = "---\nname: release-review\ndescription: Historical release review\n---\n\n# Release Review\n\nHistorical content.\n"
	const extraSkill = "---\nname: after-upgrade\ndescription: Another historical resource\n---\n\n# After Upgrade\n\nStill installable.\n"
	if err := os.WriteFile(filepath.Join(registry, "skills", "release-review", "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "skills", "after-upgrade", "SKILL.md"), []byte(extraSkill), 0o600); err != nil {
		t.Fatal(err)
	}
	runCommand := func(executable string, args ...string) (string, error) {
		t.Helper()
		return world.run(home, workspace, executable, args...)
	}
	run := func(executable string, args ...string) string {
		t.Helper()
		return world.must(home, workspace, executable, args...)
	}

	run(historical, "init", "--harnesses", "factory")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userBefore, err := os.ReadFile(userConfig)
	if err != nil || !strings.Contains(string(userBefore), "agent-marketplace/v2") {
		t.Fatalf("v3.1.1 init did not create the legacy user config: %v, %q", err, userBefore)
	}
	repoConfig := filepath.Join(workspace, ".agents", "config.yaml")
	legacy := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - path: %q\n    skills:\n      include: [release-review]\n", registry)
	if err := os.WriteFile(repoConfig, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	run(historical, "sync")
	installed := filepath.Join(workspace, ".factory", "skills", "release-review", "SKILL.md")
	assertHistoricalSkill := func() {
		t.Helper()
		content, err := os.ReadFile(installed)
		if err != nil || string(content) != skill {
			t.Fatalf("installed historical skill = %q, %v; want exact source content", content, err)
		}
	}
	assertHistoricalSkill()
	run(historical, "sync", "--check")
	run(historical, "install", "after-upgrade", "--scope", "repo")
	extraInstalled := filepath.Join(workspace, ".factory", "skills", "after-upgrade", "SKILL.md")
	if got, err := os.ReadFile(extraInstalled); err != nil || string(got) != extraSkill {
		t.Fatalf("v3.1.1 cannot install second resource: %q, %v", got, err)
	}
	run(historical, "remove", "after-upgrade", "--scope", "repo")
	if _, err := os.Lstat(extraInstalled); !os.IsNotExist(err) {
		t.Fatalf("v3.1.1 removal left second resource: %v", err)
	}
	legacyBeforeMigration, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	if output := run(candidate, "list"); !strings.Contains(output, "release-review") {
		t.Fatalf("candidate cannot browse legacy resource:\n%s", output)
	}
	if got, err := os.ReadFile(repoConfig); err != nil || string(got) != string(legacyBeforeMigration) {
		t.Fatalf("candidate list rewrote legacy config: %q, %v", got, err)
	}
	run(candidate, "sync")
	assertHistoricalSkill()
	run(candidate, "migrate", "--scope", "repo")
	migrated, err := os.ReadFile(repoConfig)
	if err != nil || !strings.Contains(string(migrated), "agent-marketplace/v4") {
		t.Fatalf("repo migration failed: %q, %v", migrated, err)
	}
	if got, err := os.ReadFile(userConfig); err != nil || string(got) != string(userBefore) {
		t.Fatalf("repo migration changed user config: %q, %v", got, err)
	}
	run(candidate, "sync")
	assertHistoricalSkill()
	run(candidate, "migrate", "--scope", "user")
	run(candidate, "sync", "--check")
	assertHistoricalSkill()
	if output := run(candidate, "status"); !strings.Contains(output, "release-review") {
		t.Fatalf("migrated status lost release-review:\n%s", output)
	}
	run(candidate, "install", "after-upgrade", "--scope", "repo")
	if got, err := os.ReadFile(extraInstalled); err != nil || string(got) != extraSkill {
		t.Fatalf("migrated installation lost second resource: %q, %v", got, err)
	}
	removeOutput := run(candidate, "remove", "after-upgrade", "--scope", "repo")
	if _, err := os.Lstat(extraInstalled); !os.IsNotExist(err) {
		configData, _ := os.ReadFile(repoConfig)
		t.Fatalf("migrated removal left second resource: %v; output=%q config=%q", err, removeOutput, configData)
	}
	run(candidate, "sync", "--check")
	assertHistoricalSkill()
	backups, err := filepath.Glob(repoConfig + ".pre-v4.*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("repository backups = %v, %v; want one", backups, err)
	}
	if got, err := os.ReadFile(backups[0]); err != nil || string(got) != string(legacyBeforeMigration) {
		t.Fatalf("backup did not preserve old repository bytes: %q, %v", got, err)
	}
	if stat, err := os.Stat(backups[0]); err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, %v; want 0600", stat, err)
	}
	finalConfig, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	run(candidate, "migrate", "--scope", "repo")
	if got, err := os.ReadFile(repoConfig); err != nil || string(got) != string(finalConfig) {
		t.Fatalf("repeated migration rewrote repository config: %q, %v", got, err)
	}

	fragment := filepath.Join(workspace, ".agents", "fragments", "resources.yaml")
	if err := os.MkdirAll(filepath.Dir(fragment), 0o700); err != nil {
		t.Fatal(err)
	}
	fragmentData := fmt.Sprintf("registries:\n  - path: %q\n    skills: {include: [release-review]}\n", registry)
	if err := os.WriteFile(fragment, []byte(fragmentData), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyRoot := "apiVersion: agent-marketplace/v2\ninclude: fragments/resources.yaml\n"
	if err := os.WriteFile(repoConfig, []byte(legacyRoot), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, userBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	run(historical, "sync", "--check")
	refusal, err := runCommand(candidate, "migrate", "--scope", "repo")
	if err == nil || !strings.Contains(refusal, "v4.migration-unsupported") {
		t.Fatalf("include-owned legacy migration = %v, %q; want safe refusal", err, refusal)
	}
	if got, err := os.ReadFile(repoConfig); err != nil || string(got) != legacyRoot {
		t.Fatalf("refused migration changed legacy root: %q, %v", got, err)
	}
	if got, err := os.ReadFile(fragment); err != nil || string(got) != fragmentData {
		t.Fatalf("refused migration changed included fragment: %q, %v", got, err)
	}
	if current, err := filepath.Glob(repoConfig + ".pre-v4.*.bak"); err != nil || len(current) != len(backups) {
		t.Fatalf("refused migration changed backup set: %v, %v", current, err)
	}
	run(candidate, "install", "after-upgrade", "--scope", "repo")
	if got, err := os.ReadFile(extraInstalled); err != nil || string(got) != extraSkill {
		t.Fatalf("refused migration left legacy installation unusable: %q, %v", got, err)
	}
}
