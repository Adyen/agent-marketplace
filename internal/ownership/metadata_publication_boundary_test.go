package ownership

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestApplyMetadataPublicationKeepsPrivateModes(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	var store *Store
	hit := false
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStateCommit {
			return nil
		}
		hit = true
		for _, path := range []string{store.StateFile(), store.JournalFile()} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("metadata mode not private: %s %v", path, err)
			}
		}
		return nil
	})))
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	if !hit {
		t.Fatal("metadata publication control did not commit state")
	}
	if record, err := store.Load(); err != nil || record == nil || record.Generation != 1 {
		t.Fatalf("real private state not readable: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("valid mutation did not publish aggregate: %q", got)
	}
	entries, err := os.ReadDir(store.recordDir())
	if err != nil || len(entries) != 1 || entries[0].Name() != stateFileName {
		t.Fatalf("metadata publication left staging/journal residue: %v %v", entries, err)
	}
}

// spec: owned-entry-reconciliation/retargeted-record-parent-cannot-receive-metadata-publication
func TestApplyMetadataPublicationRefusesRetargetedRecordParent(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	foreign := t.TempDir()
	foreignState := filepath.Join(foreign, stateFileName)
	foreignJournal := filepath.Join(foreign, journalFileName)
	for _, path := range []string{foreignState, foreignJournal} {
		if err := os.WriteFile(path, []byte("inert foreign metadata"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	retained := filepath.Join(t.TempDir(), "retained-record-directory")
	hit := false
	var store *Store
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterAggregateRename {
			return nil
		}
		hit = true
		if err := os.Rename(store.recordDir(), retained); err != nil {
			t.Fatal(err)
		}
		return os.Symlink(foreign, store.recordDir())
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !hit || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict)) {
		t.Fatalf("retargeted metadata parent accepted: hit=%v err=%v", hit, err)
	}
	for _, path := range []string{foreignState, foreignJournal} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "inert foreign metadata" {
			t.Fatalf("foreign metadata changed: %s %q %v", path, data, err)
		}
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 2 {
		t.Fatalf("foreign metadata staging residue: %v %v", entries, err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("fixture did not publish actual aggregate before metadata refusal: %q", got)
	}
	if data, err := os.ReadFile(filepath.Join(retained, journalFileName)); err != nil || len(data) == 0 {
		t.Fatalf("admitted prepared journal lost: %v", err)
	}
}

func TestPublishedMetadataPinRejectsInPlaceEdits(t *testing.T) {
	produced := fsboundary.Identity{Device: 1, Inode: 2, Size: 64, ModTimeNS: 100, ChangeTimeNS: 100, Links: 1, Mode: 0o600, UID: 501}
	renamed := produced
	renamed.ChangeTimeNS = 200
	if !publishedMetadataMatches(produced, renamed) {
		t.Fatal("rename-only change time update refused")
	}
	for name, mutate := range map[string]func(*fsboundary.Identity){
		"size":     func(id *fsboundary.Identity) { id.Size++ },
		"mtime":    func(id *fsboundary.Identity) { id.ModTimeNS++ },
		"mode":     func(id *fsboundary.Identity) { id.Mode = 0o644 },
		"links":    func(id *fsboundary.Identity) { id.Links = 2 },
		"inode":    func(id *fsboundary.Identity) { id.Inode++ },
		"owner":    func(id *fsboundary.Identity) { id.UID++ },
		"non-file": func(id *fsboundary.Identity) { id.Mode = os.ModeNamedPipe | 0o600 },
	} {
		edited := renamed
		mutate(&edited)
		if publishedMetadataMatches(produced, edited) {
			t.Fatalf("published metadata pin accepted a %s change", name)
		}
	}
}

func TestPublishCrossedRenameClassifiesPostRenameFailures(t *testing.T) {
	postRename := fmt.Errorf("%w: publish manifest: %w", fsboundary.ErrDurabilityUncertain, errors.New("injected directory sync failure"))
	if !publishCrossedRename(postRename) {
		t.Fatal("durability-uncertain publication not reported as crossed; its payload could be removed under a renamed manifest")
	}
	for _, preRename := range []error{
		fmt.Errorf("publish manifest: %w", errors.New("injected rename failure")),
		fsboundary.ErrGenerationConflict,
		fsboundary.ErrUnsafeFile,
	} {
		if publishCrossedRename(preRename) {
			t.Fatalf("pre-rename refusal reported as crossed: %v", preRename)
		}
	}
}

func TestBackupOnlyKeepsPayloadWhenManifestRenameIsDurabilityUncertain(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate("{\"servers\":{\"inert\":{}}}\n")
	store := f.store()
	injected := false
	publishMetadataHook = func(destination *fsboundary.Destination, stagedParent *fsboundary.Dir, stagedName string) error {
		if err := destination.Publish(stagedParent, stagedName); err != nil {
			return err
		}
		if !strings.HasSuffix(destination.Path(), metaSuffix) {
			return nil
		}
		injected = true
		return fmt.Errorf("%w: publish %s: injected directory sync failure", fsboundary.ErrDurabilityUncertain, destination.Path())
	}
	t.Cleanup(func() { publishMetadataHook = nil })
	if _, err := store.BackupOnly(f.rel, f.absFile, ReasonSync); !injected || !errors.Is(err, fsboundary.ErrDurabilityUncertain) {
		t.Fatalf("post-rename manifest failure not surfaced: injected=%t err=%v", injected, err)
	}
	manifests, err := filepath.Glob(filepath.Join(store.SafetyBackupDir(f.rel), "*"+metaSuffix))
	if err != nil || len(manifests) != 1 {
		t.Fatalf("renamed manifest not retained: %v %v", manifests, err)
	}
	payload := strings.TrimSuffix(manifests[0], metaSuffix) + backupSuffix
	if data, err := os.ReadFile(payload); err != nil || string(data) != "{\"servers\":{\"inert\":{}}}\n" {
		t.Fatalf("payload referenced by the renamed manifest was removed: %q %v", data, err)
	}
}
