//go:build e2e

package dependencytxn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

const (
	placeCrashChildEnv = "AGENT_MARKETPLACE_PLACE_CRASH_CHILD"
	placeCrashPlanEnv  = "AGENT_MARKETPLACE_PLACE_CRASH_PLAN"
)

// spec: target-profile-place/edited-stale-file-survives-prune-recovery
func TestPlaceCrashRecoveryBoundaries(t *testing.T) {
	for _, stage := range []FaultStage{
		FaultPlaceAfterStage,
		FaultPlaceAfterDirectory,
		FaultPlaceAfterPublish,
		FaultPlaceAfterOwnership,
		FaultPlaceAfterPrune,
	} {
		t.Run(string(stage), func(t *testing.T) {
			root, stateDir, plan := newPlaceFixture(t, false)
			if stage == FaultPlaceAfterPublish {
				second := plan.Writes[0]
				second.Path = "mods/second.txt"
				plan.Writes = append(plan.Writes, second)
			}
			var stalePath string
			if stage == FaultPlaceAfterPrune || stage == FaultPlaceAfterOwnership {
				stalePath = filepath.Join(root, "layouts", "stale.txt")
				if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(stalePath, []byte("stale\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				parent, err := fsboundary.OpenDir(filepath.Dir(stalePath))
				if err != nil {
					t.Fatal(err)
				}
				plan.Prunes = []PlacePrune{{
					Path: "layouts/stale.txt", Parent: parent.Identity(),
					Expected: Generation{Exists: true, Digest: Digest([]byte("stale\n"))},
					Mode:     0o644,
				}}
				if err := parent.Close(); err != nil {
					t.Fatal(err)
				}
			}
			planFile := filepath.Join(t.TempDir(), "place-plan.json")
			runPlaceCrashChild(t, planFile, plan, stage, 0)

			pending := CheckPending(stateDir)
			wantPendingPath := "mods"
			if stage == FaultPlaceAfterOwnership {
				wantPendingPath = "ownership"
			} else if stage == FaultPlaceAfterPrune {
				wantPendingPath = "layouts"
			}
			if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), filepath.Join(stateDir, ".dependencytxn-")) ||
				!strings.Contains(pending.Error(), wantPendingPath) {
				t.Fatalf("CheckPending = %v", pending)
			}
			blockedPath := filepath.Join(t.TempDir(), "unrelated-config.yaml")
			_, blocked := Commit(context.Background(), stateDir, []DesiredFile{{
				Path: blockedPath, Expected: Generation{}, Bytes: []byte("blocked"), Mode: 0o600,
			}})
			if !errors.Is(blocked, ErrRecoveryRequired) {
				t.Fatalf("unrelated mutation while place journal pending = %v", blocked)
			}
			keys, err := ReadPendingPlaceGenerationKeys(stateDir)
			if err != nil || !reflect.DeepEqual(keys, []string{plan.Writes[0].GenerationKey}) {
				t.Fatalf("pending generation keys = %v, %v", keys, err)
			}
			if stage == FaultPlaceAfterStage {
				if _, err := os.Lstat(filepath.Join(root, "mods")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("read-only check or stage crash wrote into target: %v", err)
				}
				stageRoot := filepath.Join(filepath.Dir(stateDir), "place-stage")
				entries, err := os.ReadDir(stageRoot)
				if err != nil || len(entries) != 1 {
					t.Fatalf("stage outside target root = %v, %v", entries, err)
				}
			}
			if stage == FaultPlaceAfterPublish {
				j, err := readJournal(journals(t, stateDir)[0])
				if err != nil {
					t.Fatal(err)
				}
				stagePath := filepath.Join(filepath.Dir(stateDir), filepath.FromSlash(j.Place.Stage))
				if err := os.Remove(filepath.Join(stagePath, j.Place.Writes[1].StageName)); err != nil {
					t.Fatal(err)
				}
			}
			if stage == FaultPlaceAfterOwnership {
				if err := os.WriteFile(stalePath, []byte("user edit\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var recoveryLog bytes.Buffer
			if stage == FaultPlaceAfterOwnership {
				err := func() error {
					previous := slog.Default()
					slog.SetDefault(slog.New(slog.NewTextHandler(&recoveryLog, nil)))
					defer slog.SetDefault(previous)
					return Recover(context.Background(), stateDir)
				}()
				if err != nil {
					t.Fatal(err)
				}
			} else if err := Recover(context.Background(), stateDir); err != nil {
				t.Fatal(err)
			}
			if stage == FaultPlaceAfterStage {
				if _, err := os.Lstat(filepath.Join(root, "mods", "new.txt")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("pre-write recovery resumed publication: %v", err)
				}
			} else if _, err := os.Stat(filepath.Join(root, "mods", "new.txt")); err != nil {
				t.Fatalf("recovery did not complete publication: %v", err)
			}
			if stage == FaultPlaceAfterPublish {
				if _, err := os.Stat(filepath.Join(root, "mods", "second.txt")); err != nil {
					t.Fatalf("recovery did not complete the unpublished suffix: %v", err)
				}
			}
			if stage == FaultPlaceAfterPrune {
				if _, err := os.Lstat(stalePath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("marked prune was not completed: %v", err)
				}
			} else if stage == FaultPlaceAfterOwnership {
				assertFile(t, stalePath, "user edit\n", 0o644)
				if !strings.Contains(recoveryLog.String(), "place transaction retained edited paths") ||
					!strings.Contains(recoveryLog.String(), "layouts/stale.txt") {
					t.Fatalf("recovery retention log = %q, want edited layouts/stale.txt", recoveryLog.String())
				}
			}
			if err := CheckPending(stateDir); err != nil {
				t.Fatalf("journal remains after recovery: %v", err)
			}
		})
	}
}

func TestPlaceCrashPartialOwnershipNext(t *testing.T) {
	for _, variant := range []string{"verified", "modified"} {
		t.Run(variant, func(t *testing.T) {
			_, stateDir, plan := newPlaceFixture(t, false)
			runPlaceCrashChild(t, filepath.Join(t.TempDir(), "plan.json"), plan, FaultPlaceAfterOwnership, 0)
			journalPath := journals(t, stateDir)[0]
			j, err := readJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			// The ownership operation is marked. Move its published bytes back
			// to staging and shorten them to model interruption during write.
			next, err := os.ReadFile(plan.Ownership.Path)
			if err != nil {
				t.Fatal(err)
			}
			stageFile := filepath.Join(filepath.Dir(stateDir), filepath.FromSlash(j.Place.Stage), placeOwnershipNextName)
			if err := os.Rename(plan.Ownership.Path, stageFile); err != nil {
				t.Fatal(err)
			}
			prefix := bytes.Clone(next[:len(next)-1])
			if variant == "modified" {
				prefix[len(prefix)-1] ^= 1
			}
			if err := os.WriteFile(stageFile, prefix, plan.Ownership.Mode); err != nil {
				t.Fatal(err)
			}
			err = Recover(context.Background(), stateDir)
			if variant == "modified" {
				if !errors.Is(err, ErrRecoveryBlocked) || !strings.Contains(err.Error(), placeOwnershipNextName) {
					t.Fatalf("recovery of modified stage = %v", err)
				}
				assertFile(t, stageFile, string(prefix), plan.Ownership.Mode)
				if _, err := os.Stat(journalPath); err != nil {
					t.Fatalf("journal not retained: %v", err)
				}
				if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unexpected ownership publication: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertFile(t, plan.Ownership.Path, string(next), plan.Ownership.Mode)
			if err := CheckPending(stateDir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// spec: target-profile-place/ownership-commits-once-after-full-publication
func TestPlaceCrashCommitsOwnershipOnce(t *testing.T) {
	for _, test := range []struct {
		point     FaultStage
		committed bool
	}{
		{point: FaultPlaceAfterPublish},
		{point: FaultPlaceAfterOwnership, committed: true},
	} {
		t.Run(string(test.point), func(t *testing.T) {
			root, stateDir, plan := newPlaceFixture(t, true)
			if err := os.Mkdir(filepath.Join(root, "mods"), 0o755); err != nil {
				t.Fatal(err)
			}
			plan.Directories = nil
			oldOwnership, err := os.ReadFile(plan.Ownership.Path)
			if err != nil {
				t.Fatal(err)
			}
			intendedOwnership, err := json.Marshal(map[string]any{
				"canonical_root": root, "root_key": testPlaceRootKey(root),
				"place_directories": []any{}, "fixture_marker": "committed",
			})
			if err != nil {
				t.Fatal(err)
			}
			plan.Ownership.Bytes = intendedOwnership
			if reflect.DeepEqual(oldOwnership, intendedOwnership) {
				t.Fatal("fixture old and intended ownership records must differ")
			}

			runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, test.point, 0)
			assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
			if pending := journals(t, stateDir); len(pending) != 1 {
				t.Fatalf("crashed ownership publication journals = %v, want one", pending)
			}
			crashedBytes, err := os.ReadFile(plan.Ownership.Path)
			if err != nil {
				t.Fatal(err)
			}
			crashedInfo, err := os.Lstat(plan.Ownership.Path)
			if err != nil {
				t.Fatal(err)
			}
			wantCrashed := oldOwnership
			if test.committed {
				wantCrashed = intendedOwnership
			}
			if !reflect.DeepEqual(crashedBytes, wantCrashed) {
				t.Fatalf("crashed ownership bytes = %q, want %q", crashedBytes, wantCrashed)
			}

			if err := Recover(context.Background(), stateDir); err != nil {
				t.Fatal(err)
			}
			assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
			assertFile(t, plan.Ownership.Path, string(intendedOwnership), 0o600)
			recoveredInfo, err := os.Lstat(plan.Ownership.Path)
			if err != nil {
				t.Fatal(err)
			}
			if test.committed && (!os.SameFile(crashedInfo, recoveredInfo) ||
				!crashedInfo.ModTime().Equal(recoveredInfo.ModTime())) {
				t.Fatalf("recovery rewrote committed ownership: before=%v after=%v", crashedInfo, recoveredInfo)
			}
			if pending := journals(t, stateDir); len(pending) != 0 {
				t.Fatalf("ownership recovery left journals: %v", pending)
			}

			if err := Recover(context.Background(), stateDir); err != nil {
				t.Fatalf("repeat recovery: %v", err)
			}
			assertFile(t, plan.Ownership.Path, string(intendedOwnership), 0o600)
			repeatedInfo, err := os.Lstat(plan.Ownership.Path)
			if err != nil ||
				!os.SameFile(recoveredInfo, repeatedInfo) ||
				!recoveredInfo.ModTime().Equal(repeatedInfo.ModTime()) {
				t.Fatalf("repeat recovery changed ownership: before=%v after=%v err=%v", recoveredInfo, repeatedInfo, err)
			}
			if pending := journals(t, stateDir); len(pending) != 0 {
				t.Fatalf("repeat recovery left journals: %v", pending)
			}
		})
	}
}

func TestPlaceCrashAfterDirectoryPruneRecovers(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	layoutPath := filepath.Join(root, "layouts")
	stalePath := filepath.Join(layoutPath, "stale.txt")
	staleBytes := []byte("stale package bytes\n")
	if err := os.Mkdir(layoutPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, staleBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	rootDir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity := rootDir.Identity()
	parent, err := rootDir.OpenPath("layouts")
	if err != nil {
		_ = rootDir.Close()
		t.Fatal(err)
	}
	parentIdentity := parent.Identity()
	if err := parent.Close(); err != nil {
		_ = rootDir.Close()
		t.Fatal(err)
	}
	if err := rootDir.Close(); err != nil {
		t.Fatal(err)
	}
	plan.Prunes = []PlacePrune{
		{
			Path: "layouts/stale.txt", Parent: parentIdentity,
			Expected: Generation{Exists: true, Digest: Digest(staleBytes)}, Mode: 0o644,
		},
		{Path: "layouts", Parent: rootIdentity, Directory: &parentIdentity},
	}
	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterPrune, 1)

	journalsBeforeRecovery := journals(t, stateDir)
	if len(journalsBeforeRecovery) != 1 {
		t.Fatalf("crashed directory prune journals = %v, want one", journalsBeforeRecovery)
	}
	journalPath := journalsBeforeRecovery[0]
	journal, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Place.Pending == nil || journal.Place.Pending.Path != "layouts" {
		t.Fatalf("crashed directory-prune operation = completed %d pending %+v", journal.Place.Completed, journal.Place.Pending)
	}
	if _, err := os.Lstat(stalePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crash did not prune stale file: %v", err)
	}
	if _, err := os.Lstat(layoutPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crash did not prune empty directory: %v", err)
	}
	assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)

	pending := CheckPending(stateDir)
	if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) ||
		strings.Contains(pending.Error(), "layouts/stale.txt") {
		t.Fatalf("CheckPending after directory prune = %v, want journal path without completed-file missing-parent failure", pending)
	}
	syncErr := errors.New("parent sync failed")
	if err := Recover(context.Background(), stateDir, func(o *options) {
		o.syncPruneParent = func(dir *fsboundary.Dir) error {
			if !dir.Identity().SameObject(rootIdentity) {
				t.Fatal("recovery synced the wrong prune parent")
			}
			return syncErr
		}
	}); !errors.Is(err, syncErr) {
		t.Fatalf("resume marked absent directory = %v, want parent sync failure", err)
	}
	retry, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Place.Completed != journal.Place.Completed || retry.Place.Pending == nil || *retry.Place.Pending != *journal.Place.Pending {
		t.Fatalf("sync failure advanced directory prune: completed %d pending %+v", retry.Place.Completed, retry.Place.Pending)
	}
	if err := Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover after directory prune: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("directory-prune recovery left journals: %v", got)
	}
	if err := Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("repeat recovery after directory prune: %v", err)
	}
	assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("repeat recovery left journals: %v", got)
	}
}

func TestPlaceCrashNoOpOwnershipGenerationRecovers(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	if err := os.Mkdir(filepath.Join(root, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan.Directories = nil
	ownershipBytes, err := json.Marshal(map[string]any{
		"canonical_root":    root,
		"place_directories": nil,
		"root_key":          testPlaceRootKey(root),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.Ownership.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Ownership.Path, ownershipBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	plan.Ownership.Bytes = ownershipBytes
	plan.Ownership.Expected = Generation{Exists: true, Digest: Digest(ownershipBytes)}
	plan.Ownership.ExpectedMode = 0o600

	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterOwnership, 0)

	payload := filepath.Join(root, "mods", "new.txt")
	assertFile(t, payload, "new package bytes\n", 0o644)
	if got, err := os.ReadFile(plan.Ownership.Path); err != nil || !reflect.DeepEqual(got, ownershipBytes) {
		t.Fatalf("crash changed no-op ownership bytes: equal=%t err=%v", reflect.DeepEqual(got, ownershipBytes), err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("crash discarded ownership journal: %v", got)
	}
	noSync := func(*fsboundary.Dir) error { return ErrRecoveryBlocked }
	if err := Recover(context.Background(), stateDir, func(o *options) { o.syncPruneParent = noSync }); err != nil {
		t.Fatalf("recover no-op ownership: %v", err)
	}
	assertFile(t, payload, "new package bytes\n", 0o644)
	if got, err := os.ReadFile(plan.Ownership.Path); err != nil || !reflect.DeepEqual(got, ownershipBytes) {
		t.Fatalf("recovery changed no-op ownership bytes: equal=%t err=%v", reflect.DeepEqual(got, ownershipBytes), err)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("recovery left ownership journal: %v", got)
	}
}

// spec: target-profile-place/marked-file-mutation-retries-parent-sync
func TestPlaceCommitGenuineNoOpsDoNotSync(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	if err := os.MkdirAll(filepath.Join(root, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "mods", "new.txt")
	want := []byte("new package bytes\n")
	if err := os.WriteFile(payload, want, plan.Writes[0].Mode); err != nil {
		t.Fatal(err)
	}
	plan.Writes[0].Expected, plan.Writes[0].ExpectedMode = Generation{Exists: true, Digest: Digest(want)}, plan.Writes[0].Mode
	if err := os.MkdirAll(filepath.Dir(plan.Ownership.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Ownership.Path, plan.Ownership.Bytes, plan.Ownership.Mode); err != nil {
		t.Fatal(err)
	}
	plan.Ownership.Expected, plan.Ownership.ExpectedMode = Generation{Exists: true, Digest: Digest(plan.Ownership.Bytes)}, plan.Ownership.Mode
	before, err := os.Lstat(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CommitPlace(context.Background(), stateDir, plan, func(o *options) {
		o.syncPruneParent = func(*fsboundary.Dir) error {
			t.Fatal("genuine no-op requested a parent sync")
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(payload)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("no-op regular file identity changed: after=%v err=%v", after, err)
	}
}

// spec: target-profile-place/marked-file-mutation-retries-parent-sync
func TestPlaceCrashMarkedVisibleMutationSyncFailureRetries(t *testing.T) {
	for _, kind := range []string{"write", "ownership", "prune"} {
		t.Run(kind, func(t *testing.T) {
			root, stateDir, plan := newPlaceFixture(t, kind == "ownership")
			var crashStage FaultStage
			var targetPath, parentPath string
			var wantBytes []byte
			var wantMode os.FileMode
			var err error
			switch kind {
			case "write":
				crashStage = FaultPlaceAfterPublish
				targetPath = filepath.Join(root, "mods", "new.txt")
				parentPath = filepath.Dir(targetPath)
				wantBytes, wantMode = []byte("new package bytes\n"), 0o644
			case "ownership":
				crashStage = FaultPlaceAfterOwnership
				targetPath = plan.Ownership.Path
				parentPath = filepath.Dir(targetPath)
				if err := os.Mkdir(filepath.Join(root, "mods"), 0o755); err != nil {
					t.Fatal(err)
				}
				plan.Directories = nil
				plan.Ownership.Bytes = []byte(fmt.Sprintf(`{"canonical_root":%q,"fixture_marker":"committed","place_directories":[],"root_key":%q}`, root, testPlaceRootKey(root)))
				wantBytes, wantMode = plan.Ownership.Bytes, 0o600
			case "prune":
				crashStage = FaultPlaceAfterPrune
				targetPath = filepath.Join(root, "layouts", "stale.txt")
				parentPath = filepath.Dir(targetPath)
				if err := os.MkdirAll(parentPath, 0o755); err != nil {
					t.Fatal(err)
				}
				stale := []byte("stale package bytes\n")
				if err := os.WriteFile(targetPath, stale, 0o644); err != nil {
					t.Fatal(err)
				}
				parent, err := fsboundary.OpenDir(parentPath)
				if err != nil {
					t.Fatal(err)
				}
				plan.Prunes = []PlacePrune{{
					Path: "layouts/stale.txt", Parent: parent.Identity(),
					Expected: Generation{Exists: true, Digest: Digest(stale)}, Mode: 0o644,
				}}
				if err := parent.Close(); err != nil {
					t.Fatal(err)
				}
			}

			runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, crashStage, 0)
			crashedJournals := journals(t, stateDir)
			if len(crashedJournals) != 1 {
				t.Fatalf("crashed journals = %v, want one", crashedJournals)
			}
			journalPath := crashedJournals[0]
			crashed, err := readJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if crashed.Place.Pending == nil || crashed.Place.Pending.Kind != kind {
				t.Fatalf("crashed journal has no pending operation: %+v", crashed.Place)
			}
			expectedParent, err := fsboundary.OpenDir(parentPath)
			if err != nil {
				t.Fatal(err)
			}
			expectedParentIdentity := expectedParent.Identity()
			if err := expectedParent.Close(); err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			var beforeInfo os.FileInfo
			if kind != "prune" {
				beforeInfo, err = os.Lstat(targetPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			assertVisible := func() {
				t.Helper()
				if kind == "prune" {
					if _, err := os.Lstat(targetPath); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("marked prune result changed: %v", err)
					}
					return
				}
				info, err := os.Lstat(targetPath)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(beforeInfo, info) {
					t.Fatalf("visible mutation replaced file: before=%v after=%v", beforeInfo, info)
				}
				assertFile(t, targetPath, string(wantBytes), wantMode)
			}

			if pending := CheckPending(stateDir); !errors.Is(pending, ErrRecoveryRequired) {
				t.Fatal(pending)
			}
			afterCheck, err := os.ReadFile(journalPath)
			if err != nil || !reflect.DeepEqual(afterCheck, beforeJournal) {
				t.Fatalf("CheckPending changed journal bytes: err=%v", err)
			}
			assertVisible()

			syncErr := errors.New("parent sync failed")
			syncCalls := 0
			failSync := true
			syncParent := func(dir *fsboundary.Dir) error {
				syncCalls++
				if !dir.Identity().SameObject(expectedParentIdentity) {
					return errors.New("recovery synced the wrong parent")
				}
				if failSync {
					return syncErr
				}
				return dir.Sync()
			}
			configureSync := func(o *options) { o.syncPruneParent = syncParent }
			err = Recover(context.Background(), stateDir, configureSync)
			if !errors.Is(err, syncErr) || !errors.Is(err, ErrRecoveryBlocked) ||
				!strings.Contains(err.Error(), journalPath) || !strings.Contains(err.Error(), crashed.Place.Pending.Path) || syncCalls != 1 {
				t.Fatalf("recovery with failed parent sync = %v after %d sync calls", err, syncCalls)
			}
			retryState, err := readJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if retryState.Place.Completed != crashed.Place.Completed ||
				!reflect.DeepEqual(retryState.Place.Pending, crashed.Place.Pending) {
				t.Fatalf("sync failure advanced recovery: before=%+v after=%+v", crashed.Place, retryState.Place)
			}
			assertVisible()

			syncCalls = 0
			failSync = false
			err = Recover(context.Background(), stateDir, configureSync)
			if err != nil || syncCalls != 1 {
				t.Fatalf("recovery retry = %v after %d sync calls", err, syncCalls)
			}
			assertVisible()
			if got := journals(t, stateDir); len(got) != 0 {
				t.Fatalf("journal remains after retry: %v", got)
			}
		})
	}
}

// spec: target-profile-place/a-partially-published-prefix-completes-exactly
func TestPlaceCrashRecreatesMissingMarkedDirectory(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	second := plan.Writes[0]
	second.Path = "layouts/second.txt"
	plan.Writes = append(plan.Writes, second)
	plan.Directories = []string{"mods", "layouts"}

	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterDirectory, 1)
	j, err := readJournal(journals(t, stateDir)[0])
	if err != nil {
		t.Fatal(err)
	}
	if j.Place.Completed == 0 || j.Place.Pending == nil || j.Place.Pending.Path != "layouts" ||
		j.Place.Directories[1].Identity == nil {
		t.Fatalf("crashed directory operation = completed %d, pending %+v, identity %+v",
			j.Place.Completed, j.Place.Pending, j.Place.Directories[1].Identity)
	}
	oldIdentity := *j.Place.Directories[1].Identity
	markedPath := filepath.Join(root, "layouts")
	asidePath := filepath.Join(filepath.Dir(root), "layouts-held")
	if err := os.Rename(markedPath, asidePath); err != nil {
		t.Fatal(err)
	}

	if err := Recover(context.Background(), stateDir); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
	assertFile(t, filepath.Join(root, "layouts", "second.txt"), "new package bytes\n", 0o644)
	newDirectory, err := fsboundary.OpenDir(markedPath)
	if err != nil {
		t.Fatal(err)
	}
	newIdentity := newDirectory.Identity()
	if err := newDirectory.Close(); err != nil {
		t.Fatal(err)
	}
	if newIdentity.SameObject(oldIdentity) {
		t.Fatal("recreated directory reused the retained directory identity")
	}
	raw, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	var ownership struct {
		Directories []PlaceDirectory `json:"place_directories"`
	}
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	var recorded *PlaceDirectory
	for i := range ownership.Directories {
		if ownership.Directories[i].Path == "layouts" {
			recorded = &ownership.Directories[i]
			break
		}
	}
	if recorded == nil || !recorded.Identity.SameObject(newIdentity) {
		t.Fatalf("ownership directories = %+v, want recreated layouts identity %+v", ownership.Directories, newIdentity)
	}
	if pending := journals(t, stateDir); len(pending) != 0 {
		t.Fatalf("recovery left journals: %v", pending)
	}
}

// spec: target-profile-place/a-recovery-stop-names-its-paths
func TestPlaceCrashUnmarkedIntendedFileIsNotAdopted(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, true)
	priorOwnership, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	second := plan.Writes[0]
	second.Path = "mods/second.txt"
	plan.Writes = append(plan.Writes, second)
	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterPublish, 0)

	secondPath := filepath.Join(root, "mods", "second.txt")
	if err := os.WriteFile(secondPath, []byte("new package bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := journals(t, stateDir)[0]
	assertRefusal := func() {
		t.Helper()
		err := Recover(context.Background(), stateDir)
		if !errors.Is(err, ErrRecoveryBlocked) || !strings.Contains(err.Error(), journalPath) ||
			!strings.Contains(err.Error(), "mods/second.txt") {
			t.Fatalf("recovery adopted an unmarked intended file: %v", err)
		}
		pending := CheckPending(stateDir)
		if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) ||
			!strings.Contains(pending.Error(), "mods/second.txt") {
			t.Fatalf("pending check did not name the unmarked intended file: %v", pending)
		}
		assertFile(t, secondPath, "new package bytes\n", 0o644)
		assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
		ownership, err := os.ReadFile(plan.Ownership.Path)
		if err != nil || !reflect.DeepEqual(ownership, priorOwnership) {
			t.Fatalf("recovery changed ownership: bytes equal prior=%t err=%v", reflect.DeepEqual(ownership, priorOwnership), err)
		}
		if got := journals(t, stateDir); len(got) != 1 || got[0] != journalPath {
			t.Fatalf("recovery discarded journal evidence: %v", got)
		}
	}

	assertRefusal()
	afterFirst, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterFirst.Place.Completed != 2 || afterFirst.Place.Pending != nil {
		t.Fatalf("first refusal advanced past the validated prefix: completed=%d pending=%+v, want completed=2 and no pending operation",
			afterFirst.Place.Completed, afterFirst.Place.Pending)
	}

	assertRefusal()
	afterSecond, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterSecond.Place.Completed != afterFirst.Place.Completed ||
		!reflect.DeepEqual(afterSecond.Place.Pending, afterFirst.Place.Pending) {
		t.Fatalf("second refusal changed journal progress: first completed/pending=%d/%+v, second=%d/%+v",
			afterFirst.Place.Completed, afterFirst.Place.Pending, afterSecond.Place.Completed, afterSecond.Place.Pending)
	}
}

func TestPlaceCrashChild(t *testing.T) {
	if os.Getenv(placeCrashChildEnv) != "1" {
		t.Skip("subprocess-only crash witness")
	}
	raw, err := os.ReadFile(os.Getenv(placeCrashPlanEnv))
	if err != nil {
		t.Fatal(err)
	}
	var plan PlacePlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Dir(filepath.Dir(filepath.Dir(plan.Ownership.Path)))
	if _, err := CommitPlace(context.Background(), filepath.Join(stateRoot, "dependencies"), plan); err != nil {
		t.Fatal(err)
	}
	t.Fatal("configured place crash point was not reached")
}

// spec: target-profile-place/prune-recovery-treats-marked-and-unmarked-absences-differently
func TestPlaceCrashPruneRefusesUnmarkedAbsence(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	prunePaths := []string{
		filepath.Join(root, "layouts", "first.txt"),
		filepath.Join(root, "layouts", "second.txt"),
	}
	if err := os.MkdirAll(filepath.Dir(prunePaths[0]), 0o755); err != nil {
		t.Fatal(err)
	}
	var parent fsboundary.Identity
	dir, err := fsboundary.OpenDir(filepath.Dir(prunePaths[0]))
	if err != nil {
		t.Fatal(err)
	}
	parent = dir.Identity()
	if err := dir.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range prunePaths {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatal(err)
		}
		plan.Prunes = append(plan.Prunes, PlacePrune{
			Path: filepath.Join("layouts", filepath.Base(path)), Parent: parent,
			Expected: Generation{Exists: true, Digest: Digest([]byte(filepath.Base(path)))},
			Mode:     0o644,
		})
	}
	planFile := filepath.Join(t.TempDir(), "place-plan.json")
	runPlaceCrashChild(t, planFile, plan, FaultPlaceAfterPrune, 0)
	if err := os.Remove(prunePaths[1]); err != nil {
		t.Fatal(err)
	}
	err = Recover(context.Background(), stateDir)
	if err == nil || !strings.Contains(err.Error(), "second.txt") || !strings.Contains(err.Error(), ".dependencytxn-") {
		t.Fatalf("recovery after an unmarked absence = %v", err)
	}
	if _, err := os.Stat(prunePaths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marked removal was not preserved: %v", err)
	}
	if _, err := os.Lstat(journals(t, stateDir)[0]); err != nil {
		t.Fatalf("recovery discarded journal evidence: %v", err)
	}
}

// spec: target-profile-place/incomplete-staging-is-cleared-never-resumed
func TestPlaceCrashBeforeFirstWriteClearsOnlyExactStages(t *testing.T) {
	for _, point := range []FaultStage{FaultPlaceAfterStage, FaultPlaceBeforePublish} {
		for _, changed := range []bool{false, true} {
			phase := "exact"
			if changed {
				phase = "changed"
			}
			t.Run(string(point)+"/"+phase, func(t *testing.T) {
				root, stateDir, plan := newPlaceFixture(t, false)
				if err := os.Mkdir(filepath.Join(root, "mods"), 0o755); err != nil {
					t.Fatal(err)
				}
				plan.Directories = nil
				second := plan.Writes[0]
				second.Path = "mods/second.txt"
				plan.Writes = append(plan.Writes, second)
				runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, point, 0)

				pending := journals(t, stateDir)
				if len(pending) != 1 {
					t.Fatalf("crash journals = %v, want one", pending)
				}
				journalPath := pending[0]
				j, err := readJournal(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				stagePath := filepath.Join(filepath.Dir(stateDir), filepath.FromSlash(j.Place.Stage))
				firstStage := filepath.Join(stagePath, j.Place.Writes[0].StageName)
				secondStage := filepath.Join(stagePath, j.Place.Writes[1].StageName)
				assertFile(t, firstStage, "new package bytes\n", 0o644)
				if point == FaultPlaceAfterStage {
					if _, err := os.Lstat(secondStage); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("after-stage crash staged the second write: %v", err)
					}
				} else {
					assertFile(t, secondStage, "new package bytes\n", 0o644)
				}
				if changed {
					if err := os.WriteFile(firstStage, []byte("foreign bytes"), 0o644); err != nil {
						t.Fatal(err)
					}
				}

				pendingErr := CheckPending(stateDir)
				if !errors.Is(pendingErr, ErrRecoveryRequired) {
					t.Fatalf("CheckPending = %v, want ErrRecoveryRequired", pendingErr)
				}
				if changed && (!strings.Contains(pendingErr.Error(), journalPath) ||
					!strings.Contains(pendingErr.Error(), j.Place.Stage)) {
					t.Fatalf("CheckPending failed to name changed stage: %v", pendingErr)
				}
				recoveryErr := Recover(context.Background(), stateDir)
				if changed {
					if recoveryErr == nil || !strings.Contains(recoveryErr.Error(), journalPath) ||
						!strings.Contains(recoveryErr.Error(), j.Place.Stage) {
						t.Fatalf("recovery failed to name changed stage: %v", recoveryErr)
					}
					assertFile(t, firstStage, "foreign bytes", 0o644)
					if got := journals(t, stateDir); len(got) != 1 || got[0] != journalPath {
						t.Fatalf("recovery discarded changed-stage evidence: %v", got)
					}
				} else {
					if recoveryErr != nil {
						t.Fatalf("recovery of exact stage: %v", recoveryErr)
					}
					if _, err := os.Lstat(stagePath); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("recovery retained exact stage: %v", err)
					}
					if got := journals(t, stateDir); len(got) != 0 {
						t.Fatalf("recovery retained exact-stage journal: %v", got)
					}
					if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("exact incomplete staging wrote ownership: %v", err)
					}
				}
				for _, destination := range []string{
					filepath.Join(root, "mods", "new.txt"),
					filepath.Join(root, "mods", "second.txt"),
				} {
					if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("incomplete staging published %s: %v", destination, err)
					}
				}
			})
		}
	}
}

// spec: target-profile-place/an-unrecorded-present-directory-stops-recovery
func TestPlaceCrashPresentDirectoryWithoutIdentityStops(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterDirectory, 0)
	journalPath := journals(t, stateDir)[0]
	j, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	j.Place.Directories[0].Identity = nil
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pending := CheckPending(stateDir)
	if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) ||
		!strings.Contains(pending.Error(), "mods") {
		t.Fatalf("CheckPending failed to name unrecorded directory: %v", pending)
	}
	err = Recover(context.Background(), stateDir)
	if err == nil || !strings.Contains(err.Error(), journalPath) || !strings.Contains(err.Error(), "mods") {
		t.Fatalf("recovery accepted unrecorded directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "mods")); err != nil {
		t.Fatalf("unrecorded directory was removed: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("recovery discarded journal: %v", got)
	}
}

// spec: target-profile-place/a-recovery-stop-names-its-paths
func TestPlaceCrashEditedPublishedPrefixStopsBeforeSuffix(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	second := plan.Writes[0]
	second.Path = "mods/second.txt"
	plan.Writes = append(plan.Writes, second)
	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterPublish, 0)
	journalPath := journals(t, stateDir)[0]
	firstPath := filepath.Join(root, "mods", "new.txt")
	if err := os.WriteFile(firstPath, []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := CheckPending(stateDir)
	if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) ||
		!strings.Contains(pending.Error(), "mods/new.txt") {
		t.Fatalf("CheckPending failed to name changed prefix: %v", pending)
	}
	err := Recover(context.Background(), stateDir)
	if err == nil || !strings.Contains(err.Error(), journalPath) || !strings.Contains(err.Error(), "mods/new.txt") {
		t.Fatalf("recovery continued through edited prefix: %v", err)
	}
	assertFile(t, firstPath, "user edit", 0o644)
	if _, err := os.Lstat(filepath.Join(root, "mods", "second.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery published a later file after the stop: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("recovery discarded journal: %v", got)
	}
}

// spec: target-profile-place/a-recovery-stop-names-its-paths
func TestPlaceCrashStatusNamesReplacedRoot(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	runPlaceCrashChild(t, filepath.Join(t.TempDir(), "place-plan.json"), plan, FaultPlaceAfterPublish, 0)
	journalPath := journals(t, stateDir)[0]
	j, err := readJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if j.Place.StageIdentity == nil {
		t.Fatal("crash did not record the stage identity")
	}
	stagePath := filepath.Join(filepath.Dir(stateDir), filepath.FromSlash(j.Place.Stage))
	stageIdentity := *j.Place.StageIdentity
	savedRoot := filepath.Join(filepath.Dir(root), "application-saved")
	if err := os.Rename(root, savedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(savedRoot, root); err != nil {
		t.Fatal(err)
	}

	pending := CheckPending(stateDir)
	if !errors.Is(pending, ErrRecoveryRequired) || !strings.Contains(pending.Error(), journalPath) ||
		!strings.Contains(pending.Error(), root) {
		t.Fatalf("CheckPending did not name the replaced root and journal: %v", pending)
	}
	if err := Recover(context.Background(), stateDir); err == nil ||
		!strings.Contains(err.Error(), journalPath) || !strings.Contains(err.Error(), root) {
		t.Fatalf("Recover did not refuse with the replaced root and journal: %v", err)
	}
	assertFile(t, filepath.Join(savedRoot, "mods", "new.txt"), "new package bytes\n", 0o644)
	if entries, err := os.ReadDir(filepath.Join(savedRoot, "mods")); err != nil || len(entries) != 1 || entries[0].Name() != "new.txt" {
		t.Fatalf("saved target tree changed after refusal: %v / %v", entries, err)
	}
	if got, err := os.ReadFile(journalPath); err != nil || !reflect.DeepEqual(got, journalBytes) {
		t.Fatalf("recovery changed journal evidence: equal=%t err=%v", reflect.DeepEqual(got, journalBytes), err)
	}
	stage, err := fsboundary.OpenDir(stagePath)
	if err != nil {
		t.Fatalf("recovery removed the stage evidence: %v", err)
	}
	if !stage.Identity().SameObject(stageIdentity) {
		_ = stage.Close()
		t.Fatal("recovery replaced the stage evidence")
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if got := journals(t, stateDir); len(got) != 1 || got[0] != journalPath {
		t.Fatalf("recovery cleared journal evidence: %v", got)
	}
}

func runPlaceCrashChild(t *testing.T, planFile string, plan PlacePlan, stage FaultStage, index int) {
	t.Helper()
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planFile, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlaceCrashChild$")
	cmd.Env = append(os.Environ(),
		placeCrashChildEnv+"=1",
		placeCrashPlanEnv+"="+planFile,
		fmt.Sprintf("AGENT_MARKETPLACE_E2E_CRASH_AT=%s:%d", stage, index),
	)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err == nil || !errors.As(err, &exit) || exit.ExitCode() != CrashExitCode {
		t.Fatalf("crash child error = %v, output %s", err, output)
	}
}
