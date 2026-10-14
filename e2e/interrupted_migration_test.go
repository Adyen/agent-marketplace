package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The process dies after publishing the first file of `migrate --scope all`.
// Read-only commands must refuse the half-published state instead of using
// it, and the next mutating command must finish the migration coherently.
func TestInterruptedMigrationRecoversOnNextMutation(t *testing.T) {
	env, roots := migrationRecoveryFixture(t)
	migratedRoots := func() int {
		t.Helper()
		count := 0
		for _, path := range roots {
			if strings.Contains(env.ReadFile(path), "apiVersion: agent-marketplace/v4") {
				count++
			}
		}
		return count
	}
	snapshot := func() map[string]string {
		t.Helper()
		files := make(map[string]string, len(roots))
		for _, path := range roots {
			files[path] = env.ReadFile(path)
		}
		return files
	}

	crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0"}, "migrate", "--scope", "all")
	if crash.ExitCode != 137 {
		t.Fatalf("injected crash did not kill migrate (exit %d):\n%s", crash.ExitCode, crash.Combined)
	}
	if got := migratedRoots(); got != 1 {
		t.Fatalf("crash left %d of %d roots migrated; want exactly the first published file", got, len(roots))
	}
	interrupted := snapshot()

	check := env.RunAM("sync", "--check")
	if check.ExitCode == 0 {
		t.Fatalf("read-only sync --check accepted a half-published migration:\n%s", check.Combined)
	}
	for path, want := range interrupted {
		if got := env.ReadFile(path); got != want {
			t.Fatalf("read-only check changed %s during an unrecovered transaction", path)
		}
	}

	env.MustRunAM("sync")
	if got := migratedRoots(); got != len(roots) {
		t.Fatalf("recovery left %d of %d roots migrated", got, len(roots))
	}
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "review body")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "second body")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)

	recovered := snapshot()
	env.MustRunAM("migrate", "--scope", "all")
	for path, want := range recovered {
		if got := env.ReadFile(path); got != want {
			t.Fatalf("migrate after recovery rewrote %s:\nbefore:\n%s\nafter:\n%s", path, want, got)
		}
	}
}

