package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	absent   bool
	link     string
}

// snapshotNativeHookMigrationEntry records a transaction participant that may
// be a regular file, a link, or already removed.
func snapshotNativeHookMigrationEntry(t *testing.T, path string) nativeHookMigrationFileSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nativeHookMigrationFileSnapshot{absent: true}
	}
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("read link %s: %v", path, err)
		}
		return nativeHookMigrationFileSnapshot{mode: info.Mode(), modified: info.ModTime().UnixNano(), link: link}
	}
	return snapshotNativeHookMigrationFile(t, path)
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
	if !sameNativeHookMigrationFileSnapshot(snapshotNativeHookMigrationEntry(t, path), before) {
		t.Fatalf("%s changed %s", phase, path)
	}
}

// canonicalNativeHookMigrationPath resolves a participant's parent, so a link
// participant names the link itself and a removed one still resolves.
func canonicalNativeHookMigrationPath(t *testing.T, path string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || !filepath.IsAbs(path) {
		t.Fatalf("resolve absolute transaction participant %s: %v", path, err)
	}
	return filepath.Join(parent, filepath.Base(path))
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
	if len(journal.Files) != len(participants) {
		t.Fatalf("dependency transaction participants = %d, want exactly %d", len(journal.Files), len(participants))
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
		t.Fatalf("dependency transaction participant paths = %v, want %v", got, want)
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

type nativeHookMigrationBackup struct {
	file, priorDigest string
	priorSize         int64
	priorAbsent       bool
	data              []byte
}

// nativeHookMigrationBackups reads every retained safety backup under the
// state root, requiring private files and directories and a manifest beside
// each backup.
func nativeHookMigrationBackups(t *testing.T, stateRoot string) []nativeHookMigrationBackup {
	t.Helper()
	var backups []nativeHookMigrationBackup
	root := filepath.Join(stateRoot, "safety-backups")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("safety backup directory %s mode = %v, want 0700", path, info.Mode().Perm())
			}
			return nil
		}
		if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
			t.Fatalf("safety backup file %s mode = %v, want a private 0600 regular file", path, info.Mode())
		}
		if !strings.HasSuffix(path, ".meta.json") {
			return nil
		}
		var manifest struct {
			File        string `json:"file"`
			PriorDigest string `json:"prior_digest"`
			PriorAbsent bool   `json:"prior_absent"`
			PriorSize   int64  `json:"prior_size"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return err
		}
		data, err := os.ReadFile(strings.TrimSuffix(path, ".meta.json") + ".bak")
		if err != nil {
			return err
		}
		backups = append(backups, nativeHookMigrationBackup{file: manifest.File, priorDigest: manifest.PriorDigest, priorSize: manifest.PriorSize, priorAbsent: manifest.PriorAbsent, data: data})
		return nil
	})
	if err != nil {
		t.Fatalf("read safety backups: %v", err)
	}
	return backups
}

// assertNativeHookMigrationBackup requires a retained backup of rel holding
// exactly want, with a manifest digest and size computed here independently.
func assertNativeHookMigrationBackup(t *testing.T, backups []nativeHookMigrationBackup, rel string, want []byte) {
	t.Helper()
	sum := sha256.Sum256(want)
	for _, backup := range backups {
		if backup.file == rel && bytes.Equal(backup.data, want) && !backup.priorAbsent &&
			backup.priorDigest == "sha256:"+hex.EncodeToString(sum[:]) && backup.priorSize == int64(len(want)) {
			return
		}
	}
	t.Fatalf("no faithful safety backup of %s among %d retained backups", rel, len(backups))
}

// assertNoNativeHookTransactionResidue requires that no dependency journal,
// staged publication, or staged journal survives under roots.
func assertNoNativeHookTransactionResidue(t *testing.T, phase string, roots ...string) {
	t.Helper()
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if strings.HasPrefix(entry.Name(), ".dependencytxn-") {
				t.Errorf("%s left transaction residue: %s", phase, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// Every crash window of the journaled Factory Native/Legacy/ownership
// migration, with and without a cache-linked Legacy support script whose
// removal joins the transaction: read-only commands refuse pending work
// without changing a participant, the journal, or the retained backups, while
// a finalized transaction is never reported as pending. Backups taken before
// publication hold the exact prior bytes privately. The next sync, including
// after a second crash during recovery, completes the admitted write set,
// converges forward, keeps handwritten Legacy files and the cache referent,
// and leaves no transaction residue.
// spec: hook-resources/owned-factory-entries-move-to-the-native-file
func TestInterruptedNativeHookMigrationRecoversOnNextMutation(t *testing.T) {
	type window struct {
		name, fault, recoveryFault string
		published                  int
		finalized                  bool
	}
	windows := func(participants int) []window {
		return []window{
			{name: "journal-before-publication", fault: "after-journal:-1", published: 0},
			{name: "first-publication", fault: "after-publish:0", published: 1},
			{name: "second-publication", fault: "after-publish:1", published: 2},
			{name: "all-published-with-journal", fault: fmt.Sprintf("after-publish:%d", participants-1), published: participants},
			{name: "finalized-before-exit", fault: "after-finalize:-1", published: participants, finalized: true},
			{name: "interrupted-recovery", fault: "after-publish:0", published: 1, recoveryFault: "before-publish:1"},
		}
	}
	for _, supportLink := range []bool{false, true} {
		participantCount, variant := 3, "aggregates"
		if supportLink {
			participantCount, variant = 4, "support-link"
		}
		for _, tc := range windows(participantCount) {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
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
				env.WriteFile("/workspace/.factory/hooks/handwritten.sh", "#!/bin/sh\necho handwritten-support\n")
				handwrittenPath := filepath.Join(env.workspace, ".factory", "hooks", "handwritten.sh")
				handwrittenBefore := snapshotNativeHookMigrationFile(t, handwrittenPath)
				env.WriteFile("/workspace/vendor/registry/hooks/hooks.json", nativeV2)

				participants := []string{nativePath, statePath, legacyPath}
				before := map[string]nativeHookMigrationFileSnapshot{
					nativePath: nativeBefore,
					statePath:  stateBefore,
					legacyPath: legacyBefore,
				}
				referentPath := filepath.Join(env.home, ".cache", "agent-marketplace", "legacy-hook-support", "guard.sh")
				supportPath := filepath.Join(env.workspace, ".factory", "hooks", "scripts", "guard.sh")
				var referentBefore nativeHookMigrationFileSnapshot
				if supportLink {
					env.WriteFile("/root/.cache/agent-marketplace/legacy-hook-support/guard.sh", "#!/bin/sh\necho legacy-support\n")
					referentBefore = snapshotNativeHookMigrationFile(t, referentPath)
					if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(referentPath, supportPath); err != nil {
						t.Fatal(err)
					}
					participants = append(participants, supportPath)
					before[supportPath] = snapshotNativeHookMigrationEntry(t, supportPath)
				}
				residueRoots := []string{filepath.Join(env.workspace, ".factory"), stateRoot}
				crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.fault}, "sync")
				if crash.ExitCode != 137 {
					t.Fatalf("injected sync crash at %s exited %d:\n%s", tc.fault, crash.ExitCode, crash.Combined)
				}
				backups := nativeHookMigrationBackups(t, stateRoot)
				assertNativeHookMigrationBackup(t, backups, "hooks.json", nativeBefore.data)
				assertNativeHookMigrationBackup(t, backups, "hooks/hooks.json", legacyBefore.data)

				interrupted := map[string]nativeHookMigrationFileSnapshot{configPath: configBefore, handwrittenPath: handwrittenBefore}
				for _, path := range participants {
					interrupted[path] = snapshotNativeHookMigrationEntry(t, path)
				}
				journals := nativeHookMigrationJournals(t, stateRoot)
				if tc.finalized {
					if len(journals) != 0 {
						t.Fatalf("finalized transaction left dependency journals: %v", journals)
					}
				} else {
					if len(journals) != 1 {
						t.Fatalf("interrupted dependency journals = %v, want exactly one", journals)
					}
					interrupted[journals[0]] = assertNativeHookMigrationJournalParticipants(t, journals[0], participants)
				}
				published := func() int {
					t.Helper()
					changed := 0
					for _, path := range participants {
						if !sameNativeHookMigrationFileSnapshot(before[path], snapshotNativeHookMigrationEntry(t, path)) {
							changed++
						}
					}
					return changed
				}
				if got := published(); got != tc.published {
					t.Fatalf("crash at %s published %d of the %d transaction participants, want %d", tc.fault, got, len(participants), tc.published)
				}

				assertReadOnly := func(phase string, pending bool) {
					t.Helper()
					backupsBefore := nativeHookMigrationBackups(t, stateRoot)
					for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
						result := env.RunAM(args...)
						refused := strings.Contains(result.Combined, "dependency.recovery-required")
						if pending && (result.ExitCode == 0 || !refused) || !pending && refused {
							t.Fatalf("%s: read-only %v exit %d, recovery-required %t, want pending %t:\n%s", phase, args, result.ExitCode, refused, pending, result.Combined)
						}
						for path, want := range interrupted {
							assertNativeHookMigrationFileUnchanged(t, phase+" "+strings.Join(args, " "), path, want)
						}
						if !reflect.DeepEqual(nativeHookMigrationBackups(t, stateRoot), backupsBefore) {
							t.Fatalf("%s: read-only %v changed the retained safety backups", phase, args)
						}
					}
				}
				assertReadOnly("after crash", !tc.finalized)
				if tc.recoveryFault != "" {
					crash = env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": tc.recoveryFault}, "sync")
					if crash.ExitCode != 137 {
						t.Fatalf("injected recovery crash at %s exited %d:\n%s", tc.recoveryFault, crash.ExitCode, crash.Combined)
					}
					if got := nativeHookMigrationJournals(t, stateRoot); len(got) != 1 || got[0] != journals[0] {
						t.Fatalf("interrupted recovery journals = %v, want the original %v", got, journals)
					}
					if got := published(); got != tc.published {
						t.Fatalf("recovery crash before entry 1 published %d participants, want still %d", got, tc.published)
					}
					for _, path := range participants {
						interrupted[path] = snapshotNativeHookMigrationEntry(t, path)
					}
					assertReadOnly("after interrupted recovery", true)
				}

				env.MustRunAM("sync")
				if remaining := nativeHookMigrationJournals(t, stateRoot); len(remaining) != 0 {
					t.Fatalf("recovery left dependency journals: %v", remaining)
				}
				assertNoNativeHookTransactionResidue(t, "recovery", residueRoots...)
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
					nativePath:      nativeAfter,
					statePath:       stateAfter,
					legacyPath:      legacyAfter,
					configPath:      configBefore,
					handwrittenPath: handwrittenBefore,
				}
				if supportLink {
					recovered[supportPath] = nativeHookMigrationFileSnapshot{absent: true}
					recovered[referentPath] = referentBefore
				}
				for path, want := range recovered {
					assertNativeHookMigrationFileUnchanged(t, "post-recovery sync --check", path, want)
				}
				env.MustRunAM("sync")
				for path, want := range recovered {
					assertNativeHookMigrationFileUnchanged(t, "repeat sync", path, want)
				}
				assertNoNativeHookTransactionResidue(t, "repeat sync", residueRoots...)
			})
		}
	}
}

func sameNativeHookMigrationFileSnapshot(a, b nativeHookMigrationFileSnapshot) bool {
	return bytes.Equal(a.data, b.data) && a.mode == b.mode && a.modified == b.modified && a.absent == b.absent && a.link == b.link
}
