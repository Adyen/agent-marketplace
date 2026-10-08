package ownership

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type metadataPublicationObserver struct {
	slog.Handler
	before             func(string, string)
	recovery           func()
	journalCleanup     func(string)
	emptySafetyCleanup func(string)
}

func (h metadataPublicationObserver) Handle(ctx context.Context, record slog.Record) error {
	phase, path, destination := "", "", ""
	record.Attrs(func(attr slog.Attr) bool {
		switch attr.Key {
		case "phase":
			phase = attr.Value.String()
		case "path":
			path = attr.Value.String()
		case "destination":
			destination = attr.Value.String()
		}
		return true
	})
	if phase == "ownership-metadata-publication" && h.before != nil {
		h.before(path, destination)
	}
	if phase == "ownership-recovery-aggregate-observed" && h.recovery != nil {
		h.recovery()
	}
	if phase == "ownership-journal-cleanup" && h.journalCleanup != nil {
		h.journalCleanup(path)
	}
	if phase == "ownership-empty-safety-cleanup" && h.emptySafetyCleanup != nil {
		h.emptySafetyCleanup(path)
	}
	return h.Handler.Handle(ctx, record)
}

// spec: owned-entry-reconciliation/metadata-publication-retains-produced-staged-generation
func TestApplyMetadataPublicationRetainsProducedStagedGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	original, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-produced-state-stage")
	var stagedPath string
	var stagedRaw []byte
	var foreignInfo os.FileInfo
	hit := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(metadataPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), before: func(path, destination string) {
		if hit || destination != stateFileName {
			return
		}
		hit, stagedPath = true, path
		stagedRaw, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, stagedRaw, 0o600); err != nil {
			t.Fatal(err)
		}
		foreignInfo, err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))})
	if current, readErr := os.ReadFile(store.StateFile()); readErr != nil || !bytes.Equal(current, original) {
		t.Fatalf("metadata publication overwrote original state with replacement stage: hit=%t result=%+v err=%v raw=%q read=%v", hit, result, err, current, readErr)
	}
	if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) || result.Generation != 0 {
		t.Fatalf("replaced metadata stage did not generation-refuse: hit=%t result=%+v err=%v", hit, result, err)
	}
	if info, err := os.Stat(store.StateFile()); err != nil || !os.SameFile(originalInfo, info) || originalInfo.Mode() != info.Mode() || !originalInfo.ModTime().Equal(info.ModTime()) {
		t.Fatalf("original state metadata changed: %v", err)
	}
	for _, path := range []string{stagedPath, retained} {
		if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, stagedRaw) {
			t.Fatalf("metadata refusal changed produced/replacement stage: %q %v", path, err)
		}
	}
	if info, err := os.Stat(stagedPath); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
		t.Fatalf("replacement metadata stage changed or removed: %v", err)
	}
	if raw, _ := f.readAggregate(); raw != priorContent {
		t.Fatalf("fixture did not really publish native generation before state refusal: %q", raw)
	}
	if journal, err := store.ReadJournal(); err != nil || journal == nil || journal.Phase != PhaseAggregateRenamed || journal.Generation != 2 {
		t.Fatalf("refusal lost real recovery journal: %+v %v", journal, err)
	}
	if entries, err := os.ReadDir(store.recordDir()); err != nil || len(entries) != 3 {
		t.Fatalf("metadata refusal lost original state/journal/replacement or left owned stage residue: %v %v", entries, err)
	}
	if err := os.Remove(stagedPath); err != nil {
		t.Fatal(err)
	}
	outcomes, err := f.store().Recover()
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledForward {
		t.Fatalf("genuine recovery did not publish original desired state: %+v %v", outcomes, err)
	}
	if record, err := store.Load(); err != nil || record == nil || record.Generation != 2 {
		t.Fatalf("real recovery did not commit generation2: %+v %v", record, err)
	}
	if raw, _ := f.readAggregate(); raw != priorContent {
		t.Fatalf("genuine recovery changed native desired bytes: %q", raw)
	}
	if entries, err := os.ReadDir(store.recordDir()); err != nil || len(entries) != 1 || entries[0].Name() != stateFileName {
		t.Fatalf("healthy recovery left owned staging/journal residue: %v %v", entries, err)
	}
}

