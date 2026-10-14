package dependencytxn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadSymlinkSnapshotUsesLinkText(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "cache", "script.sh")
	link := filepath.Join(root, "links", "script.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	gotTarget, generation, err := ReadSymlinkSnapshot(link)
	if err != nil {
		t.Fatalf("ReadSymlinkSnapshot: %v", err)
	}
	if gotTarget != target || generation != (Generation{Exists: true, Digest: Digest([]byte(target))}) {
		t.Fatalf("snapshot = %q/%+v, want link text and its digest", gotTarget, generation)
	}
	danglingTarget := filepath.Join(root, "missing")
	dangling := filepath.Join(root, "links", "dangling")
	if err := os.Symlink(danglingTarget, dangling); err != nil {
		t.Fatal(err)
	}
	gotTarget, generation, err = ReadSymlinkSnapshot(dangling)
	if err != nil || gotTarget != danglingTarget || generation != (Generation{Exists: true, Digest: Digest([]byte(danglingTarget))}) {
		t.Fatalf("dangling snapshot = %q/%+v/%v", gotTarget, generation, err)
	}
}

func TestLinkDeletionCommitAndRecovery(t *testing.T) {
	for _, stage := range []FaultStage{FaultAfterJournal, FaultAfterPublish} {
		t.Run(string(stage), func(t *testing.T) {
			root, state := testDirs(t)
			cache := filepath.Join(root, "cache")
			if err := os.MkdirAll(cache, 0o755); err != nil {
				t.Fatal(err)
			}
			referent := filepath.Join(cache, "payload")
			mustWrite(t, referent, []byte("referent bytes"), 0o600)
			path := filepath.Join(root, "hook-support")
			target, err := filepath.Rel(root, referent)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			linkTarget, expected, err := ReadSymlinkSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			desired := DesiredFile{Path: path, Expected: expected, ExpectedLinkTarget: linkTarget, Delete: true}
			interrupted := errors.New("interrupted link publication")
			_, err = Commit(context.Background(), state, []DesiredFile{desired}, WithFaultHook(func(point FaultPoint) error {
				if point.Stage == stage {
					return interrupted
				}
				return nil
			}))
			if !errors.Is(err, interrupted) {
				t.Fatalf("Commit error = %v, want injected interruption", err)
			}
			if len(journals(t, state)) != 1 {
				t.Fatal("missing link deletion recovery journal")
			}
			if stage == FaultAfterJournal {
				if _, err := os.Lstat(path); err != nil {
					t.Fatalf("link changed before publish: %v", err)
				}
			} else if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("link still exists after publish: %v", err)
			}
			if err := Recover(context.Background(), state); err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("link remains after recovery: %v", err)
			}
			assertFile(t, referent, "referent bytes", 0o600)
			if len(journals(t, state)) != 0 {
				t.Fatal("recovery journal remains")
			}
		})
	}
}

func TestLinkDeletionRejectsTargetAndKindSwaps(t *testing.T) {
	t.Run("target swapped before commit", func(t *testing.T) {
		root, state := testDirs(t)
		path := filepath.Join(root, "link")
		original := filepath.Join(root, "original")
		replacement := filepath.Join(root, "replacement")
		if err := os.Symlink(original, path); err != nil {
			t.Fatal(err)
		}
		linkTarget, expected, err := ReadSymlinkSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(replacement, path); err != nil {
			t.Fatal(err)
		}
		_, err = Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: expected, ExpectedLinkTarget: linkTarget, Delete: true}})
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("Commit error = %v, want generation conflict", err)
		}
		if got, err := os.Readlink(path); err != nil || got != replacement {
			t.Fatalf("replacement link = %q, %v", got, err)
		}
	})

	t.Run("regular file has same bytes as link text", func(t *testing.T) {
		root, state := testDirs(t)
		path := filepath.Join(root, "file")
		target := filepath.Join(root, "target")
		mustWrite(t, path, []byte(target), 0o600)
		expected := Generation{Exists: true, Digest: Digest([]byte(target))}
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: expected, ExpectedLinkTarget: target, Delete: true}})
		if err == nil {
			t.Fatal("regular file matched a link deletion participant")
		}
		assertFile(t, path, target, 0o600)
	})

	t.Run("ancestor symlink is refused", func(t *testing.T) {
		root := t.TempDir()
		real := filepath.Join(root, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		leaf := filepath.Join(real, "leaf")
		if err := os.Symlink(filepath.Join(root, "target"), leaf); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(real, alias); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadSymlinkSnapshot(filepath.Join(alias, "leaf")); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("ReadSymlinkSnapshot through ancestor link = %v, want ErrUnsafePath", err)
		}
	})
}

