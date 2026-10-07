package dependencytxn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