func TestMetadataPublicationJournalAndSafetyConsumersRetainStages(t *testing.T) {
	for _, lane := range []string{"journal-phase", "prepared-journal", "safety-manifest"} {
		t.Run(lane, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)
			store := f.store()
			mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
			stateRaw, err := os.ReadFile(store.StateFile())
			if err != nil {
				t.Fatal(err)
			}
			stateInfo, err := os.Stat(store.StateFile())
			if err != nil {
				t.Fatal(err)
			}
			retained := filepath.Join(t.TempDir(), "retained-original-metadata-stage")
			var stagedPath, destinationPath string
			var stagedRaw, originalRaw []byte
			var originalInfo, foreignInfo os.FileInfo
			hit := false
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(metadataPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), before: func(path, destination string) {
				if hit {
					return
				}
				if lane == "safety-manifest" {
					if !strings.HasSuffix(destination, metaSuffix) {
						return
					}
				} else {
					if destination != journalFileName {
						return
					}
					if lane == "journal-phase" {
						raw, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						var journal Journal
						if err := json.Unmarshal(raw, &journal); err != nil {
							t.Fatal(err)
						}
						if journal.Phase != PhaseAggregateRenamed {
							return
						}
					}
				}
				hit, stagedPath, destinationPath = true, path, filepath.Join(filepath.Dir(path), destination)
				var err error
				stagedRaw, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if lane == "journal-phase" {
					originalRaw, err = os.ReadFile(destinationPath)
					if err != nil {
						t.Fatal(err)
					}
					originalInfo, err = os.Stat(destinationPath)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Rename(path, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, stagedRaw, 0o600); err != nil {
					t.Fatal(err)
				}
				foreignInfo, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
			}}))
			var result Result
			if lane == "safety-manifest" {
				_, err = store.BackupOnly(f.rel, f.absFile, ReasonSync)
			} else {
				result, err = store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))})
			}
			if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) || result.Generation != 0 {
				t.Fatalf("real metadata consumer did not refuse replaced stage: hit=%t result=%+v err=%v", hit, result, err)
			}
			if originalInfo == nil {
				if _, err := os.Lstat(destinationPath); !os.IsNotExist(err) {
					t.Fatalf("metadata consumer published foreign stage at original absent name: %v", err)
				}
			} else {
				if raw, err := os.ReadFile(destinationPath); err != nil || !bytes.Equal(raw, originalRaw) {
					t.Fatalf("journal phase consumer overwrote original prepared journal: %q %v", raw, err)
				}
				if info, err := os.Stat(destinationPath); err != nil || !os.SameFile(originalInfo, info) || originalInfo.Mode() != info.Mode() || !originalInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("original prepared journal metadata changed: %v", err)
				}
			}
			for _, path := range []string{stagedPath, retained} {
				if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, stagedRaw) {
					t.Fatalf("consumer refusal changed produced/replacement stage: %q %v", path, err)
				}
			}
			if info, err := os.Stat(stagedPath); err != nil || !os.SameFile(foreignInfo, info) || foreignInfo.Mode() != info.Mode() || !foreignInfo.ModTime().Equal(info.ModTime()) {
				t.Fatalf("consumer removed or changed foreign stage: %v", err)
			}
			if raw, err := os.ReadFile(store.StateFile()); err != nil || !bytes.Equal(raw, stateRaw) {
				t.Fatalf("consumer refusal changed original state: %v", err)
			}
			if info, err := os.Stat(store.StateFile()); err != nil || !os.SameFile(stateInfo, info) || stateInfo.Mode() != info.Mode() || !stateInfo.ModTime().Equal(info.ModTime()) {
				t.Fatalf("consumer refusal changed original state metadata: %v", err)
			}
			expectedNative := nextContent
			if lane == "journal-phase" {
				expectedNative = priorContent
			}
			if raw, _ := f.readAggregate(); raw != expectedNative {
				t.Fatalf("consumer fixture crossed wrong native phase: %q", raw)
			}
			if err := os.Remove(stagedPath); err != nil {
				t.Fatal(err)
			}
			switch lane {
			case "journal-phase":
				outcomes, err := f.store().Recover()
				if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledForward {
					t.Fatalf("original prepared evidence cannot genuinely recover: %+v %v", outcomes, err)
				}
			case "prepared-journal":
				if result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))}); err != nil || result.Generation != 2 {
					t.Fatalf("healthy prepared-journal retry did not really commit: %+v %v", result, err)
				}
			default:
				backup, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
				if err != nil {
					t.Fatalf("healthy safety-manifest retry refused: %v", err)
				}
				if raw, err := os.ReadFile(backup); err != nil || string(raw) != nextContent {
					t.Fatalf("healthy safety retry did not contain actual original native bytes: %q %v", raw, err)
				}
				if raw, err := os.ReadFile(metaPathFor(backup)); err != nil {
					t.Fatal(err)
				} else {
					var manifest BackupManifest
					if err := json.Unmarshal(raw, &manifest); err != nil || manifest.PriorDigest != Digest([]byte(nextContent)) {
						t.Fatalf("healthy safety metadata does not describe actual bytes: %+v %v", manifest, err)
					}
				}
			}
		})
	}
}
