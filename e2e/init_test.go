package e2e_test

// The init command tests are standalone because the declarative table runner
// always seeds a ~/.agents/config.yaml (even an empty GlobalCfg is
// auto-injected with apiVersion and harnesses). The init tests require the
// user personal config to be absent, so they cannot run inside the table.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesV4UserPersonalConfig(t *testing.T) {
	env := newEnv(t)

	// Ensure no user personal config exists; newEnv does not write one.
	cfgPath := filepath.Join(env.home, ".agents", "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatalf("precondition failed: user personal config already exists at %s", cfgPath)
	}

	r := env.RunAM("init", "--harnesses", "factory")
	if r.ExitCode != 0 {
		t.Fatalf("init failed (exit %d): %s", r.ExitCode, r.Combined)
	}
	if !strings.Contains(r.Combined, "Initialized user personal config") {
		t.Fatalf("expected init success message, got: %s", r.Combined)
	}

	if !env.CheckFileExists("/root/.agents/config.yaml") {
		t.Fatal("user personal config was not created")
	}
	if !env.CheckFileContains("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v4") {
		t.Fatalf("config does not declare v4 apiVersion:\n%s", env.ReadFile("/root/.agents/config.yaml"))
	}
	if !env.CheckFileContains("/root/.agents/config.yaml", "harnesses:") {
		t.Fatalf("config does not contain harnesses:\n%s", env.ReadFile("/root/.agents/config.yaml"))
	}
	if !env.CheckFileContains("/root/.agents/config.yaml", "- factory") {
		t.Fatalf("config does not contain factory harness:\n%s", env.ReadFile("/root/.agents/config.yaml"))
	}
	if env.CheckFileContains("/root/.agents/config.yaml", "registries:") {
		t.Fatalf("config should not contain registries:\n%s", env.ReadFile("/root/.agents/config.yaml"))
	}
	if env.CheckFileContains("/root/.agents/config.yaml", "logging:") {
		t.Fatalf("config should not contain logging:\n%s", env.ReadFile("/root/.agents/config.yaml"))
	}
}

func TestInitV4ConfigRejectsLegacyUserInstall(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registryURL := env.CreateBareRepo("init-install", SkillsOnly(
		Skill{Name: "review", Description: "Review changes"},
	))

	initResult := env.RunAM("init", "--harnesses", "factory")
	if initResult.ExitCode != 0 {
		t.Fatalf("init failed (exit %d): %s", initResult.ExitCode, initResult.Combined)
	}
	before := env.ReadFile("/root/.agents/config.yaml")

	installResult := env.RunAM("install", "review", "--from", registryURL, "--scope", "user")
	if installResult.ExitCode == 0 || !strings.Contains(installResult.Combined, "v4.writer-opt-in") {
		t.Fatalf("install after v4 init = exit %d, want v4 writer guard: %s", installResult.ExitCode, installResult.Combined)
	}
	if after := env.ReadFile("/root/.agents/config.yaml"); after != before {
		t.Fatalf("rejected install changed initialized user config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	env.AssertFileAbsent("/root/.factory/skills/review")
}

func TestInitRequiresHarnessesInNonInteractiveMode(t *testing.T) {
	env := newEnv(t)

	r := env.RunAM("init")
	if r.ExitCode == 0 {
		t.Fatalf("expected init without --harnesses to fail, got: %s", r.Combined)
	}
	if !strings.Contains(r.Combined, "--harnesses") {
		t.Fatalf("expected actionable --harnesses guidance, got: %s", r.Combined)
	}
}

func TestInitWithExplicitHarnessWhenFactoryDetected(t *testing.T) {
	env := newEnv(t)

	// Simulate an installed Factory harness root. The non-interactive path
	// still requires --harnesses, so this only verifies the command works
	// when the installation root happens to be present.
	factoryRoot := filepath.Join(env.home, ".factory")
	if err := os.MkdirAll(factoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// No user config yet.
	cfgPath := filepath.Join(env.home, ".agents", "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatalf("precondition failed: user personal config already exists")
	}

	r := env.RunAM("init", "--harnesses", "factory")
	if r.ExitCode != 0 {
		t.Fatalf("init failed (exit %d): %s", r.ExitCode, r.Combined)
	}

	if !env.CheckFileExists("/root/.agents/config.yaml") {
		t.Fatal("user personal config was not created")
	}
}
