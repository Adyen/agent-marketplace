//go:build unix

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newStagingFor opens the production staging transaction for the fixture's
// aggregate, so a test exercises the same object Apply uses rather than a
// look-alike helper.
func newStagingFor(t *testing.T, f *fixture, st *Store) (*stagingTransaction, string, string) {
	t.Helper()
	dirPath, filePath, err := stagedPathFor(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	staging, err := newStagingTransaction(st.canonicalRoot, f.absFile, dirPath, filePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staging.close() })
	if err := staging.create(); err != nil {
		t.Fatal(err)
	}
	return staging, dirPath, filePath
}

func TestStagingWriteRefusesAPreexistingStagedEntry(t *testing.T) {
	for _, mode := range []string{"symlink-outside", "symlink-inside", "file"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			staging, _, filePath := newStagingFor(t, f, f.store())

			victim := filepath.Join(t.TempDir(), "victim")
			const original = "must survive"
			if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			inside := filepath.Join(f.targetRoot, "inside-the-root")
			if err := os.WriteFile(inside, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "symlink-outside":
				if err := os.Symlink(victim, filePath); err != nil {
					t.Fatal(err)
				}
			case "symlink-inside":
				// An os.Root follows links that stay inside the root, so this is the
				// case O_EXCL — not the root — has to refuse.
				if err := os.Symlink(inside, filePath); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := staging.write([]byte("replacement"), 0o600); err == nil {
				t.Fatal("staging.write followed a pre-existing staged entry")
			}
			for _, path := range []string{victim, inside} {
				if got, err := os.ReadFile(path); err != nil || string(got) != original {
					t.Fatalf("%s = %q, %v; want %q", path, got, err, original)
				}
			}
			if mode != "file" {
				if info, err := os.Lstat(filePath); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("pre-existing staged symlink was changed: info=%v err=%v", info, err)
				}
			}
		})
	}
}