func migrationRecoveryFixture(t *testing.T) (*Env, []string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
	for _, name := range []string{"review", "second"} {
		env.WriteFile("/workspace/vendor/registry/skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+name+" body\n")
	}
	env.WriteRepoConfig("registries:\n  - path: ./vendor/registry\n    skills:\n      include: [review]\n")
	env.WriteLocalConfig("registries:\n  - path: ./vendor/registry\n    skills:\n      include: [second]\n")
	env.MustRunAM("sync")
	return env, []string{"/root/.agents/config.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/config.local.yaml"}
}

func TestInterruptedMigrationCrashWindows(t *testing.T) {
	for _, tc := range []struct {
		name, fault, recoveryFault string
		published                  int
		finalized                  bool
	}{
		{name: "journal-before-publication", fault: "after-journal:-1", published: 0},
		{name: "first-backup-publication", fault: "after-publish:1", published: 1},
		{name: "second-config-publication", fault: "after-publish:2", published: 2},
		{name: "all-configs-before-last-backup", fault: "after-publish:4", published: 3},
		{name: "all-published-with-journal", fault: "after-publish:5", published: 3},
		{name: "finalized-before-exit", fault: "after-finalize:-1", published: 3, finalized: true},
		{name: "interrupted-recovery", fault: "after-publish:0", published: 1, recoveryFault: "after-publish:2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, roots := migrationRecoveryFixture(t)
			original := map[string]string{}
			for _, path := range roots {
				original[path] = env.ReadFile(path)
			}
			state := filepath.Join(env.home, ".local", "state", "agent-marketplace", "dependencies")
			protected := []string{filepath.Join(env.home, ".agents"), filepath.Join(env.workspace, ".agents"), filepath.Join(env.workspace, ".factory", "skills"), state}
			registry := filepath.Join(env.workspace, "vendor", "registry")
			registryBefore, err := pathFingerprint(registry)
			if err != nil {
				t.Fatal(err)
			}
			migratedRoots := func() int {
				t.Helper()
				count := 0
				for _, path := range roots {
					if strings.Contains(env.ReadFile(path), "apiVersion: agent-marketplace/v4") {
						count++
					}
				}
				return count
			}
			journals := func() []string {
				t.Helper()
				paths, err := filepath.Glob(filepath.Join(state, ".dependencytxn-*.json"))
				if err != nil {
					t.Fatal(err)
				}
				return paths
			}
			snapshot := func() map[string]string {
				t.Helper()
				files := map[string]string{}
				for _, path := range protected {
					fingerprint, err := pathFingerprint(path)
					if err != nil {
						t.Fatal(err)
					}
					files[path] = fingerprint
				}
				return files
			}
			assertUnchanged := func(before map[string]string) {
				t.Helper()
				for path, want := range before {
					if got, err := pathFingerprint(path); err != nil || got != want {
						t.Fatalf("bytes, mode, mtime or tree entries changed at %s: %v", path, err)
					}
				}
			}
			assertReadOnly := func(pending, inSync bool) {
				t.Helper()
				before := snapshot()
				for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
					result := env.RunAM(args...)
					if pending {
						if result.ExitCode != 1 || !strings.Contains(result.Combined, "dependency.recovery-required") {
							t.Fatalf("read-only %v did not refuse pending recovery: exit=%d\n%s", args, result.ExitCode, result.Combined)
						}
					} else if inSync {
						env.AssertExitCode(result, 0)
					} else {
						env.AssertOutputNotContains(result, "dependency.recovery-required")
						if result.ExitCode != 0 {
							env.AssertExitCode(result, 1)
							if args[0] == "status" {
								env.AssertOutputContains(result, "DRIFT")
								env.AssertOutputContains(result, "review")
								env.AssertOutputContains(result, "second")
							} else {
								env.AssertOutputContains(result, "out of sync")
							}
						}
					}
					assertUnchanged(before)
				}
			}
			crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.fault}, "migrate", "--scope", "all")
			if crash.ExitCode != 137 || migratedRoots() != tc.published {
				t.Fatalf("fault %s: exit=%d migrated=%d, want exit 137 and %d roots\n%s", tc.fault, crash.ExitCode, migratedRoots(), tc.published, crash.Combined)
			}
			pending := journals()
			if tc.finalized {
				if len(pending) != 0 {
					t.Fatalf("finalization left a journal: %v", pending)
				}
			} else {
				if len(pending) != 1 {
					t.Fatalf("expected exactly one recovery journal: %v", pending)
				}
				var journal struct {
					Files []struct {
						Path string `json:"path"`
					} `json:"files"`
				}
				if err := json.Unmarshal([]byte(env.ReadFile(pending[0])), &journal); err != nil || len(journal.Files) != 2*len(roots) {
					t.Fatalf("journal participants=%d, want config and backup for each of %d roots: %v", len(journal.Files), len(roots), err)
				}
				configs, backups := map[string]bool{}, map[string]bool{}
				for _, path := range roots {
					canonical, err := filepath.EvalSymlinks(env.mustResolvePath(path))
					if err != nil {
						t.Fatal(err)
					}
					configs[canonical], backups[canonical] = true, true
				}
				for _, file := range journal.Files {
					if configs[file.Path] {
						delete(configs, file.Path)
						continue
					}
					found := false
					for path := range backups {
						if strings.HasPrefix(file.Path, path+".pre-v4.") && strings.HasSuffix(file.Path, ".bak") {
							delete(backups, path)
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("unexpected or duplicate journal participant %s", file.Path)
					}
				}
				if len(configs) != 0 || len(backups) != 0 {
					t.Fatalf("journal missing config or backup participants: configs=%v backups=%v", configs, backups)
				}
			}
			assertReadOnly(!tc.finalized, false)
			if tc.recoveryFault != "" {
				crash = env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.recoveryFault}, "sync")
				if crash.ExitCode != 137 || migratedRoots() != 2 || len(journals()) != 1 {
					t.Fatalf("second crash did not interrupt recovery: exit=%d migrated=%d journals=%v\n%s", crash.ExitCode, migratedRoots(), journals(), crash.Combined)
				}
				assertReadOnly(true, false)
			}
			env.MustRunAM("sync")
			if migratedRoots() != len(roots) || len(journals()) != 0 {
				t.Fatalf("recovery did not converge: migrated=%d journals=%v", migratedRoots(), journals())
			}
			for _, name := range []string{"review", "second"} {
				env.AssertFileContains("/workspace/.factory/skills/"+name+"/SKILL.md", name+" body")
			}
			if got, err := pathFingerprint(registry); err != nil || got != registryBefore {
				t.Fatalf("migration or recovery changed source registry: %v", err)
			}
			for _, path := range roots {
				backups, err := filepath.Glob(env.mustResolvePath(path) + ".pre-v4.*.bak")
				if err != nil || len(backups) != 1 {
					t.Fatalf("backup for %s: %v / %v", path, backups, err)
				}
				info, err := os.Lstat(backups[0])
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || env.ReadFile(backups[0]) != original[path] {
					t.Fatalf("backup did not preserve original private config at %s: %v", path, err)
				}
			}
			for _, root := range protected {
				if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if strings.HasPrefix(entry.Name(), ".dependencytxn-") && (strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".json")) {
						t.Errorf("recovery left transaction residue: %s", path)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			assertReadOnly(false, true)
			recovered := snapshot()
			env.MustRunAM("sync")
			env.MustRunAM("migrate", "--scope", "all")
			assertUnchanged(recovered)
		})
	}
}
