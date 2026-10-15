package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The obsolete migration crash fixtures now exercise a supported
// multi-destination journaled purge. Every crash window must leave read-only
// commands refusing pending work without changes, and the next mutating
// command must finish the admitted write set: the repository config and lock
// are absent, while the user config and handwritten output stay.
func TestInterruptedPurgeRecoversOnNextMutation(t *testing.T) {
	for _, tc := range []struct {
		name, fault, recoveryFault string
		remaining                  int
		finalized                  bool
	}{
		{name: "journal-before-publication", fault: "after-journal:-1", remaining: 2},
		{name: "first-publication", fault: "after-publish:0", remaining: 1},
		{name: "all-published-with-journal", fault: "after-publish:1", remaining: 0},
		{name: "finalized-before-exit", fault: "after-finalize:-1", remaining: 0, finalized: true},
		{name: "interrupted-recovery", fault: "after-publish:0", remaining: 1, recoveryFault: "before-publish:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t)
			env.InitGitRepo()
			env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			url := env.CreateBareRepo("interrupted-purge", SkillsOnly(Skill{Name: "review"}))
			env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nsources: {team: {git: '" + url + "', ref: main}}\nresources: {team: {include: [skill/review]}}\n")
			env.MustRunAM("sync")
			env.WriteFile("/workspace/.factory/skills/handwritten/SKILL.md", "HANDWRITTEN\n")
			user := env.ReadFile("/root/.agents/config.yaml")
			paths := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}
			const journalRoot = "/root/.local/state/agent-marketplace/dependencies"
			journalPattern := filepath.Join(env.mustResolvePath(journalRoot), ".dependencytxn-*.json")
			journals := func() []string {
				t.Helper()
				matches, err := filepath.Glob(journalPattern)
				if err != nil {
					t.Fatal(err)
				}
				return matches
			}
			present := func() map[string]string {
				t.Helper()
				files := map[string]string{}
				for _, path := range paths {
					if _, err := os.Lstat(env.mustResolvePath(path)); err == nil {
						fingerprint, err := pathFingerprint(env.mustResolvePath(path))
						if err != nil {
							t.Fatal(err)
						}
						files[path] = fingerprint
					} else if !os.IsNotExist(err) {
						t.Fatal(err)
					}
				}
				return files
			}
			protected := []string{env.mustResolvePath("/root/.agents"), env.mustResolvePath("/workspace/.agents"), env.mustResolvePath("/workspace/.factory"), env.mustResolvePath(journalRoot)}
			snapshot := func() map[string]string {
				t.Helper()
				trees := map[string]string{}
				for _, root := range protected {
					fingerprint, err := pathFingerprint(root)
					if err != nil {
						t.Fatal(err)
					}
					trees[root] = fingerprint
				}
				return trees
			}
			assertReadOnly := func(pending bool) {
				t.Helper()
				before := snapshot()
				for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
					result := env.RunAM(args...)
					if pending {
						if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
							t.Fatalf("read-only %v did not refuse pending work: exit=%d\n%s", args, result.ExitCode, result.Combined)
						}
					} else {
						env.AssertOutputNotContains(result, "dependency.recovery-required")
					}
					for root, want := range before {
						if got, err := pathFingerprint(root); err != nil || got != want {
							t.Fatalf("read-only %v changed bytes, mode, mtime or entries under %s: %v", args, root, err)
						}
					}
				}
			}

			participants := map[string]bool{}
			for _, path := range paths {
				canonical, err := filepath.EvalSymlinks(env.mustResolvePath(path))
				if err != nil {
					t.Fatal(err)
				}
				participants[canonical] = true
			}
			crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.fault}, "deinit", "--scope", "repo", "--purge-config")
			if crash.ExitCode != 137 {
				t.Fatalf("fault %s did not terminate journaled purge (exit %d):\n%s", tc.fault, crash.ExitCode, crash.Combined)
			}
			if got := len(present()); got != tc.remaining {
				t.Fatalf("fault %s left %d destinations, want %d", tc.fault, got, tc.remaining)
			}
			wantJournals := 1
			if tc.finalized {
				wantJournals = 0
			}
			pending := journals()
			if len(pending) != wantJournals {
				t.Fatalf("fault %s left journals %v, want %d", tc.fault, pending, wantJournals)
			}
			if !tc.finalized {
				var journal struct {
					Files []struct {
						Path string `json:"path"`
					} `json:"files"`
				}
				data, err := os.ReadFile(pending[0])
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &journal); err != nil {
					t.Fatal(err)
				}
				got := map[string]bool{}
				for _, file := range journal.Files {
					got[file.Path] = true
				}
				if len(journal.Files) != len(participants) || !reflect.DeepEqual(got, participants) {
					t.Fatalf("journal participants = %v, want exactly the repository config and lock %v", got, participants)
				}
			}
			assertReadOnly(!tc.finalized)
			if tc.recoveryFault != "" {
				crash = env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.recoveryFault}, "sync")
				if crash.ExitCode != 137 || len(journals()) != 1 || len(present()) != tc.remaining {
					t.Fatalf("second crash did not interrupt recovery: exit=%d journals=%v remaining=%v\n%s", crash.ExitCode, journals(), present(), crash.Combined)
				}
				assertReadOnly(true)
			}

			env.MustRunAM("sync")
			for _, path := range paths {
				env.AssertFileAbsent(path)
			}
			if got := journals(); len(got) != 0 {
				t.Fatalf("recovery did not finalize the pending journal: %v", got)
			}
			if got := env.ReadFile("/root/.agents/config.yaml"); got != user {
				t.Fatal("recovery changed the independent user configuration")
			}
			if got := env.ReadFile("/workspace/.factory/skills/handwritten/SKILL.md"); got != "HANDWRITTEN\n" {
				t.Fatalf("purge/recovery changed unowned output: %q", got)
			}
			for _, root := range []string{env.mustResolvePath("/workspace/.agents"), env.mustResolvePath(journalRoot)} {
				if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						if os.IsNotExist(err) {
							return nil
						}
						return err
					}
					if strings.HasPrefix(entry.Name(), ".dependencytxn-") {
						t.Errorf("recovery left transaction residue: %s", path)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			env.MustRunAM("sync", "--check")
		})
	}
}