func TestStagingWriteCleanupRemovesPartialFile(t *testing.T) {
	f := newFixture(t)
	staging, dirPath, filePath := newStagingFor(t, f, f.store())
	partial, err := staging.dir.OpenFile(staging.fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := partial.WriteString("partial"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("injected write failure")
	if err := staging.cleanupWrite(partial, cause); !errors.Is(err, cause) {
		t.Fatalf("cleanup error = %v; want cause %v", err, cause)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("partial staged file stat error = %v; want absent", err)
	}
	if err := staging.remove(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Fatalf("staging directory stat error = %v; want absent", err)
	}
}

type stagingPathAttacker struct {
	store         *Store
	point         CrashPoint
	victim        string
	mode          string
	fired         bool
	journal       *Journal
	replaceParent func() error
}

func (a *stagingPathAttacker) Crash(point CrashPoint) error {
	if a.fired || point != a.point {
		return nil
	}
	a.fired = true
	journal, err := a.store.ReadJournal()
	if err != nil {
		return err
	}
	if journal == nil {
		return errors.New("staging attack: journal is missing")
	}
	a.journal = journal
	if a.mode == "replace-parent" {
		if a.replaceParent == nil {
			return errors.New("staging attack: replace-parent callback is missing")
		}
		return a.replaceParent()
	}
	if point == CrashAfterJournalPrepared && journal.StagingDir != "" {
		if err := os.Mkdir(journal.StagingDir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Remove(journal.StagedPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	switch a.mode {
	case "symlink":
		return os.Symlink(a.victim, journal.StagedPath)
	case "file":
		return os.WriteFile(journal.StagedPath, []byte("attacker-controlled\n"), 0o600)
	default:
		return errors.New("staging attack: unknown mode")
	}
}

func TestApplyUsesTheRecordedProductionStagingPath(t *testing.T) {
	for _, mode := range []string{"symlink", "file"} {
		t.Run("pre-existing-"+mode, func(t *testing.T) {
			f := newFixture(t)
			prior := "{}\n"
			f.writeAggregate(prior)
			victim := filepath.Join(t.TempDir(), "victim")
			const original = "must survive"
			if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}

			st := f.store()
			attacker := &stagingPathAttacker{store: st, point: CrashAfterJournalPrepared, victim: victim, mode: mode}
			st.crasher = attacker
			_, err := st.Apply(Mutation{
				Rel:               f.rel,
				AbsFile:           f.absFile,
				NextBytes:         []byte(`{"alpha":"value-alpha"}` + "\n"),
				Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
				Reason:            ReasonSync,
				ExpectPriorDigest: Digest([]byte(prior)),
			})
			if err == nil {
				t.Fatal("Apply unexpectedly published through a pre-existing staging entry")
			}
			if attacker.journal == nil || attacker.journal.StagingDir == "" || filepath.Dir(attacker.journal.StagedPath) != attacker.journal.StagingDir {
				t.Fatalf("journal staging paths = %#v; want a private recorded directory", attacker.journal)
			}
			if got, err := os.ReadFile(victim); err != nil || string(got) != original {
				t.Fatalf("victim = %q, %v; want %q", got, err, original)
			}
			if got, exists := f.readAggregate(); !exists || got != prior {
				t.Fatalf("aggregate = %q, exists=%v; want prior %q", got, exists, prior)
			}

			recovered := f.store()
			if _, err := recovered.Recover(); err == nil {
				t.Fatal("Recover removed a pre-existing staging entry")
			}
			if journal, err := recovered.ReadJournal(); err != nil || journal == nil {
				t.Fatalf("journal after fail-closed recovery = %#v, %v; want preserved", journal, err)
			}
		})
	}
}

func TestGenerationConflictDoesNotFollowPlantedStagingSymlink(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	unrelated := filepath.Join(t.TempDir(), "unrelated")
	const original = "do not truncate"
	if err := os.WriteFile(unrelated, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := f.absFile + ".am-staged-1"
	if err := os.Symlink(unrelated, planted); err != nil {
		t.Fatal(err)
	}

	st := f.store()
	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:               []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:                ReasonSync,
		ExpectPriorGeneration: 1,
		ExpectPriorDigest:     Digest([]byte(prior)),
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Apply error = %v, want ErrGenerationConflict", err)
	}
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != original {
		t.Fatalf("unrelated target = %q, %v; want %q", got, err, original)
	}
	if info, err := os.Lstat(planted); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("planted staging symlink was changed: info=%v err=%v", info, err)
	}
}

func TestApplyRefusesReplacedAggregateDirectory(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	movedRoot := filepath.Join(t.TempDir(), "moved-root")
	attackerRoot := filepath.Join(t.TempDir(), "attacker-root")
	if err := os.Mkdir(attackerRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	st := f.store()
	st.crasher = &stagingPathAttacker{store: st, point: CrashBeforeAggregateRename, victim: filepath.Join(t.TempDir(), "unused"), mode: "replace-parent"}
	attacker := st.crasher.(*stagingPathAttacker)
	attacker.replaceParent = func() error {
		if err := os.Rename(f.targetRoot, movedRoot); err != nil {
			return err
		}
		return os.Symlink(attackerRoot, f.targetRoot)
	}
	_, err := st.Apply(Mutation{
		Rel:               f.rel,
		AbsFile:           f.absFile,
		NextBytes:         []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:            ReasonSync,
		ExpectPriorDigest: Digest([]byte(prior)),
	})
	if err == nil {
		t.Fatal("Apply unexpectedly published through the replaced aggregate directory")
	}
	if got, exists := f.readAggregate(); exists || got != "" {
		t.Fatalf("replaced aggregate path = %q, exists=%v; want absent", got, exists)
	}
	if got, err := os.ReadFile(filepath.Join(movedRoot, f.rel)); err != nil || string(got) != prior {
		t.Fatalf("original aggregate = %q, %v; want prior %q", got, err, prior)
	}
	if got, err := os.ReadFile(filepath.Join(attackerRoot, f.rel)); !os.IsNotExist(err) || got != nil {
		t.Fatalf("attacker aggregate = %q, %v; want absent", got, err)
	}
}

func TestNewJournalsUseThePrivateStagingFormat(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{}\n")
	st := f.store(WithCrasher(CrashAt(CrashAfterJournalPrepared)))
	_, err := st.Apply(Mutation{
		Rel:       f.rel,
		AbsFile:   f.absFile,
		NextBytes: []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:   []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:    ReasonSync,
	})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want injected crash", err)
	}
	journal, err := st.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if journal.Version != 2 || journal.StagingDir == "" || filepath.Dir(journal.StagedPath) != journal.StagingDir {
		t.Fatalf("journal = %#v, want version 2 private staging directory", journal)
	}
}

func TestReadJournalRejectsUnrelatedStagingPath(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{
		Version:       journalVersion,
		RootKey:       st.rootKey,
		CanonicalRoot: st.canonicalRoot,
		AbsFile:       f.absFile,
		StagingDir:    filepath.Join(filepath.Dir(f.absFile), "unrelated"),
		StagedPath:    filepath.Join(filepath.Dir(f.absFile), "unrelated", filepath.Base(f.absFile)),
	}
	if err := st.writeJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadJournal(); err == nil {
		t.Fatal("ReadJournal accepted staging paths outside the recorded private format")
	}
	if _, err := os.Stat(st.JournalFile()); err != nil {
		t.Fatalf("journal = %v, want preserved evidence", err)
	}
}

func TestReadJournalAcceptsLegacyStagingFormat(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := &Journal{
		Version:       legacyJournalVersion,
		RootKey:       st.rootKey,
		CanonicalRoot: st.canonicalRoot,
		Generation:    1,
		Phase:         PhasePrepared,
		File:          f.rel,
		AbsFile:       f.absFile,
		StagedPath:    f.absFile + ".am-staged-1",
		NextDigest:    Digest([]byte("next")),
	}
	if err := st.writeJournal(legacy); err != nil {
		t.Fatal(err)
	}
	got, err := st.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != legacyJournalVersion || got.StagingDir != "" {
		t.Fatalf("legacy journal = %#v, want version %d without staging directory", got, legacyJournalVersion)
	}
}

func TestRecoverRejectsLegacyStagingPathOutsideAggregate(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	const original = "must survive"
	if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := &Journal{
		Version:       legacyJournalVersion,
		RootKey:       st.rootKey,
		CanonicalRoot: st.canonicalRoot,
		Generation:    1,
		Phase:         PhasePrepared,
		File:          f.rel,
		AbsFile:       f.absFile,
		StagedPath:    victim,
		PriorAbsent:   true,
		NextDigest:    Digest([]byte("next")),
	}
	if err := st.writeJournal(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Recover(); err == nil {
		t.Fatal("Recover accepted a legacy staging path outside the aggregate")
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != original {
		t.Fatalf("victim = %q, %v; want %q", got, err, original)
	}
	if journal, err := st.ReadJournal(); err == nil || journal != nil {
		t.Fatalf("journal after rejected legacy path = %#v, %v; want preserved corrupt evidence", journal, err)
	}
	if _, err := os.Stat(st.JournalFile()); err != nil {
		t.Fatalf("journal evidence = %v; want preserved", err)
	}
}

func TestApplyRejectsStagingPathReplacementBeforePublication(t *testing.T) {
	for _, point := range []CrashPoint{CrashAfterStagedWrite, CrashBeforeAggregateRename} {
		for _, mode := range []string{"symlink", "file"} {
			t.Run(string(point)+"-"+mode, func(t *testing.T) {
				f := newFixture(t)
				prior := "{}\n"
				f.writeAggregate(prior)
				victim := filepath.Join(t.TempDir(), "victim")
				const original = "must survive"
				if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}

				st := f.store()
				st.crasher = &stagingPathAttacker{store: st, point: point, victim: victim, mode: mode}
				_, err := st.Apply(Mutation{
					Rel:               f.rel,
					AbsFile:           f.absFile,
					NextBytes:         []byte(`{"alpha":"value-alpha"}` + "\n"),
					Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
					Reason:            ReasonSync,
					ExpectPriorDigest: Digest([]byte(prior)),
				})
				if err == nil {
					t.Fatal("Apply published a staging path that changed after staging")
				}
				if got, err := os.ReadFile(victim); err != nil || string(got) != original {
					t.Fatalf("victim = %q, %v; want %q", got, err, original)
				}
				if got, exists := f.readAggregate(); !exists || got != prior {
					t.Fatalf("aggregate = %q, exists=%v; want prior %q", got, exists, prior)
				}
				// The replacement is refused BEFORE the rename, so the aggregate is
				// never momentarily an attacker-chosen symlink or file. A rollback
				// after publication could not have promised that.
				if info, err := os.Lstat(f.absFile); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("aggregate mode = %v, %v; want an untouched regular file", info, err)
				}
				if _, err := f.store().Recover(); err == nil {
					t.Fatal("Recover removed staging evidence after its identity changed")
				}
				if journal, err := f.store().ReadJournal(); err != nil || journal == nil {
					t.Fatalf("journal after fail-closed recovery = %#v, %v; want preserved", journal, err)
				}
			})
		}
	}
}

// A symlink standing where the aggregate belongs must not donate its target's
// permissions to the published file. Inheriting them would let one planted link
// make every later generation world-writable, which converts a one-shot symlink
// plant into permanent write access to managed content.
func TestPublishedAggregateDoesNotInheritASymlinkTargetMode(t *testing.T) {
	f := newFixture(t)
	victim := filepath.Join(t.TempDir(), "victim")
	const original = "must survive"
	if err := os.WriteFile(victim, []byte(original), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, f.absFile); err != nil {
		t.Fatal(err)
	}

	next := `{"alpha":"value-alpha"}` + "\n"
	mustApply(t, f.store(), Mutation{
		Rel:               f.rel,
		AbsFile:           f.absFile,
		NextBytes:         []byte(next),
		Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:            ReasonSync,
		ExpectPriorDigest: Digest([]byte(original)),
	})

	info, err := os.Lstat(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("published aggregate mode = %v; want a regular file replacing the symlink", info.Mode())
	}
	if info.Mode().Perm() != defaultAggregateMode.Perm() {
		t.Fatalf("published perm = %v; want the default %v", info.Mode().Perm(), defaultAggregateMode.Perm())
	}
	if got, exists := f.readAggregate(); !exists || got != next {
		t.Fatalf("aggregate = %q, exists=%v; want %q", got, exists, next)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != original {
		t.Fatalf("victim = %q, %v; want %q", got, err, original)
	}
}

// A version-1 residue whose pathname now names a symlink is refused rather than
// unlinked. The name is the one version 1 would have written, so pinning the
// pathname is not enough on its own.
func TestRecoverRefusesALegacyStagedSymlink(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	const original = "must survive"
	if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := f.absFile + ".am-staged-1"
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}
	if err := st.writeJournal(&Journal{
		Version:       legacyJournalVersion,
		RootKey:       st.rootKey,
		CanonicalRoot: st.canonicalRoot,
		Generation:    1,
		Phase:         PhasePrepared,
		File:          f.rel,
		AbsFile:       f.absFile,
		StagedPath:    planted,
		PriorAbsent:   true,
		NextDigest:    Digest([]byte("next")),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store().Recover(); err == nil {
		t.Fatal("Recover unlinked a legacy staged path that had become a symlink")
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != original {
		t.Fatalf("victim = %q, %v; want %q", got, err, original)
	}
	if info, err := os.Lstat(planted); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("planted legacy staging symlink = %v, %v; want preserved", info, err)
	}
	if _, err := os.Stat(st.JournalFile()); err != nil {
		t.Fatalf("journal evidence = %v; want preserved", err)
	}
}
