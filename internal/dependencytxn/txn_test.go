package dependencytxn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommitAndUnchangedFiles(t *testing.T) {
	root, state := testDirs(t)
	first := filepath.Join(root, "config.yaml")
	second := filepath.Join(root, "lock.yaml")
	mustWrite(t, first, []byte("old-config\n"), 0o600)
	mustWrite(t, second, []byte("same\n"), 0o640)

	res, err := Commit(context.Background(), state, []DesiredFile{
		{Path: second, Expected: generation(t, second), Bytes: []byte("same\n"), Mode: 0o640},
		{Path: first, Expected: generation(t, first), Bytes: []byte("new-config\n"), Mode: 0o644},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 1 || res.Changed[0] != first {
		t.Fatalf("changed = %#v", res.Changed)
	}
	assertFile(t, first, "new-config\n", 0o644)
	assertFile(t, second, "same\n", 0o640)
	assertNoTransactionResidue(t, root, state)
}

func TestCommitCreatesAndChangesMode(t *testing.T) {
	root, state := testDirs(t)
	created := filepath.Join(root, "lock.yaml")
	modeOnly := filepath.Join(root, "config.yaml")
	mustWrite(t, modeOnly, []byte("same"), 0o600)

	_, err := Commit(context.Background(), state, []DesiredFile{
		{Path: created, Expected: Generation{}, Bytes: []byte("created"), Mode: 0o640},
		{Path: modeOnly, Expected: generation(t, modeOnly), Bytes: []byte("same"), Mode: 0o644},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, created, "created", 0o640)
	assertFile(t, modeOnly, "same", 0o644)
	assertNoTransactionResidue(t, root, state)
}

func TestCommitRejectsConcurrentChangeWithoutResidue(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("before"), 0o600)
	expected := generation(t, path)
	mustWrite(t, path, []byte("foreign"), 0o600)

	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: expected, Bytes: []byte("desired"), Mode: 0o600}})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("error = %v", err)
	}
	assertFile(t, path, "foreign", 0o600)
	assertNoTransactionResidue(t, root, state)
}

func TestCommitRejectsSymlinkedDestinationAncestor(t *testing.T) {
	root, state := testDirs(t)
	external := t.TempDir()
	link := filepath.Join(root, "config")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "config.yaml")

	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Bytes: []byte("desired"), Mode: 0o600}})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	if _, statErr := os.Stat(filepath.Join(external, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink target was written: %v", statErr)
	}
	assertNoTransactionResidue(t, root, state)
}

func TestCommitSupportsSystemPathAlias(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(canonicalRoot, "state")
	path := filepath.Join(root, "config.yaml")
	_, err = Commit(context.Background(), state, []DesiredFile{{Path: path, Bytes: []byte("desired"), Mode: 0o600}})
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "desired", 0o600)
}