func TestLinkDeletionRevalidatesJournalTargetBeforePublish(t *testing.T) {
	for _, replacementKind := range []string{"symlink", "regular"} {
		t.Run(replacementKind, func(t *testing.T) {
			root, state := testDirs(t)
			path := filepath.Join(root, "owned")
			original := filepath.Join(root, "cache", "original")
			replacement := filepath.Join(root, "cache", "replacement")
			if err := os.MkdirAll(filepath.Dir(original), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(original, path); err != nil {
				t.Fatal(err)
			}
			target, expected, err := ReadSymlinkSnapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			desired := DesiredFile{Path: path, Expected: expected, ExpectedLinkTarget: target, Delete: true}
			_, err = Commit(context.Background(), state, []DesiredFile{desired}, WithFaultHook(func(point FaultPoint) error {
				if point.Stage != FaultAfterJournal {
					return nil
				}
				if err := os.Remove(path); err != nil {
					return err
				}
				if replacementKind == "symlink" {
					return os.Symlink(replacement, path)
				}
				return os.WriteFile(path, []byte(target), 0o600)
			}))
			if !errors.Is(err, ErrRecoveryBlocked) {
				t.Fatalf("Commit error = %v, want blocked third-party replacement", err)
			}
			if len(journals(t, state)) != 1 {
				t.Fatal("blocked publication lost recovery evidence")
			}
			if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
				t.Fatalf("Recover error = %v, want blocked replacement", err)
			}
			if replacementKind == "symlink" {
				if got, err := os.Readlink(path); err != nil || got != replacement {
					t.Fatalf("replacement link = %q, %v", got, err)
				}
			} else {
				assertFile(t, path, target, 0o600)
			}
		})
	}
}

func TestLinkDeletionJournalValidation(t *testing.T) {
	root, _ := testDirs(t)
	path := filepath.Join(root, "link")
	target := filepath.Join(root, "target")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	valid := journalFile{
		Path:          path,
		Old:           Generation{Exists: true, Digest: Digest([]byte(target))},
		OldLinkTarget: target,
		Delete:        true,
	}
	validate := func(file journalFile) error {
		return validateJournal(&journal{Version: journalVersion, ID: "link-delete", Files: []journalFile{file}})
	}
	if err := validate(valid); err != nil {
		t.Fatalf("valid link deletion journal: %v", err)
	}
	invalid := []journalFile{
		{Path: path, Old: valid.Old, OldLinkTarget: target},
		{Path: path, Old: Generation{Exists: true, Digest: Digest([]byte("other"))}, OldLinkTarget: target, Delete: true},
		{Path: path, OldLinkTarget: target, Delete: true},
		{Path: path, Old: valid.Old, OldLinkTarget: target, Delete: true, Temp: filepath.Join(root, "temp")},
		{Path: path, Old: valid.Old, OldMode: 0o777, OldLinkTarget: target, Delete: true},
	}
	for i, file := range invalid {
		if err := validate(file); !errors.Is(err, ErrRecoveryBlocked) {
			t.Errorf("invalid journal case %d error = %v, want ErrRecoveryBlocked", i, err)
		}
	}
}