type nativeHookMigrationFileSnapshot struct {
	data     []byte
	mode     os.FileMode
	modified int64
}

func snapshotNativeHookMigrationFile(t *testing.T, path string) nativeHookMigrationFileSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s mode = %v, want a regular file", path, info.Mode())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return nativeHookMigrationFileSnapshot{data: data, mode: info.Mode(), modified: info.ModTime().UnixNano()}
}

func assertNativeHookMigrationFileUnchanged(t *testing.T, phase, path string, before nativeHookMigrationFileSnapshot) {
	t.Helper()
	if !sameNativeHookMigrationFileSnapshot(snapshotNativeHookMigrationFile(t, path), before) {
		t.Fatalf("%s changed %s", phase, path)
	}
}

func canonicalNativeHookMigrationPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) {
		t.Fatalf("resolve absolute transaction participant %s: %v", path, err)
	}
	return filepath.Clean(resolved)
}

func nativeHookMigrationSourceDigest(t *testing.T, data []byte) string {
	t.Helper()
	var state struct {
		Files map[string]struct {
			Entries []struct {
				Kind         string `json:"kind"`
				Container    string `json:"container"`
				Resource     string `json:"resource"`
				SourceDigest string `json:"source_digest"`
			} `json:"entries"`
			HookSatisfactions []json.RawMessage `json:"hook_satisfactions"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode ownership state: %v", err)
	}
	file, ok := state.Files["hooks.json"]
	if len(state.Files) != 1 || !ok || len(file.Entries) != 1 || len(file.HookSatisfactions) != 0 {
		t.Fatalf("ownership hooks.json entries = %+v, files = %v", file.Entries, state.Files)
	}
	entry := file.Entries[0]
	if entry.Kind != "hook" || entry.Container != "PreToolUse" || entry.Resource != "PreToolUse-command" || entry.SourceDigest == "" {
		t.Fatalf("hooks.json owned Hook entry = %+v", entry)
	}
	return entry.SourceDigest
}

func assertNativeHookMigrationFactoryFile(t *testing.T, path, command string) nativeHookMigrationFileSnapshot {
	t.Helper()
	snapshot := snapshotNativeHookMigrationFile(t, path)
	var document map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(snapshot.data, &document); err != nil {
		t.Fatalf("decode Factory Native hooks: %v", err)
	}
	groups, ok := document["PreToolUse"]
	if len(document) != 1 || !ok || len(groups) != 1 || groups[0].Matcher != "Execute" || len(groups[0].Hooks) != 1 ||
		groups[0].Hooks[0].Type != "command" || groups[0].Hooks[0].Command != command {
		t.Fatalf("Factory Native hooks = %#v, want one Execute command %q", document, command)
	}
	if strings.Contains(string(snapshot.data), "_managed_by") {
		t.Fatal("Factory Native hooks unexpectedly contain an in-file marker")
	}
	return snapshot
}

func assertNativeHookMigrationLegacyManual(t *testing.T, path, manualGroup, rootComment string) nativeHookMigrationFileSnapshot {
	t.Helper()
	snapshot := snapshotNativeHookMigrationFile(t, path)
	text := string(snapshot.data)
	if snapshot.mode.Perm() != 0o640 || strings.Count(text, manualGroup) != 1 || !strings.Contains(text, "  "+rootComment+"\n") ||
		!strings.Contains(text, `"unrelated" : { "keep" : "legacy-setting" }`) || strings.Contains(text, "_managed_by") || strings.Contains(text, "echo legacy-owned") {
		t.Fatalf("Legacy hooks did not preserve only the raw handwritten group:\n%s", text)
	}
	withoutComment := strings.Replace(text, "  "+rootComment+"\n", "", 1)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(withoutComment), &keys); err != nil || len(keys) != 2 {
		t.Fatalf("recovered Legacy root members = %v / %v, want unrelated and PreToolUse", keys, err)
	}
	var document struct {
		Unrelated struct {
			Keep string `json:"keep"`
		} `json:"unrelated"`
		PreToolUse []struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"PreToolUse"`
	}
	if err := json.Unmarshal([]byte(withoutComment), &document); err != nil || len(document.PreToolUse) != 1 || document.Unrelated.Keep != "legacy-setting" ||
		document.PreToolUse[0].Matcher != "Manual" || len(document.PreToolUse[0].Hooks) != 1 ||
		document.PreToolUse[0].Hooks[0].Type != "command" || document.PreToolUse[0].Hooks[0].Command != "echo handwritten" {
		t.Fatalf("recovered Legacy document = %+v / %v", document, err)
	}
	return snapshot
}

func assertNativeHookMigrationJournalParticipants(t *testing.T, journalPath string, participants []string) nativeHookMigrationFileSnapshot {
	t.Helper()
	snapshot := snapshotNativeHookMigrationFile(t, journalPath)
	var journal struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(snapshot.data, &journal); err != nil {
		t.Fatalf("decode dependency transaction journal: %v", err)
	}
	if len(journal.Files) != 3 {
		t.Fatalf("dependency transaction participants = %d, want exactly 3", len(journal.Files))
	}
	want := make(map[string]bool, len(participants))
	for _, path := range participants {
		want[canonicalNativeHookMigrationPath(t, path)] = true
	}
	got := make(map[string]bool, len(journal.Files))
	for _, file := range journal.Files {
		path := canonicalNativeHookMigrationPath(t, file.Path)
		if got[path] {
			t.Fatalf("duplicate dependency transaction participant %s", path)
		}
		got[path] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency transaction participant paths = %v, want native/state/legacy %v", got, want)
	}
	return snapshot
}

func nativeHookMigrationJournals(t *testing.T, stateDir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(stateDir, "dependencies", ".dependencytxn-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
func TestInterruptedNativeHookMigrationRecoversOnNextMutation(t *testing.T) {
	for _, fault := range []string{"after-publish:0", "after-publish:1"} {
		t.Run(fault, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\nresources: {}")
			nativeV1 := `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo native-v1"}]}]}` + "\n"
			nativeV2 := `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo native-v2"}]}]}` + "\n"
			env.WriteFile("/workspace/vendor/registry/hooks/hooks.json", nativeV1)
			env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {hooks: {directory: ./vendor/registry, source_harness: factory}}\nresources: {hooks: {include: [hook/PreToolUse-command]}}")
			configPath := filepath.Join(env.workspace, ".agents", "config.yaml")
			configBefore := snapshotNativeHookMigrationFile(t, configPath)

			env.MustRunAM("sync")
			nativePath := filepath.Join(env.workspace, ".factory", "hooks.json")
			legacyPath := filepath.Join(env.workspace, ".factory", "hooks", "hooks.json")
			nativeBefore := assertNativeHookMigrationFactoryFile(t, nativePath, "echo native-v1")
			if nativeBefore.mode.Perm() != 0o600 {
				t.Fatalf("new Native hooks mode = %v, want private 0600", nativeBefore.mode.Perm())
			}
			if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
				t.Fatalf("initial sync created a Legacy hooks file: %v", err)
			}
			stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
			stateFiles, err := filepath.Glob(filepath.Join(stateRoot, "ownership", "*", "state.json"))
			if err != nil || len(stateFiles) != 1 {
				t.Fatalf("ownership state files = %v / %v, want one fixture record", stateFiles, err)
			}
			statePath := stateFiles[0]
			stateBefore := snapshotNativeHookMigrationFile(t, statePath)
			initialDigest := nativeHookMigrationSourceDigest(t, stateBefore.data)

			const rootComment = "// preserve legacy root comment"
			const manualGroup = `{ "matcher" : "Manual", "hooks" : [{"type":"command","command":"echo handwritten"}] }`
			markedGroup := `{"_managed_by":"agent-marketplace","matcher":"Execute","hooks":[{"type":"command","command":"echo legacy-owned"}]}`
			legacySeed := "{\n  " + rootComment + "\n  \"unrelated\" : { \"keep\" : \"legacy-setting\" },\n  \"PreToolUse\" : [\n    " + markedGroup + ",\n    " + manualGroup + "\n  ]\n}\n"
			env.WriteFile("/workspace/.factory/hooks/hooks.json", legacySeed)
			if err := os.Chmod(legacyPath, 0o640); err != nil {
				t.Fatal(err)
			}
			legacyBefore := snapshotNativeHookMigrationFile(t, legacyPath)
			env.WriteFile("/workspace/vendor/registry/hooks/hooks.json", nativeV2)

			participants := []string{nativePath, statePath, legacyPath}
			before := map[string]nativeHookMigrationFileSnapshot{
				nativePath: nativeBefore,
				statePath:  stateBefore,
				legacyPath: legacyBefore,
			}
			crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": fault}, "sync")
			if crash.ExitCode != 137 {
				t.Fatalf("injected sync crash at %s exited %d:\n%s", fault, crash.ExitCode, crash.Combined)
			}

			journals := nativeHookMigrationJournals(t, stateRoot)
			if len(journals) != 1 {
				t.Fatalf("interrupted dependency journals = %v, want exactly one", journals)
			}
			journalPath := journals[0]
			journalBefore := assertNativeHookMigrationJournalParticipants(t, journalPath, participants)
			interrupted := map[string]nativeHookMigrationFileSnapshot{
				nativePath:  snapshotNativeHookMigrationFile(t, nativePath),
				statePath:   snapshotNativeHookMigrationFile(t, statePath),
				legacyPath:  snapshotNativeHookMigrationFile(t, legacyPath),
				journalPath: journalBefore,
				configPath:  configBefore,
			}
			changed := 0
			for _, path := range participants {
				if !sameNativeHookMigrationFileSnapshot(before[path], interrupted[path]) {
					changed++
				}
			}
			if changed < 1 || changed > 2 {
				t.Fatalf("crash changed %d of the 3 transaction participants, want a proper non-empty subset", changed)
			}

			for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
				result := env.RunAM(args...)
				if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
					t.Fatalf("read-only %v did not refuse the interrupted transaction (exit %d):\n%s", args, result.ExitCode, result.Combined)
				}
				for path, want := range interrupted {
					assertNativeHookMigrationFileUnchanged(t, strings.Join(args, " "), path, want)
				}
			}

			env.MustRunAM("sync")
			if remaining := nativeHookMigrationJournals(t, stateRoot); len(remaining) != 0 {
				t.Fatalf("recovery left dependency journals: %v", remaining)
			}
			nativeAfter := assertNativeHookMigrationFactoryFile(t, nativePath, "echo native-v2")
			if nativeAfter.mode.Perm() != nativeBefore.mode.Perm() || strings.Contains(string(nativeAfter.data), "echo native-v1") || strings.Contains(string(nativeAfter.data), "echo legacy-owned") {
				t.Fatalf("recovered Native hooks mode/content invalid: mode=%v content=%s", nativeAfter.mode.Perm(), nativeAfter.data)
			}
			legacyAfter := assertNativeHookMigrationLegacyManual(t, legacyPath, manualGroup, rootComment)
			stateAfter := snapshotNativeHookMigrationFile(t, statePath)
			nextDigest := nativeHookMigrationSourceDigest(t, stateAfter.data)
			if nextDigest == "" || nextDigest == initialDigest {
				t.Fatal("recovered source digest did not track the updated physical definition")
			}
			check := env.RunAM("sync", "--check")
			if check.ExitCode != 0 {
				t.Fatalf("sync --check after recovery exited %d:\n%s", check.ExitCode, check.Combined)
			}
			recovered := map[string]nativeHookMigrationFileSnapshot{
				nativePath: nativeAfter,
				statePath:  stateAfter,
				legacyPath: legacyAfter,
				configPath: configBefore,
			}
			for path, want := range recovered {
				assertNativeHookMigrationFileUnchanged(t, "post-recovery sync --check", path, want)
			}
			env.MustRunAM("sync")
			for path, want := range recovered {
				assertNativeHookMigrationFileUnchanged(t, "repeat sync", path, want)
			}
			if remaining := nativeHookMigrationJournals(t, stateRoot); len(remaining) != 0 {
				t.Fatalf("repeat sync left dependency journals: %v", remaining)
			}
		})
	}
}

func sameNativeHookMigrationFileSnapshot(a, b nativeHookMigrationFileSnapshot) bool {
	return bytes.Equal(a.data, b.data) && a.mode == b.mode && a.modified == b.modified
}