func TestCommitRetainsRecoveryEvidenceAfterJournalRenameFsyncFailure(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	injected := errors.New("injected state directory fsync failure")
	failed := false

	_, err := Commit(context.Background(), state, []DesiredFile{{
		Path: path, Expected: generation(t, path), Bytes: []byte("new"), Mode: 0o640,
	}}, WithFsyncDir(func(dir string) error {
		if dir == state && !failed {
			failed = true
			return injected
		}
		return fsyncDir(dir)
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("Commit error = %v, want injected fsync failure", err)
	}
	journalPaths := journals(t, state)
	if len(journalPaths) != 1 {
		t.Fatalf("journals = %v, want durable recovery journal", journalPaths)
	}
	raw, readErr := os.ReadFile(journalPaths[0])
	if readErr != nil {
		t.Fatal(readErr)
	}
	var persisted journal
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Files) != 1 {
		t.Fatalf("journal files = %d, want 1", len(persisted.Files))
	}
	if _, statErr := os.Stat(persisted.Files[0].Temp); statErr != nil {
		t.Fatalf("staged recovery file missing: %v", statErr)
	}

	if err := Recover(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "new", 0o640)
	assertNoTransactionResidue(t, root, state)
}

func TestRecoveryAtEveryPublicationInterruption(t *testing.T) {
	for interruptedAfter := 0; interruptedAfter < 3; interruptedAfter++ {
		t.Run(fmt.Sprintf("after-publish-%d", interruptedAfter), func(t *testing.T) {
			root, state := testDirs(t)
			files := make([]DesiredFile, 3)
			for i := range files {
				path := filepath.Join(root, fmt.Sprintf("%d.yaml", i))
				mustWrite(t, path, []byte(fmt.Sprintf("old-%d", i)), 0o600)
				files[i] = DesiredFile{Path: path, Expected: generation(t, path), Bytes: []byte(fmt.Sprintf("new-%d", i)), Mode: 0o640}
			}
			injected := errors.New("injected crash")
			_, err := Commit(context.Background(), state, files, WithFaultHook(func(p FaultPoint) error {
				if p.Stage == FaultAfterPublish && p.Index == interruptedAfter {
					return injected
				}
				return nil
			}))
			if !errors.Is(err, injected) {
				t.Fatalf("commit error = %v", err)
			}
			if err := Recover(context.Background(), state); err != nil {
				t.Fatal(err)
			}
			for i, file := range files {
				assertFile(t, file.Path, fmt.Sprintf("new-%d", i), 0o640)
			}
			assertNoTransactionResidue(t, root, state)
		})
	}
}

func TestPreJournalInterruptionsLeaveNoTransactionResidue(t *testing.T) {
	points := []FaultPoint{
		{Stage: FaultAfterLocks, Index: -1},
		{Stage: FaultAfterCAS, Index: -1},
		{Stage: FaultAfterStage, Index: 0},
		{Stage: FaultAfterStage, Index: 1},
	}
	for _, point := range points {
		t.Run(fmt.Sprintf("%s-%d", point.Stage, point.Index), func(t *testing.T) {
			root, state := testDirs(t)
			files := make([]DesiredFile, 2)
			for i := range files {
				path := filepath.Join(root, fmt.Sprintf("%d.yaml", i))
				mustWrite(t, path, []byte(fmt.Sprintf("old-%d", i)), 0o600)
				files[i] = DesiredFile{Path: path, Expected: generation(t, path), Bytes: []byte(fmt.Sprintf("new-%d", i)), Mode: 0o600}
			}
			injected := errors.New("injected crash")
			_, err := Commit(context.Background(), state, files, WithFaultHook(func(got FaultPoint) error {
				if got.Stage == point.Stage && got.Index == point.Index {
					return injected
				}
				return nil
			}))
			if !errors.Is(err, injected) {
				t.Fatalf("commit error = %v", err)
			}
			for i, file := range files {
				assertFile(t, file.Path, fmt.Sprintf("old-%d", i), 0o600)
			}
			assertNoTransactionResidue(t, root, state)
		})
	}
}

func TestRecoveryFinishesZeroPublishedPrefix(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	injected := errors.New("injected crash")
	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("new"), Mode: 0o640}}, WithFaultHook(func(p FaultPoint) error {
		if p.Stage == FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("commit error = %v", err)
	}
	if err := Recover(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "new", 0o640)
	assertNoTransactionResidue(t, root, state)
}

func TestStateDirectoryIsPrivate(t *testing.T) {
	root, state := testDirs(t)
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if _, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Bytes: []byte("new"), Mode: 0o600}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode = %o", info.Mode().Perm())
	}
}

func TestCommitRechecksGenerationImmediatelyBeforePublish(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("new"), Mode: 0o600}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultBeforePublish {
			mustWrite(t, path, []byte("foreign"), 0o600)
		}
		return nil
	}))
	if !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Commit error = %v", err)
	}
	assertFile(t, path, "foreign", 0o600)
	if len(journals(t, state)) != 1 {
		t.Fatal("generation conflict removed recovery evidence")
	}
}

func TestCommitRechecksUnchangedAssertionAfterStaging(t *testing.T) {
	root, state := testDirs(t)
	configPath := filepath.Join(root, "config.yaml")
	lockPath := filepath.Join(root, "lock.yaml")
	mustWrite(t, configPath, []byte("stable-config"), 0o600)
	mustWrite(t, lockPath, []byte("old-lock"), 0o600)

	_, err := Commit(context.Background(), state, []DesiredFile{
		{Path: configPath, Expected: generation(t, configPath), Bytes: []byte("stable-config"), Mode: 0o600},
		{Path: lockPath, Expected: generation(t, lockPath), Bytes: []byte("new-lock"), Mode: 0o600},
	}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterStage {
			mustWrite(t, configPath, []byte("concurrent-config"), 0o600)
		}
		return nil
	}))
	if !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Commit error = %v", err)
	}
	assertFile(t, configPath, "concurrent-config", 0o600)
	assertFile(t, lockPath, "old-lock", 0o600)
	if len(journals(t, state)) != 1 {
		t.Fatal("unchanged assertion conflict removed recovery evidence")
	}
}

