package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestSafetyEvidenceRefusesUnsafeShapes(t *testing.T) {
	for _, shape := range []string{"hardlink", "external parent", "dangling parent"} {
		t.Run(shape, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)
			store := f.store()
			result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
			manifest := metaPathFor(result.BackupPath)
			before, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(t.TempDir(), "foreign-evidence")
			if shape == "hardlink" {
				if err := os.Link(manifest, foreign); err != nil {
					t.Fatal(err)
				}
			} else {
				parent := store.SafetyBackupDir(f.rel)
				if err := os.Rename(parent, foreign); err != nil {
					t.Fatal(err)
				}
				link := foreign
				if shape == "dangling parent" {
					link = filepath.Join(t.TempDir(), "absent")
				}
				if err := os.Symlink(link, parent); err != nil {
					t.Fatal(err)
				}
				foreign = filepath.Join(foreign, filepath.Base(manifest))
			}
			if backups, err := store.SafetyBackups(f.rel); len(backups) != 0 || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)) {
				t.Fatalf("unsafe evidence admitted or hidden as absence: %v %v", backups, err)
			}
			if data, err := os.ReadFile(foreign); err != nil || string(data) != string(before) {
				t.Fatalf("foreign evidence changed: %v", err)
			}
		})
	}
}

// spec: owned-entry-reconciliation/aliased-safety-manifest-cannot-nominate-orphan-removal
func TestOrphanCandidatesRefuseAliasedSafetyManifest(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)
	result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Entries: []Entry{entry}, Reason: ReasonSync})
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(f.rel)
	if !errors.Is(err, ErrStateMissing) || proof.OwnedCount() != 0 {
		t.Fatalf("orphan fixture did not lose ownership proof: %v", err)
	}
	manifestPath := metaPathFor(result.BackupPath)
	foreign := filepath.Join(t.TempDir(), "foreign-manifest")
	if err := os.Rename(manifestPath, foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, manifestPath); err != nil {
		t.Fatal(err)
	}
	existing := []EntryValue{{ID: entry.ID(), ValueDigest: entry.ValueDigest}}
	candidates, err := store.OrphanCandidates(proof, f.rel, existing, nil)
	if len(candidates) != 0 || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)) {
		t.Fatalf("foreign safety claims nominated orphan removal: %v %v", candidates, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != string(before) {
		t.Fatalf("foreign safety metadata changed: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("evidence read changed aggregate: %q", got)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, manifestPath); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.OrphanCandidates(proof, f.rel, existing, nil)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("valid real safety evidence failed orphan nomination: %v %v", candidates, err)
	}
}