func TestOrdinarySnapshotAndDeletionStillRefuseSymlinks(t *testing.T) {
	root, state := testDirs(t)
	target := filepath.Join(root, "target")
	path := filepath.Join(root, "link")
	mustWrite(t, target, []byte("referent"), 0o600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadSnapshot(path); err == nil {
		t.Fatal("ReadSnapshot accepted a symlink")
	}
	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: Generation{Exists: true, Digest: Digest([]byte(target))}, Delete: true}})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("ordinary deletion error = %v, want ErrUnsafePath", err)
	}
	if got, err := os.Readlink(path); err != nil || got != target {
		t.Fatalf("ordinary delete changed link = %q, %v", got, err)
	}
	assertFile(t, target, "referent", 0o600)
}

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
// spec: hook-resources/repository-support-conversion-check-writes-nothing
func TestLinkReplacementCommitAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{name: "replacement bytes", data: []byte("replacement support\n"), mode: 0o640},
		{name: "empty replacement", data: []byte{}, mode: 0o600},
	} {
		t.Run("commit/"+tc.name, func(t *testing.T) {
			f := newLinkReplacementFixture(t)
			result, err := Commit(context.Background(), f.state, []DesiredFile{f.desired(t, tc.data, tc.mode)})
			if err != nil {
				t.Fatalf("Commit valid link replacement: %v", err)
			}
			if len(result.Changed) != 1 || result.Changed[0] != f.path {
				t.Fatalf("Changed = %v, want %q", result.Changed, f.path)
			}
			assertLinkReplacementFile(t, f.path, tc.data, tc.mode)
			f.assertReferentUnchanged(t)
			if len(journals(t, f.state)) != 0 {
				t.Fatal("completed replacement retained its journal")
			}
			assertNoTransactionResidue(t, f.root, f.state)
		})
	}

	for _, stage := range []FaultStage{FaultAfterStage, FaultAfterJournal, FaultAfterPublish} {
		t.Run(string(stage), func(t *testing.T) {
			f := newLinkReplacementFixture(t)
			data := []byte("replacement support\n")
			mode := os.FileMode(0o640)
			injected := errors.New("interrupted link replacement")
			_, err := Commit(context.Background(), f.state, []DesiredFile{f.desired(t, data, mode)}, WithFaultHook(func(point FaultPoint) error {
				if point.Stage == stage {
					return injected
				}
				return nil
			}))
			if !errors.Is(err, injected) {
				t.Errorf("Commit error = %v, want injected interruption at %s", err, stage)
			}
			if stage == FaultAfterStage {
				f.assertOriginalLink(t)
				if len(journals(t, f.state)) != 0 {
					t.Error("pre-journal failure retained a recovery journal")
				}
				assertNoTransactionResidue(t, f.root, f.state)
				return
			}
			if len(journals(t, f.state)) != 1 {
				t.Errorf("journal count after %s = %d, want 1", stage, len(journals(t, f.state)))
				return
			}
			if stage == FaultAfterJournal {
				f.assertOriginalLink(t)
			} else {
				assertLinkReplacementFile(t, f.path, data, mode)
			}
			assertLinkReplacementCheckPendingReadOnly(t, f, stage == FaultAfterJournal)
			if err := Recover(context.Background(), f.state); err != nil {
				t.Fatalf("Recover after %s: %v", stage, err)
			}
			assertLinkReplacementFile(t, f.path, data, mode)
			f.assertReferentUnchanged(t)
			if err := Recover(context.Background(), f.state); err != nil {
				t.Fatalf("repeat Recover after %s: %v", stage, err)
			}
			if len(journals(t, f.state)) != 0 {
				t.Fatal("recovery journal remains after idempotent recovery")
			}
			assertNoTransactionResidue(t, f.root, f.state)
		})
	}
}