func TestRecoveryRetainsUnchangedAssertions(t *testing.T) {
	root, state := testDirs(t)
	configPath := filepath.Join(root, "config.yaml")
	lockPath := filepath.Join(root, "lock.yaml")
	mustWrite(t, configPath, []byte("stable-config"), 0o600)
	mustWrite(t, lockPath, []byte("old-lock"), 0o600)
	injected := errors.New("injected crash")
	_, err := Commit(context.Background(), state, []DesiredFile{
		{Path: configPath, Expected: generation(t, configPath), Bytes: []byte("stable-config"), Mode: 0o600},
		{Path: lockPath, Expected: generation(t, lockPath), Bytes: []byte("new-lock"), Mode: 0o600},
	}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("Commit error = %v", err)
	}
	journalPaths := journals(t, state)
	if len(journalPaths) != 1 {
		t.Fatalf("journals = %v", journalPaths)
	}
	raw, err := os.ReadFile(journalPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), configPath) || !strings.Contains(string(raw), `"assertions"`) {
		t.Fatalf("journal omitted unchanged assertion: %s", raw)
	}
	mustWrite(t, configPath, []byte("concurrent-config"), 0o600)
	if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Recover error = %v", err)
	}
	assertFile(t, configPath, "concurrent-config", 0o600)
	assertFile(t, lockPath, "old-lock", 0o600)
	if len(journals(t, state)) != 1 {
		t.Fatal("blocked recovery removed durable preconditions")
	}
}

func TestRecoveryRejectsSymlinkJournal(t *testing.T) {
	_, state := testDirs(t)
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "journal.json")
	mustWrite(t, target, []byte(`{"version":1}`), 0o600)
	link := filepath.Join(state, ".dependencytxn-symlink.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Recover error = %v", err)
	}
}

func TestMalformedJournalBlocksWithoutMutation(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("user"), 0o600)
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(state, ".dependencytxn-malformed.json")
	mustWrite(t, journalPath, []byte(`{"version":1,"files":[`), 0o600)
	if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("recovery error = %v", err)
	}
	assertFile(t, path, "user", 0o600)
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("malformed recovery evidence removed: %v", err)
	}
}

func TestRecoveryBlocksOnForeignBytes(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	injected := errors.New("injected crash")
	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("new"), Mode: 0o640}}, WithFaultHook(func(p FaultPoint) error {
		if p.Stage == FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("commit error = %v", err)
	}
	mustWrite(t, path, []byte("foreign"), 0o600)
	if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("recovery error = %v", err)
	}
	assertFile(t, path, "foreign", 0o600)
	if len(journals(t, state)) != 1 {
		t.Fatalf("blocked recovery removed its evidence")
	}
}

