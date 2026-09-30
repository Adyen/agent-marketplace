package e2e_test

import (
	"strings"
	"testing"
)

const (
	legacyUserWithFactory     = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"
	legacyUserWithoutHarness  = "apiVersion: agent-marketplace/v2\nregistries: []\n"
	legacyRepoWithoutHarness  = "apiVersion: agent-marketplace/v2\nregistries: []\n"
	currentUserWithFactoryV4  = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	migrateUserConfigPath     = "/root/.agents/config.yaml"
	migrateRepoConfigPath     = "/workspace/.agents/config.yaml"
	migrateUnsupportedFlagMsg = "is not supported with --to v2"
)

// migrate must never silently ignore a flag: each flag either takes effect on
// the selected path or the command fails before writing anything.
func TestMigrateFlagsTakeEffectOrFailWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name       string
		user, repo string
		args       []string
		wantFail   string
		wantUser   []string
	}{
		{
			name: "to v2 dry run is refused", repo: legacyRepoWithoutHarness,
			args: []string{"migrate", "--to", "v2", "--dry-run", "--harnesses", "factory"}, wantFail: "--dry-run " + migrateUnsupportedFlagMsg,
		},
		{
			name: "to v2 scope is refused", user: legacyUserWithFactory, repo: legacyRepoWithoutHarness,
			args: []string{"migrate", "--to", "v2", "--scope", "repo"}, wantFail: "--scope " + migrateUnsupportedFlagMsg,
		},
		{
			name: "to v2 conflicting harnesses are refused", user: legacyUserWithFactory,
			args: []string{"migrate", "--to", "v2", "--harnesses", "cursor"}, wantFail: "already declares harnesses",
		},
		{
			name: "v4 migration applies harnesses to a user config without them", user: legacyUserWithoutHarness,
			args: []string{"migrate", "--scope", "user", "--harnesses", "cursor"}, wantUser: []string{"apiVersion: agent-marketplace/v4", "cursor"},
		},
		{
			name: "v4 migration refuses harnesses that conflict with the user config", user: legacyUserWithFactory,
			args: []string{"migrate", "--scope", "user", "--harnesses", "factory,cursor"}, wantFail: "already declares harnesses",
		},
		{
			name: "v4 migration accepts harnesses equal to an already-v4 user config", user: currentUserWithFactoryV4,
			args: []string{"migrate", "--scope", "user", "--harnesses", "factory"}, wantUser: []string{"harnesses: [factory]"},
		},
		{
			name: "v4 migration refuses harnesses an already-v4 user config does not declare", user: currentUserWithFactoryV4,
			args: []string{"migrate", "--scope", "user", "--harnesses", "cursor"}, wantFail: "already declares harnesses",
		},
		{
			name: "v4 migration refuses harnesses when the user config is out of scope", user: legacyUserWithoutHarness, repo: legacyRepoWithoutHarness,
			args: []string{"migrate", "--scope", "repo", "--harnesses", "cursor"}, wantFail: "does not include the user config",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			if tc.user != "" {
				env.WriteFile(migrateUserConfigPath, tc.user)
			}
			if tc.repo != "" {
				env.WriteRepoConfig(tc.repo)
			}
			user := readIfPresent(env, migrateUserConfigPath)
			repo := readIfPresent(env, migrateRepoConfigPath)

			result := env.RunAM(tc.args...)
			if tc.wantFail != "" {
				if result.ExitCode == 0 || !strings.Contains(result.Combined, tc.wantFail) {
					t.Fatalf("migrate %v did not fail with %q (exit %d):\n%s", tc.args, tc.wantFail, result.ExitCode, result.Combined)
				}
				if got := readIfPresent(env, migrateUserConfigPath); got != user {
					t.Fatalf("refused migrate changed the user config:\n%s", got)
				}
				if got := readIfPresent(env, migrateRepoConfigPath); got != repo {
					t.Fatalf("refused migrate changed the repository config:\n%s", got)
				}
				return
			}
			if result.ExitCode != 0 {
				t.Fatalf("migrate %v failed (exit %d):\n%s", tc.args, result.ExitCode, result.Combined)
			}
			got := env.ReadFile(migrateUserConfigPath)
			for _, want := range tc.wantUser {
				if !strings.Contains(got, want) {
					t.Fatalf("user config after migrate %v lacks %q:\n%s", tc.args, want, got)
				}
			}
		})
	}
}

// A dry run with harnesses previews the conversion and writes nothing.
func TestMigrateDryRunWithHarnessesWritesNothing(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	// Written verbatim: WriteGlobalConfig would add default harnesses.
	env.WriteFile(migrateUserConfigPath, legacyUserWithoutHarness)
	result := env.RunAM("migrate", "--dry-run", "--scope", "user", "--harnesses", "cursor")
	if result.ExitCode != 0 || !strings.Contains(result.Combined, "Would migrate user personal config") {
		t.Fatalf("migrate --dry-run --harnesses failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if got := env.ReadFile(migrateUserConfigPath); got != legacyUserWithoutHarness {
		t.Fatalf("dry run changed the user config:\n%s", got)
	}
}

func readIfPresent(env *Env, path string) string {
	if !env.CheckFileExists(path) {
		return ""
	}
	return env.ReadFile(path)
}
