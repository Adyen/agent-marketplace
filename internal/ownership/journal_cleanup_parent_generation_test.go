package ownership

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: owned-entry-reconciliation/journal-cleanup-refuses-detached-original-parent
func TestJournalCleanupRefusesDetachedOriginalParent(t *testing.T) {
	for _, consumer := range []string{"apply", "recover"} {
		for _, replacement := range []string{"directory", "alias"} {
			t.Run(consumer+"/"+replacement, func(t *testing.T) {
				f := newFixture(t)
				f.writeAggregate(priorContent)
				store := f.store()
				if consumer == "recover" {
					crashing := f.store(WithCrasher(CrashAt(CrashAfterStateCommit)))
					if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
						t.Fatalf("real committed crash fixture failed: %v", err)
					}
				}
				foreign, retained := t.TempDir(), filepath.Join(t.TempDir(), "retained-original-journal-directory")
				foreignRaw := []byte("foreign inert metadata")
				foreignInfo := make(map[string]os.FileInfo)
				for _, name := range []string{stateFileName, journalFileName} {
					if err := os.WriteFile(filepath.Join(foreign, name), foreignRaw, 0o640); err != nil {
						t.Fatal(err)
					}
					var err error
					foreignInfo[name], err = os.Stat(filepath.Join(foreign, name))
					if err != nil {
						t.Fatal(err)
					}
				}
				foreignDirectory, err := os.Stat(foreign)
				if err != nil {
					t.Fatal(err)
				}
				originalRaw := make(map[string][]byte)
				originalInfo := make(map[string]os.FileInfo)
				var originalDirectory, nativeInfo os.FileInfo
				hit := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(metadataPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), journalCleanup: func(path string) {
					if hit {
						return
					}
					hit = true
					var err error
					originalDirectory, err = os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					nativeInfo, err = os.Stat(f.absFile)
					if err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{stateFileName, journalFileName} {
						originalRaw[name], err = os.ReadFile(filepath.Join(path, name))
						if err != nil {
							t.Fatal(err)
						}
						originalInfo[name], err = os.Stat(filepath.Join(path, name))
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Rename(store.recordDir(), retained); err != nil {
						t.Fatal(err)
					}
					if replacement == "directory" {
						err = os.Rename(foreign, store.recordDir())
					} else {
						err = os.Symlink(foreign, store.recordDir())
					}
					if err != nil {
						t.Fatal(err)
					}
				}}))
				var result Result
				var outcomes []RecoveryOutcome
				if consumer == "apply" {
					result, err = store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
				} else {
					outcomes, err = store.Recover()
				}
				expected := fsboundary.ErrGenerationConflict
				if replacement == "alias" {
					expected = fsboundary.ErrUnsafePath
				}
				if raw, readErr := os.ReadFile(filepath.Join(retained, journalFileName)); readErr != nil || !bytes.Equal(raw, originalRaw[journalFileName]) {
					t.Fatalf("journal cleanup consumed detached original evidence: hit=%t result=%+v outcomes=%+v err=%v read=%v", hit, result, outcomes, err, readErr)
				}
				if !hit || result.Generation != 0 || len(outcomes) != 0 || !errors.Is(err, expected) {
					t.Fatalf("detached journal parent did not refuse: hit=%t result=%+v outcomes=%+v err=%v", hit, result, outcomes, err)
				}
				foreignPath := foreign
				if replacement == "directory" {
					foreignPath = store.recordDir()
				}
				for _, name := range []string{stateFileName, journalFileName} {
					if raw, err := os.ReadFile(filepath.Join(retained, name)); err != nil || !bytes.Equal(raw, originalRaw[name]) {
						t.Fatalf("original metadata bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(retained, name)); err != nil || !os.SameFile(originalInfo[name], info) || originalInfo[name].Mode() != info.Mode() || !originalInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("original metadata changed: %q %v", name, err)
					}
					if raw, err := os.ReadFile(filepath.Join(foreignPath, name)); err != nil || !bytes.Equal(raw, foreignRaw) {
						t.Fatalf("foreign metadata bytes changed: %q %v", name, err)
					}
					if info, err := os.Stat(filepath.Join(foreignPath, name)); err != nil || !os.SameFile(foreignInfo[name], info) || foreignInfo[name].Mode() != info.Mode() || !foreignInfo[name].ModTime().Equal(info.ModTime()) {
						t.Fatalf("foreign metadata changed: %q %v", name, err)
					}
				}
				for _, item := range []struct {
					path string
					info os.FileInfo
				}{{retained, originalDirectory}, {foreignPath, foreignDirectory}} {
					if info, err := os.Stat(item.path); err != nil || !os.SameFile(item.info, info) || item.info.Mode() != info.Mode() || !item.info.ModTime().Equal(info.ModTime()) {
						t.Fatalf("journal cleanup changed original/foreign directory metadata: %q %v", item.path, err)
					}
					if entries, err := os.ReadDir(item.path); err != nil || len(entries) != 2 {
						t.Fatalf("journal cleanup changed original/foreign membership: %q %v %v", item.path, entries, err)
					}
				}
				if raw, _ := f.readAggregate(); raw != nextContent {
					t.Fatalf("fixture did not really publish native generation before cleanup: %q", raw)
				}
				if info, err := os.Stat(f.absFile); err != nil || !os.SameFile(nativeInfo, info) || nativeInfo.Mode() != info.Mode() || !nativeInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("late journal refusal changed committed native metadata: %v", err)
				}
				if replacement == "alias" {
					if link, err := os.Readlink(store.recordDir()); err != nil || link != foreign {
						t.Fatalf("journal cleanup changed alias: %q %v", link, err)
					}
					if err := os.Remove(store.recordDir()); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Rename(store.recordDir(), foreign); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Rename(retained, store.recordDir()); err != nil {
					t.Fatal(err)
				}
				outcomes, err = f.store().Recover()
				if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionAlreadyCommitted {
					t.Fatalf("original committed journal cannot genuinely recover: %+v %v", outcomes, err)
				}
				if record, err := store.Load(); err != nil || record == nil || record.Generation != 1 {
					t.Fatalf("healthy original committed state missing: %+v %v", record, err)
				}
				if journal, err := store.ReadJournal(); err != nil || journal != nil {
					t.Fatalf("healthy recovery did not clear actual journal: %+v %v", journal, err)
				}
				if entries, err := os.ReadDir(store.recordDir()); err != nil || len(entries) != 1 || entries[0].Name() != stateFileName {
					t.Fatalf("healthy recovery left stage/journal residue: %v %v", entries, err)
				}
			})
		}
	}
}