func TestRecoveryBlocksOnNonPrefixState(t *testing.T) {
	root, state := testDirs(t)
	files := make([]DesiredFile, 2)
	for i := range files {
		path := filepath.Join(root, fmt.Sprintf("%d.yaml", i))
		mustWrite(t, path, []byte(fmt.Sprintf("old-%d", i)), 0o600)
		files[i] = DesiredFile{Path: path, Expected: generation(t, path), Bytes: []byte(fmt.Sprintf("new-%d", i)), Mode: 0o600}
	}
	injected := errors.New("injected crash")
	_, err := Commit(context.Background(), state, files, WithFaultHook(func(p FaultPoint) error {
		if p.Stage == FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	mustWrite(t, files[1].Path, files[1].Bytes, 0o600)
	if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("recovery error = %v", err)
	}
	assertFile(t, files[0].Path, "old-0", 0o600)
	assertFile(t, files[1].Path, "new-1", 0o600)
}

func TestUnsafeDestinations(t *testing.T) {
	root, state := testDirs(t)
	regular := filepath.Join(root, "regular")
	mustWrite(t, regular, []byte("x"), 0o600)

	t.Run("symlink destination", func(t *testing.T) {
		link := filepath.Join(root, "link")
		if err := os.Symlink(regular, link); err != nil {
			t.Fatal(err)
		}
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: link, Expected: generation(t, regular), Bytes: []byte("bad"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
		assertFile(t, regular, "x", 0o600)
	})

	t.Run("nonregular destination", func(t *testing.T) {
		dir := filepath.Join(root, "directory")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: dir, Expected: Generation{Exists: true, Digest: Digest(nil)}, Bytes: []byte("bad"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("symlink ancestor", func(t *testing.T) {
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		linkDir := filepath.Join(root, "linked")
		if err := os.Symlink(realDir, linkDir); err != nil {
			t.Fatal(err)
		}
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: filepath.Join(linkDir, "new"), Bytes: []byte("bad"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestReadGenerationRejectsOversizedSparseFileWithoutResidue(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDependencyFileSize + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = ReadGeneration(path)
	if !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("ReadGeneration error = %v, want ErrRecoveryBlocked", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("ReadGeneration created transaction state: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("oversized generation left residue: %v", entries)
	}
}

func TestContextTimeoutWhileLockHeld(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: generationNoTest(path), Bytes: []byte("first"), Mode: 0o600}}, WithFaultHook(func(p FaultPoint) error {
			if p.Stage == FaultAfterLocks {
				close(entered)
				<-release
			}
			return nil
		}))
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := Commit(ctx, state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("second"), Mode: 0o600}})
	if !errors.Is(err, ErrLockTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCommitsSerializeAndCAS(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	mustWrite(t, path, []byte("old"), 0o600)
	expected := generation(t, path)
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: expected, Bytes: []byte("first"), Mode: 0o600}}, WithFaultHook(func(p FaultPoint) error {
			if p.Stage == FaultAfterLocks {
				close(entered)
				<-release
			}
			return nil
		}))
		errs <- err
	}()
	<-entered
	go func() {
		defer wg.Done()
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: expected, Bytes: []byte("second"), Mode: 0o600}})
		errs <- err
	}()
	close(release)
	wg.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrGenerationConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	assertNoTransactionResidue(t, root, state)
}

func TestMultiProcessSerialization(t *testing.T) {
	root, state := testDirs(t)
	path := filepath.Join(root, "config.yaml")
	ready := filepath.Join(root, "ready")
	release := filepath.Join(root, "release")
	mustWrite(t, path, []byte("old"), 0o600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestTxnHelperProcess$")
	cmd.Env = append(os.Environ(),
		"DEPENDENCYTXN_HELPER=1",
		"DEPENDENCYTXN_PATH="+path,
		"DEPENDENCYTXN_STATE="+state,
		"DEPENDENCYTXN_READY="+ready,
		"DEPENDENCYTXN_RELEASE="+release,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	_, err := Commit(ctx, state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("second"), Mode: 0o600}})
	cancel()
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("error = %v", err)
	}
	mustWrite(t, release, nil, 0o600)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "helper", 0o600)
	assertNoTransactionResidue(t, root, state)
}

func TestTxnHelperProcess(t *testing.T) {
	if os.Getenv("DEPENDENCYTXN_HELPER") != "1" {
		return
	}
	path := os.Getenv("DEPENDENCYTXN_PATH")
	state := os.Getenv("DEPENDENCYTXN_STATE")
	ready := os.Getenv("DEPENDENCYTXN_READY")
	release := os.Getenv("DEPENDENCYTXN_RELEASE")
	_, err := Commit(context.Background(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("helper"), Mode: 0o600}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultAfterLocks {
			return nil
		}
		mustWrite(t, ready, nil, 0o600)
		for {
			if _, err := os.Stat(release); err == nil {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func testDirs(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "files")
	state := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root, state
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func generation(t *testing.T, path string) Generation {
	t.Helper()
	g, err := ReadGeneration(path)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func generationNoTest(path string) Generation {
	raw, _ := os.ReadFile(path)
	return Generation{Exists: true, Digest: Digest(raw)}
}

func assertFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != contents {
		t.Fatalf("%s contents = %q", path, raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
}

func assertNoTransactionResidue(t *testing.T, root, state string) {
	t.Helper()
	for _, dir := range []string{root, state} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".dependencytxn-") || strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("transaction residue: %s", filepath.Join(dir, entry.Name()))
			}
		}
	}
}

func journals(t *testing.T, state string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(state, ".dependencytxn-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
