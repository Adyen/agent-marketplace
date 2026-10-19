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

// spec: owned-entry-reconciliation/recovery-cleanup-rechecks-original-state-after-aggregate-read
func TestRecoverCleanupRechecksOriginalStateAfterAggregateRead(t *testing.T) {
	for _, action := range []string{"rollback", "absent-rollback", "abandon"} {
		for _, replacement := range []string{"same-json-file", "alias"} {
			t.Run(action+"/"+replacement, func(t *testing.T) {
				f := newFixture(t)
				mustApply(t, f.store(), Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync})
				priorDigest := Digest([]byte(priorContent))
				if action == "absent-rollback" {
					if err := os.Remove(f.absFile); err != nil {
						t.Fatal(err)
					}
					priorDigest = ""
				}
				crashing := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
				if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: priorDigest}); !errors.Is(err, ErrInjectedCrash) {
					t.Fatalf("real prepared generation failed: %v", err)
				}
				journal, err := crashing.ReadJournal()
				if err != nil || journal == nil || journal.Phase != PhasePrepared {
					t.Fatalf("real prepared journal missing: %+v %v", journal, err)
				}
				stateRaw, err := os.ReadFile(crashing.StateFile())
				if err != nil {
					t.Fatal(err)
				}
				stateInfo, err := os.Stat(crashing.StateFile())
				if err != nil {
					t.Fatal(err)
				}
				journalRaw, err := os.ReadFile(crashing.JournalFile())
				if err != nil {
					t.Fatal(err)
				}
				journalInfo, err := os.Stat(crashing.JournalFile())
				if err != nil {
					t.Fatal(err)
				}
				stageInfo, err := os.Stat(journal.StagedPath)
				if err != nil {
					t.Fatal(err)
				}
				nativeRaw := []byte(priorContent)
				if action == "abandon" {
					nativeRaw = []byte("inert handwritten changed native aggregate")
					if err := os.WriteFile(f.absFile, nativeRaw, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				var nativeInfo os.FileInfo
				if action != "absent-rollback" {
					nativeInfo, err = os.Stat(f.absFile)
					if err != nil {
						t.Fatal(err)
					}
				}
				retained, foreign := filepath.Join(t.TempDir(), "retained-original-recovery-state"), filepath.Join(t.TempDir(), "foreign-recovery-state")
				if replacement == "alias" {
					if err := os.WriteFile(foreign, stateRaw, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				var foreignInfo os.FileInfo
				hit := false
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(metadataPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), recovery: func() {
					if hit {
						return
					}
					hit = true
					if err := os.Rename(crashing.StateFile(), retained); err != nil {
						t.Fatal(err)
					}
					if replacement == "alias" {
						if err := os.Symlink(foreign, crashing.StateFile()); err != nil {
							t.Fatal(err)
						}
					} else {
						foreign = crashing.StateFile()
						if err := os.WriteFile(foreign, stateRaw, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					foreignInfo, err = os.Stat(foreign)
					if err != nil {
						t.Fatal(err)
					}
				}}))
				outcomes, err := f.store().Recover()
				expected := fsboundary.ErrGenerationConflict
				if replacement == "alias" {
					expected = fsboundary.ErrUnsafePath
				}
				if !hit || len(outcomes) != 0 || !errors.Is(err, expected) {
					t.Fatalf("recovery acted on late substituted state: hit=%t outcomes=%+v err=%v", hit, outcomes, err)
				}
				for _, item := range []struct {
					path string
					raw  []byte
					info os.FileInfo
				}{{foreign, stateRaw, foreignInfo}, {retained, stateRaw, stateInfo}, {crashing.JournalFile(), journalRaw, journalInfo}, {journal.StagedPath, []byte(nextContent), stageInfo}} {
					if raw, err := os.ReadFile(item.path); err != nil || !bytes.Equal(raw, item.raw) {
						t.Fatalf("late state refusal changed real recovery bytes: %q %v", item.path, err)
					}
					if info, err := os.Stat(item.path); err != nil || !os.SameFile(item.info, info) || item.info.Mode() != info.Mode() || !item.info.ModTime().Equal(info.ModTime()) {
						t.Fatalf("late state refusal changed recovery metadata: %q %v", item.path, err)
					}
				}
				if nativeInfo != nil {
					if raw, err := os.ReadFile(f.absFile); err != nil || !bytes.Equal(raw, nativeRaw) {
						t.Fatalf("recovery refusal changed native bytes: %q %v", raw, err)
					}
					if info, err := os.Stat(f.absFile); err != nil || !os.SameFile(nativeInfo, info) || nativeInfo.Mode() != info.Mode() || !nativeInfo.ModTime().Equal(info.ModTime()) {
						t.Fatalf("recovery refusal changed native metadata: %v", err)
					}
				} else if _, err := os.Lstat(f.absFile); !os.IsNotExist(err) {
					t.Fatalf("absent native aggregate created by refusal: %v", err)
				}
				if replacement == "alias" {
					if link, err := os.Readlink(crashing.StateFile()); err != nil || link != foreign {
						t.Fatalf("state alias changed: %q %v", link, err)
					}
				}
				if entries, err := os.ReadDir(crashing.recordDir()); err != nil || len(entries) != 2 {
					t.Fatalf("refusal changed metadata membership or left stage: %v %v", entries, err)
				}
				if err := os.Remove(crashing.StateFile()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(retained, crashing.StateFile()); err != nil {
					t.Fatal(err)
				}
				outcomes, err = f.store().Recover()
				wantAction := ActionRolledBack
				if action == "abandon" {
					wantAction = ActionAbandoned
					if !errors.Is(err, ErrForeignChange) {
						t.Fatalf("genuine abandonment lost foreign-change diagnostic: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if len(outcomes) != 1 || outcomes[0].Action != wantAction {
					t.Fatalf("original authority cannot genuinely complete recovery: %+v %v", outcomes, err)
				}
				for _, path := range []string{journal.StagedPath, crashing.JournalFile()} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("genuine recovery did not remove owned evidence: %q %v", path, err)
					}
				}
				if record, err := crashing.Load(); err != nil || record == nil || record.Generation != 1 {
					t.Fatalf("genuine recovery changed committed state: %+v %v", record, err)
				}
				if nativeInfo != nil {
					if raw, err := os.ReadFile(f.absFile); err != nil || !bytes.Equal(raw, nativeRaw) {
						t.Fatalf("genuine recovery overwrote handwritten/prior bytes: %q %v", raw, err)
					}
				} else if _, err := os.Lstat(f.absFile); !os.IsNotExist(err) {
					t.Fatalf("genuine recovery created absent native aggregate: %v", err)
				}
			})
		}
	}
}