func TestLinkReplacementStatStabilityChecksMutationTimes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "staged")
	mustWrite(t, path, []byte("same-size"), 0o640)
	stat := func() unix.Stat_t {
		t.Helper()
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		var info unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &info); err != nil {
			t.Fatal(err)
		}
		return info
	}
	before := stat()
	if !sameRegularFileState(before, stat()) {
		t.Fatal("unchanged staged file failed the state check")
	}
	if err := os.WriteFile(path, []byte("different"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	after := stat()
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mode != after.Mode || before.Nlink != after.Nlink {
		t.Fatalf("test mutation changed an identity field: before=%+v after=%+v", before, after)
	}
	if before.Mtim == after.Mtim && before.Ctim == after.Ctim {
		t.Fatalf("test mutation did not change timestamps: before=%+v after=%+v", before, after)
	}
	if sameRegularFileState(before, after) {
		t.Fatal("same-inode same-size mode-preserving staged mutation passed the state check")
	}
}

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
func TestLinkReplacementPreservesLinkWhenStageChangesBeforePublish(t *testing.T) {
	f := newLinkReplacementFixture(t)
	desiredBytes := []byte("replacement support\n")
	mode := os.FileMode(0o640)
	var faultReached bool
	var stagePath string
	var changedStage []byte
	_, err := Commit(context.Background(), f.state, []DesiredFile{f.desired(t, desiredBytes, mode)}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultBeforePublish {
			return nil
		}
		faultReached = true
		stages, err := filepath.Glob(filepath.Join(filepath.Dir(point.Path), ".dependencytxn-*.tmp"))
		if err != nil || len(stages) != 1 {
			return fmt.Errorf("staged replacement paths = %v / %v", stages, err)
		}
		stagePath = stages[0]
		stageInfo, err := os.Lstat(stagePath)
		if err != nil {
			return err
		}
		staged, err := os.ReadFile(stagePath)
		if err != nil {
			return err
		}
		changedStage = bytes.Clone(staged)
		changedStage[0] ^= 1
		if err := os.WriteFile(stagePath, changedStage, stageInfo.Mode().Perm()); err != nil {
			return err
		}
		return os.Chmod(stagePath, stageInfo.Mode().Perm())
	}))
	if !faultReached {
		t.Fatal("stage mutation fault boundary was not reached")
	}
	if !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Commit error = %v, want ErrRecoveryBlocked", err)
	}
	f.assertOriginalLink(t)
	f.assertReferentUnchanged(t)
	stagedAfter, err := os.ReadFile(stagePath)
	if err != nil || !bytes.Equal(stagedAfter, changedStage) {
		t.Fatalf("staged replacement changed after refusal: %q / %v", stagedAfter, err)
	}
	if len(journals(t, f.state)) != 1 {
		t.Fatal("staged mutation refusal removed the recovery journal")
	}
	if err := Recover(context.Background(), f.state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Recover error = %v, want ErrRecoveryBlocked", err)
	}
	f.assertOriginalLink(t)
	f.assertReferentUnchanged(t)
	if len(journals(t, f.state)) != 1 {
		t.Fatal("blocked recovery removed its journal")
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestLinkReplacementRejectsForeignState(t *testing.T) {
	replacement := []byte("replacement support\n")
	replacementMode := os.FileMode(0o640)
	cases := []struct {
		name    string
		replace func(*testing.T, *linkReplacementFixture) string
	}{
		{
			name: "symlink target swap",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(f.root, "cache", "other-payload")
				if err := os.Symlink(target, f.path); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
		{
			name: "regular file matching link text",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, f.path, []byte(f.linkTarget), 0o600)
				return ""
			},
		},
		{
			name: "regular file matching replacement",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, f.path, replacement, replacementMode)
				return ""
			},
		},
		{
			name: "regular file with different mode",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, f.path, replacement, 0o600)
				return ""
			},
		},
		{
			name: "missing link",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
		{
			name: "hard link",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				external := filepath.Join(f.root, "third-party")
				mustWrite(t, external, []byte("third-party bytes\n"), 0o600)
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(external, f.path); err != nil {
					t.Fatal(err)
				}
				return external
			},
		},
		{
			name: "FIFO",
			replace: func(t *testing.T, f *linkReplacementFixture) string {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(f.path, 0o600); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
	}

	for _, tc := range cases {
		t.Run("before commit/"+tc.name, func(t *testing.T) {
			f := newLinkReplacementFixture(t)
			desired := f.desired(t, replacement, replacementMode)
			external := tc.replace(t, f)
			before := snapshotLinkReplacementPath(t, f.path)
			var externalBefore linkReplacementPathSnapshot
			if external != "" {
				externalBefore = snapshotLinkReplacementPath(t, external)
			}
			_, err := Commit(context.Background(), f.state, []DesiredFile{desired})
			if !errors.Is(err, ErrGenerationConflict) {
				t.Errorf("Commit error = %v, want ErrGenerationConflict", err)
			}
			assertLinkReplacementPathUnchanged(t, f.path, before)
			if external != "" {
				assertLinkReplacementPathUnchanged(t, external, externalBefore)
			}
			f.assertReferentUnchanged(t)
			if len(journals(t, f.state)) != 0 {
				t.Error("foreign destination swap created a journal")
			}
			assertNoTransactionResidue(t, f.root, f.state)
		})
	}

	for _, tc := range cases {
		t.Run("after journal/"+tc.name, func(t *testing.T) {
			f := newLinkReplacementFixture(t)
			desired := f.desired(t, replacement, replacementMode)
			injected := errors.New("interrupt before link replacement publish")
			_, err := Commit(context.Background(), f.state, []DesiredFile{desired}, WithFaultHook(func(point FaultPoint) error {
				if point.Stage == FaultAfterJournal {
					return injected
				}
				return nil
			}))
			if !errors.Is(err, injected) {
				t.Errorf("Commit error = %v, want injected post-journal interruption", err)
				if len(journals(t, f.state)) == 0 {
					f.assertOriginalLink(t)
					assertNoTransactionResidue(t, f.root, f.state)
					return
				}
			}
			paths := journals(t, f.state)
			if len(paths) != 1 {
				t.Errorf("journal count after interruption = %d, want 1", len(paths))
				return
			}
			external := tc.replace(t, f)
			before := snapshotLinkReplacementPath(t, f.path)
			var externalBefore linkReplacementPathSnapshot
			if external != "" {
				externalBefore = snapshotLinkReplacementPath(t, external)
			}
			journalBefore, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			pending, err := readJournal(paths[0])
			if err != nil || len(pending.Files) != 1 {
				t.Fatalf("read pending link replacement journal: files=%d err=%v", len(pending.Files), err)
			}
			stagePath := pending.Files[0].Temp
			stageBefore := snapshotLinkReplacementPath(t, stagePath)
			if !stageBefore.exists {
				t.Fatalf("staged replacement %s is missing", stagePath)
			}
			err = Recover(context.Background(), f.state)
			if !errors.Is(err, ErrRecoveryBlocked) {
				t.Errorf("Recover error = %v, want ErrRecoveryBlocked", err)
			}
			assertLinkReplacementPathUnchanged(t, f.path, before)
			assertLinkReplacementPathUnchanged(t, stagePath, stageBefore)
			if external != "" {
				assertLinkReplacementPathUnchanged(t, external, externalBefore)
			}
			if after, readErr := os.ReadFile(paths[0]); readErr != nil || !bytes.Equal(after, journalBefore) {
				t.Errorf("recovery journal changed while blocking foreign state: %v", readErr)
			}
			if len(journals(t, f.state)) != 1 {
				t.Error("blocked recovery removed its journal")
			}
			f.assertReferentUnchanged(t)
		})
	}

	t.Run("ancestor symlink", func(t *testing.T) {
		f := newLinkReplacementFixture(t)
		alias := filepath.Join(f.root, "alias")
		if err := os.Symlink(f.root, alias); err != nil {
			t.Fatal(err)
		}
		aliasPath := filepath.Join(alias, filepath.Base(f.path))
		if _, _, err := ReadSymlinkSnapshot(aliasPath); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("ReadSymlinkSnapshot through parent symlink = %v, want ErrUnsafePath", err)
		}
		desired := f.desired(t, replacement, replacementMode)
		desired.Path = aliasPath
		_, err := Commit(context.Background(), f.state, []DesiredFile{desired})
		if !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Commit through parent symlink error = %v, want ErrUnsafePath", err)
		}
		f.assertOriginalLink(t)
		f.assertReferentUnchanged(t)
		if len(journals(t, f.state)) != 0 {
			t.Error("parent symlink refusal created a journal")
		}
	})
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestLinkReplacementRejectsInvalidJournal(t *testing.T) {
	f := newLinkReplacementFixture(t)
	newBytes := []byte("replacement support\n")
	valid := journalFile{
		Path:          f.path,
		Temp:          filepath.Join(f.root, ".dependencytxn-staged.tmp"),
		Old:           Generation{Exists: true, Digest: Digest([]byte(f.linkTarget))},
		OldLinkTarget: f.linkTarget,
		NewDigest:     Digest(newBytes),
		NewMode:       0o640,
	}
	validate := func(file journalFile) error {
		return validateJournal(&journal{Version: journalVersion, ID: "link-replacement", Files: []journalFile{file}})
	}
	if err := validate(valid); err != nil {
		t.Errorf("valid non-delete link-replacement journal: %v", err)
	}
	invalid := []struct {
		name   string
		mutate func(*journalFile)
	}{
		{name: "missing stage", mutate: func(file *journalFile) { file.Temp = "" }},
		{name: "missing new digest", mutate: func(file *journalFile) { file.NewDigest = "" }},
		{name: "zero new mode", mutate: func(file *journalFile) { file.NewMode = 0 }},
		{name: "invalid mode bits", mutate: func(file *journalFile) { file.NewMode = 0o640 | os.ModeSetgid }},
		{name: "old link generation mismatch", mutate: func(file *journalFile) { file.Old.Digest = Digest([]byte("different link text")) }},
		{name: "missing old generation", mutate: func(file *journalFile) { file.Old = Generation{} }},
		{name: "old link mode present", mutate: func(file *journalFile) { file.OldMode = 0o600 }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			file := valid
			tc.mutate(&file)
			if err := validate(file); !errors.Is(err, ErrRecoveryBlocked) {
				t.Errorf("invalid journal error = %v, want ErrRecoveryBlocked", err)
			}
		})
	}

	t.Run("missing staged file", func(t *testing.T) {
		f := newLinkReplacementFixture(t)
		file := valid
		file.Path = f.path
		file.Old = Generation{Exists: true, Digest: Digest([]byte(f.linkTarget))}
		file.OldLinkTarget = f.linkTarget
		file.Temp = filepath.Join(f.root, ".dependencytxn-missing-stage.tmp")
		if err := validate(file); err != nil {
			t.Errorf("link replacement journal with a contained missing stage is structurally valid: %v", err)
		}
		if err := os.MkdirAll(f.state, 0o700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(&journal{Version: journalVersion, ID: "missing-stage", Files: []journalFile{file}})
		if err != nil {
			t.Fatal(err)
		}
		journalPath := filepath.Join(f.state, ".dependencytxn-missing-stage.json")
		if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Recover(context.Background(), f.state); !errors.Is(err, ErrRecoveryBlocked) {
			t.Errorf("Recover with missing stage error = %v, want ErrRecoveryBlocked", err)
		}
		f.assertOriginalLink(t)
		if _, err := os.Lstat(file.Temp); !os.IsNotExist(err) {
			t.Errorf("missing staged file appeared during recovery: %v", err)
		}
		if len(journals(t, f.state)) != 1 {
			t.Error("blocked missing-stage recovery removed its journal")
		}
	})
}

