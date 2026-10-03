package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type publicationBoundaryCrasher func(CrashPoint) error

func (c publicationBoundaryCrasher) Crash(point CrashPoint) error { return c(point) }

// spec: owned-entry-reconciliation/durable-staging-refusal-preserves-authority
func TestApplyRetargetedParentDoesNotStageOutsideRoot(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\"prior\":true}\n")
	foreign := f.t.TempDir()
	foreignFile := filepath.Join(foreign, filepath.Base(f.absFile))
	if err := os.WriteFile(foreignFile, []byte("{\"foreign\":true}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(f.targetRoot), "retained-root")
	hit := false
	stagedOutside := false
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point == CrashAfterStagedWrite {
			entries, err := os.ReadDir(foreign)
			if err != nil {
				t.Fatal(err)
			}
			stagedOutside = len(entries) != 1
			return nil
		}
		if point != CrashAfterJournalPrepared {
			return nil
		}
		hit = true
		if err := os.Rename(f.targetRoot, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, f.targetRoot); err != nil {
			t.Fatal(err)
		}
		return nil
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte("{\"next\":true}\n"), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte("{\"prior\":true}\n"))})
	if !hit || err == nil {
		t.Fatalf("staging parent retarget fixture did not refuse: hit=%v err=%v", hit, err)
	}
	if stagedOutside {
		t.Fatal("ownership staged private replacement bytes in the foreign namespace before refusal")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(f.absFile) {
		t.Fatalf("ownership staging escaped into foreign root: %v %v", entries, err)
	}
	if data, err := os.ReadFile(foreignFile); err != nil || string(data) != "{\"foreign\":true}\n" {
		t.Fatalf("foreign bytes changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(retained, filepath.Base(f.absFile))); err != nil || string(data) != "{\"prior\":true}\n" {
		t.Fatalf("admitted bytes changed: %q %v", data, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 1 {
		t.Fatalf("refused staging residue in admitted root: %v %v", entries, err)
	}
}

func TestApplyRefusesReplacementStagedGeneration(t *testing.T) {
	f := newFixture(t)
	prior := []byte("{\"prior\":true}\n")
	f.writeAggregate(string(prior))
	retained := filepath.Join(t.TempDir(), "retained-stage")
	var stagePath string
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStagedWrite {
			return nil
		}
		journal, err := f.store().ReadJournal()
		if err != nil || journal == nil {
			t.Fatalf("prepared journal missing: %v", err)
		}
		stagePath = journal.StagedPath
		if err := os.Rename(stagePath, retained); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(stagePath, []byte("foreign inert staged bytes"), 0o640)
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte("{\"next\":true}\n"), Reason: ReasonSync, ExpectPriorDigest: Digest(prior)})
	if stagePath == "" || !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("replacement stage accepted: %s %v", stagePath, err)
	}
	if data, err := os.ReadFile(f.absFile); err != nil || !bytes.Equal(data, prior) {
		t.Fatalf("target bytes changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(stagePath); err != nil || string(data) != "foreign inert staged bytes" {
		t.Fatalf("foreign stage removed: %q %v", data, err)
	}
}

func TestApplyCapturedPublicationPreservesSharedMode(t *testing.T) {
	f := newFixture(t)
	prior, next := []byte("{\"prior\":true}\n"), []byte("{\"next\":true}\n")
	f.writeAggregate(string(prior))
	if err := os.Chmod(f.absFile, 0o640); err != nil {
		t.Fatal(err)
	}
	store := f.store()
	result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: next, Reason: ReasonSync, ExpectPriorDigest: Digest(prior)})
	if err != nil || result.Generation != 1 {
		t.Fatalf("captured publication positive control failed: %+v %v", result, err)
	}
	if data, err := os.ReadFile(f.absFile); err != nil || !bytes.Equal(data, next) {
		t.Fatalf("native content update did not occur: %q %v", data, err)
	}
	if info, err := os.Stat(f.absFile); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("shared mode not preserved: %v", err)
	}
	if entries, err := os.ReadDir(f.targetRoot); err != nil || len(entries) != 1 {
		t.Fatalf("publication residue: %v %v", entries, err)
	}
}

func TestApplyRefusesSameBytesReplacementGeneration(t *testing.T) {
	f := newFixture(t)
	prior := []byte("{\"prior\":true}\n")
	f.writeAggregate(string(prior))
	retained := filepath.Join(t.TempDir(), "retained.json")
	hit := false
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStagedWrite {
			return nil
		}
		hit = true
		if err := os.Rename(f.absFile, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.absFile, prior, 0o640); err != nil {
			t.Fatal(err)
		}
		return nil
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte("{\"next\":true}\n"), Reason: ReasonSync, ExpectPriorDigest: Digest(prior)})
	if !hit || !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("same-byte generation replacement accepted: hit=%v err=%v", hit, err)
	}
	for _, path := range []string{f.absFile, retained} {
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, prior) {
			t.Fatalf("publication changed %s: %q %v", path, data, err)
		}
	}
}
