//go:build unix

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStagedRefusesPreexistingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "unrelated")
	stage := filepath.Join(dir, "aggregate.json.am-staged-known")
	const original = "must survive"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, stage); err != nil {
		t.Fatal(err)
	}

	if _, err := writeStaged(stage, []byte("replacement"), 0o600); err == nil {
		t.Fatal("writeStaged followed a pre-existing staging symlink")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != original {
		t.Fatalf("unrelated target = %q, %v; want %q", got, err, original)
	}
	if info, err := os.Lstat(stage); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("pre-existing staging symlink was changed: info=%v err=%v", info, err)
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
	st.crasher = &stagingPathAttacker{store: st, point: CrashAfterStagedVerified, victim: filepath.Join(t.TempDir(), "unused"), mode: "replace-parent"}
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

func TestApplyRejectsStagingPathReplacementBeforePublication(t *testing.T) {
	for _, point := range []CrashPoint{CrashAfterStagedWrite, CrashAfterStagedVerified} {
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