type linkReplacementFixture struct {
	root         string
	state        string
	path         string
	referent     string
	linkTarget   string
	referentInfo os.FileInfo
}

func newLinkReplacementFixture(t *testing.T) *linkReplacementFixture {
	t.Helper()
	root, state := testDirs(t)
	cache := filepath.Join(root, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	referent := filepath.Join(cache, "payload")
	mustWrite(t, referent, []byte("referent bytes\n"), 0o600)
	referentInfo, err := os.Stat(referent)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hook-support")
	linkTarget, err := filepath.Rel(root, referent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, path); err != nil {
		t.Fatal(err)
	}
	return &linkReplacementFixture{root: root, state: state, path: path, referent: referent, linkTarget: linkTarget, referentInfo: referentInfo}
}

func (f *linkReplacementFixture) desired(t *testing.T, replacement []byte, mode os.FileMode) DesiredFile {
	t.Helper()
	linkTarget, expected, err := ReadSymlinkSnapshot(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != f.linkTarget {
		t.Fatalf("link target = %q, want %q", linkTarget, f.linkTarget)
	}
	return DesiredFile{
		Path:               f.path,
		Expected:           expected,
		ExpectedMode:       0,
		ExpectedLinkTarget: linkTarget,
		Bytes:              append([]byte{}, replacement...),
		Mode:               mode.Perm(),
	}
}

func (f *linkReplacementFixture) assertOriginalLink(t *testing.T) {
	t.Helper()
	info, err := os.Lstat(f.path)
	if err != nil {
		t.Fatalf("Lstat original link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("original destination mode = %v, want symlink", info.Mode())
	}
	if target, err := os.Readlink(f.path); err != nil || target != f.linkTarget {
		t.Fatalf("original link target = %q, %v, want %q", target, err, f.linkTarget)
	}
	f.assertReferentUnchanged(t)
}

func (f *linkReplacementFixture) assertReferentUnchanged(t *testing.T) {
	t.Helper()
	assertFile(t, f.referent, "referent bytes\n", 0o600)
	info, err := os.Stat(f.referent)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(f.referentInfo, info) {
		t.Fatal("link replacement changed the referent inode")
	}
}

func assertLinkReplacementFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat replacement: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("replacement mode = %v, want regular file", info.Mode())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if !bytes.Equal(got, contents) {
		t.Fatalf("replacement bytes = %q, want %q", got, contents)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("replacement permissions = %o, want %o", info.Mode().Perm(), mode.Perm())
	}
}

type linkReplacementPathSnapshot struct {
	exists   bool
	info     os.FileInfo
	bytes    []byte
	linkText string
}

func snapshotLinkReplacementPath(t *testing.T, path string) linkReplacementPathSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return linkReplacementPathSnapshot{}
	}
	if err != nil {
		t.Fatalf("Lstat %s: %v", path, err)
	}
	snapshot := linkReplacementPathSnapshot{exists: true, info: info}
	if info.Mode()&os.ModeSymlink != 0 {
		snapshot.linkText, err = os.Readlink(path)
	} else if info.Mode().IsRegular() {
		snapshot.bytes, err = os.ReadFile(path)
	}
	if err != nil {
		t.Fatalf("read snapshot %s: %v", path, err)
	}
	return snapshot
}

func assertLinkReplacementPathUnchanged(t *testing.T, path string, before linkReplacementPathSnapshot) {
	t.Helper()
	after := snapshotLinkReplacementPath(t, path)
	if before.exists != after.exists {
		t.Fatalf("%s existence changed: before=%v after=%v", path, before.exists, after.exists)
	}
	if !before.exists {
		return
	}
	if !os.SameFile(before.info, after.info) || before.info.Mode() != after.info.Mode() ||
		before.info.Size() != after.info.Size() || !before.info.ModTime().Equal(after.info.ModTime()) {
		t.Errorf("%s file identity or metadata changed", path)
	}
	if before.linkText != after.linkText || !bytes.Equal(before.bytes, after.bytes) {
		t.Errorf("%s bytes or link text changed", path)
	}
}

func assertLinkReplacementCheckPendingReadOnly(t *testing.T, f *linkReplacementFixture, wantStage bool) {
	t.Helper()
	journalPaths := journals(t, f.state)
	if len(journalPaths) != 1 {
		t.Fatalf("journal count before CheckPending = %d, want 1", len(journalPaths))
	}
	journalPath := journalPaths[0]
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var pending journal
	if err := json.Unmarshal(journalBefore, &pending); err != nil {
		t.Fatalf("decode pending journal: %v", err)
	}
	if len(pending.Files) != 1 || pending.Files[0].Temp == "" {
		t.Fatalf("pending replacement files = %+v", pending.Files)
	}
	stagePath := pending.Files[0].Temp
	stageBefore, stageErr := os.ReadFile(stagePath)
	stageInfoBefore, stageInfoErr := os.Lstat(stagePath)
	stagePathsBefore, err := filepath.Glob(filepath.Join(filepath.Dir(f.path), ".dependencytxn-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if wantStage {
		if stageErr != nil || stageInfoErr != nil || len(stagePathsBefore) != 1 || stagePathsBefore[0] != stagePath {
			t.Fatalf("staged replacement missing before CheckPending: file=%v lstat=%v paths=%v", stageErr, stageInfoErr, stagePathsBefore)
		}
	} else if !os.IsNotExist(stageErr) || !os.IsNotExist(stageInfoErr) || len(stagePathsBefore) != 0 {
		t.Fatalf("published replacement retained staging: read=%v lstat=%v paths=%v", stageErr, stageInfoErr, stagePathsBefore)
	}
	pathBefore := snapshotLinkReplacementPath(t, f.path)
	stateNamesBefore := linkReplacementDirectoryNames(t, f.state)
	pathNamesBefore := linkReplacementDirectoryNames(t, filepath.Dir(f.path))
	if err := CheckPending(f.state); !errors.Is(err, ErrRecoveryRequired) {
		t.Errorf("CheckPending error = %v, want ErrRecoveryRequired", err)
	}
	assertLinkReplacementPathUnchanged(t, f.path, pathBefore)
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(journalAfter, journalBefore) {
		t.Errorf("CheckPending changed journal: %v", err)
	}
	stageAfter, stageAfterErr := os.ReadFile(stagePath)
	stageInfoAfter, stageInfoAfterErr := os.Lstat(stagePath)
	if wantStage {
		if stageAfterErr != nil || stageInfoAfterErr != nil || !bytes.Equal(stageAfter, stageBefore) ||
			!os.SameFile(stageInfoBefore, stageInfoAfter) || stageInfoBefore.Mode() != stageInfoAfter.Mode() {
			t.Errorf("CheckPending changed staged replacement: read=%v lstat=%v", stageAfterErr, stageInfoAfterErr)
		}
	} else if !os.IsNotExist(stageAfterErr) || !os.IsNotExist(stageInfoAfterErr) {
		t.Errorf("CheckPending changed absent stage: read=%v lstat=%v", stageAfterErr, stageInfoAfterErr)
	}
	if !slices.Equal(stateNamesBefore, linkReplacementDirectoryNames(t, f.state)) {
		t.Error("CheckPending changed state directory entries")
	}
	if !slices.Equal(pathNamesBefore, linkReplacementDirectoryNames(t, filepath.Dir(f.path))) {
		t.Error("CheckPending changed destination directory entries")
	}
	journalPathsAfter := journals(t, f.state)
	if len(journalPathsAfter) != 1 || journalPathsAfter[0] != journalPath {
		t.Errorf("journal paths after CheckPending = %v, want %q", journalPathsAfter, journalPath)
	}
}

func linkReplacementDirectoryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i := range entries {
		names[i] = entries[i].Name()
	}
	return names
}
