package dependencytxn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type placeStateFixture struct {
	CanonicalRoot    string `json:"canonical_root"`
	RootKey          string `json:"root_key"`
	PlaceDirectories []any  `json:"place_directories"`
	FixtureMarker    string `json:"fixture_marker,omitempty"`
}

func newPlaceFixture(t *testing.T, oldState bool) (string, string, PlacePlan) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "application")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	rootDir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity := rootDir.Identity()
	if err := rootDir.Close(); err != nil {
		t.Fatal(err)
	}

	stateRoot := filepath.Join(base, "agent-marketplace")
	stateDir := filepath.Join(stateRoot, "dependencies")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	sourcePath := filepath.Join(stateRoot, "packages", key, "payload", "new.txt")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	source := []byte("new package bytes\n")
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourcePath, 0o644); err != nil {
		t.Fatal(err)
	}

	rootKey := testPlaceRootKey(root)
	ownershipPath := filepath.Join(stateRoot, "ownership", rootKey, "state.json")
	ownership := DesiredFile{
		Path: ownershipPath,
		Bytes: mustPlaceState(t, placeStateFixture{
			CanonicalRoot: root, RootKey: rootKey, PlaceDirectories: []any{},
		}),
		Mode: 0o600,
	}
	if oldState {
		if err := os.MkdirAll(filepath.Dir(ownershipPath), 0o700); err != nil {
			t.Fatal(err)
		}
		old := mustPlaceState(t, placeStateFixture{
			CanonicalRoot: root, RootKey: rootKey, PlaceDirectories: []any{},
		})
		if err := os.WriteFile(ownershipPath, old, 0o600); err != nil {
			t.Fatal(err)
		}
		ownership.Expected = Generation{Exists: true, Digest: Digest(old)}
		ownership.ExpectedMode = 0o600
	}

	plan := PlacePlan{
		Root:         root,
		RootIdentity: rootIdentity,
		Directories:  []string{"mods"},
		Writes: []PlaceWrite{{
			Path: "mods/new.txt", GenerationKey: key, SourcePath: "payload/new.txt",
			Expected: Generation{}, ExpectedMode: 0,
			Digest: Digest(source), Mode: 0o644, Size: int64(len(source)),
		}},
		Ownership: ownership,
	}
	return root, stateDir, plan
}

func TestPlaceCommitRefusesChangedSelectionBeforeJournal(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, true)
	inputPath := filepath.Join(filepath.Dir(root), "deployed-config.yaml")
	beforeInput := []byte("selection: original\n")
	if err := os.WriteFile(inputPath, beforeInput, 0o600); err != nil {
		t.Fatal(err)
	}
	selection := ReadAssertion{
		Path: inputPath, RequestedPath: inputPath, Expected: generation(t, inputPath),
	}
	ownershipBefore, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CommitPlace(
		context.Background(), stateDir, plan,
		WithReadAssertions([]ReadAssertion{selection}),
		WithFaultHook(func(point FaultPoint) error {
			if point.Stage == FaultAfterLocks {
				return os.WriteFile(inputPath, []byte("selection: changed\n"), 0o600)
			}
			return nil
		}),
	)
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("CommitPlace with changed selection error = %v, want generation conflict", err)
	}
	if got, err := os.ReadFile(inputPath); err != nil || string(got) != "selection: changed\n" {
		t.Fatalf("fault did not change selection input: %q, %v", got, err)
	}
	targetPath := filepath.Join(root, "mods", "new.txt")
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("place target changed before journal: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(targetPath)); !os.IsNotExist(err) {
		t.Fatalf("place directory changed before journal: %v", err)
	}
	if got, err := os.ReadFile(plan.Ownership.Path); err != nil || !reflect.DeepEqual(got, ownershipBefore) {
		t.Fatalf("ownership changed before journal: %v", err)
	}
	if err := CheckPending(stateDir); err != nil {
		t.Fatalf("changed selection left a transaction journal: %v", err)
	}
}

// spec: target-profile-place/staging-happens-outside-the-target-root
func TestPlaceRejectsTargetRootsOverlappingState(t *testing.T) {
	for _, test := range []struct {
		name string
		root func(base, stateRoot string) string
	}{
		{name: "contains-state-root", root: func(base, _ string) string { return base }},
		{name: "inside-place-stage", root: func(_, stateRoot string) string {
			return filepath.Join(stateRoot, "place-stage", "target")
		}},
		{name: "inside-packages", root: func(_, stateRoot string) string {
			return filepath.Join(stateRoot, "packages", "target")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, stateDir, plan := newPlaceFixture(t, false)
			stateRoot := filepath.Dir(stateDir)
			base := filepath.Dir(stateRoot)
			root := test.root(base, stateRoot)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			rootIdentity, err := fsboundary.OpenDir(root)
			if err != nil {
				t.Fatal(err)
			}
			plan.Root = root
			plan.RootIdentity = rootIdentity.Identity()
			_ = rootIdentity.Close()
			plan.Writes = nil
			plan.Directories = nil
			plan.Prunes = nil
			rootKey := testPlaceRootKey(root)
			plan.Ownership = DesiredFile{
				Path: filepath.Join(stateRoot, "ownership", rootKey, "state.json"),
				Bytes: mustPlaceState(t, placeStateFixture{
					CanonicalRoot: root, RootKey: rootKey, PlaceDirectories: []any{},
				}),
				Mode: 0o600,
			}
			sentinel := filepath.Join(root, "unchanged.txt")
			sentinelBytes := []byte("keep existing target bytes\n")
			if err := os.WriteFile(sentinel, sentinelBytes, 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := CommitPlace(context.Background(), stateDir, plan); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("overlapping target root error = %v, want ErrUnsafePath", err)
			}
			if got, err := os.ReadFile(sentinel); err != nil || !reflect.DeepEqual(got, sentinelBytes) {
				t.Fatalf("target sentinel after refusal = %q / %v", got, err)
			}
			if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("overlapping-root refusal wrote ownership: %v", err)
			}
			if entries, err := os.ReadDir(stateDir); err != nil || len(entries) != 0 {
				t.Fatalf("overlapping-root refusal left journals: %v / %v", entries, err)
			}
		})
	}

	root, _, plan := newPlaceFixture(t, false)
	stateDir := filepath.Join(root, "new-state", "dependencies")
	if _, err := CommitPlace(context.Background(), stateDir, plan); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("overlapping target root with nonexistent state directory error = %v, want ErrUnsafePath", err)
	}
	if _, err := os.Lstat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("overlapping-root refusal created state directory: %v", err)
	}
}

func mustPlaceState(t *testing.T, state placeStateFixture) []byte {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testPlaceRootKey(root string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("agent-marketplace/ownership/root/v1\x00%d\x00%s", len(root), root)))
	return hex.EncodeToString(sum[:])
}

func TestPlaceCommitPublishesGenerationAndOwnership(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	plan.Ownership.Mode = 0o640
	result, err := CommitPlace(context.Background(), stateDir, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Changed, []string{"mods/new.txt"}) || len(result.Retained) != 0 {
		t.Fatalf("CommitPlace result = %#v", result)
	}
	assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
	ownership, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	var got placeStateFixture
	if err := json.Unmarshal(ownership, &got); err != nil {
		t.Fatal(err)
	}
	if got.CanonicalRoot != root || got.RootKey != testPlaceRootKey(root) || len(got.PlaceDirectories) != 1 {
		t.Fatalf("ownership record = %#v", got)
	}
	info, err := os.Stat(plan.Ownership.Path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("ownership mode = %v, %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "place-stage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage appeared in target root: %v", err)
	}
	keys, err := ReadPendingPlaceGenerationKeys(stateDir)
	if err != nil || len(keys) != 0 {
		t.Fatalf("pending generation keys = %v, %v", keys, err)
	}
}

func TestPlaceCommitsOwnershipWithoutTargetOperations(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	editedPath := filepath.Join(root, "edited.txt")
	editedBytes := []byte("user edit\n")
	if err := os.WriteFile(editedPath, editedBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	plan.Writes = nil
	plan.Directories = nil
	plan.Prunes = nil
	plan.Ownership.Bytes = mustPlaceState(t, placeStateFixture{
		CanonicalRoot: plan.Root, RootKey: testPlaceRootKey(plan.Root),
		PlaceDirectories: []any{}, FixtureMarker: "ownership-only",
	})
	if _, err := CommitPlace(context.Background(), stateDir, plan); err != nil {
		t.Fatal(err)
	}
	assertFile(t, editedPath, string(editedBytes), 0o600)
	raw, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["fixture_marker"] != "ownership-only" {
		t.Fatalf("committed ownership fixture marker = %v", got["fixture_marker"])
	}
	if pending := journals(t, stateDir); len(pending) != 0 {
		t.Fatalf("ownership-only transaction left journals: %v", pending)
	}
}

func TestPlaceConflictingSourceExpectationsRefuseBeforeStage(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	second := plan.Writes[0]
	second.Path = "mods/second.txt"
	second.Digest = Digest([]byte("different expected bytes\n"))
	plan.Writes = append(plan.Writes, second)

	_, commitErr := CommitPlace(context.Background(), stateDir, plan)
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("conflicting source expectations left journals: %v", got)
	}
	for _, rel := range []string{"mods/new.txt", "mods/second.txt"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("conflicting source expectations published %s: %v", rel, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(stateDir), "place-stage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conflicting source expectations created staging: %v", err)
	}
	if !errors.Is(commitErr, ErrGenerationConflict) {
		t.Fatalf("conflicting source expectations were not rejected: %v", commitErr)
	}
}

func TestPlaceRefusesChangedStageModeBeforePublishing(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	var stageFile string
	_, commitErr := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultPlaceAfterStage {
			return nil
		}
		stageRoot := filepath.Join(filepath.Dir(stateDir), "place-stage")
		entries, err := os.ReadDir(stageRoot)
		if err != nil {
			return err
		}
		if len(entries) != 1 {
			return fmt.Errorf("place stage entries = %v, want one scratch stage", entries)
		}
		stagePath := filepath.Join(stageRoot, entries[0].Name())
		stageEntries, err := os.ReadDir(stagePath)
		if err != nil {
			return err
		}
		if len(stageEntries) != 1 {
			return fmt.Errorf("scratch stage entries = %v, want one staged write", stageEntries)
		}
		stageFile = filepath.Join(stagePath, stageEntries[0].Name())
		return os.Chmod(stageFile, 0o600)
	}))
	if commitErr == nil {
		t.Fatal("CommitPlace accepted a stage file with a changed mode")
	}
	if stageFile == "" {
		t.Fatal("after-stage fault hook did not capture the scratch stage")
	}
	assertFile(t, stageFile, "new package bytes\n", 0o600)
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("changed stage mode discarded journal: %v", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods", "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed stage mode created destination: %v", err)
	}
	if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed stage mode created ownership record: %v", err)
	}
}

// spec: target-profile-place/oversized-journal-refuses-before-staging
func TestPlaceOversizedJournalRefusesBeforeStage(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	write := plan.Writes[0]
	plan.Writes = make([]PlaceWrite, 18000)
	for i := range plan.Writes {
		plan.Writes[i] = write
		plan.Writes[i].Path = fmt.Sprintf("mods/file-%05d.txt", i)
	}

	if _, err := CommitPlace(context.Background(), stateDir, plan); !errors.Is(err, ErrRecoveryBlocked) || !strings.Contains(err.Error(), "fully marked place journal requires") {
		t.Fatalf("oversized place commit = %v, want fully marked journal size refusal", err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(stateDir), "place-stage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized plan created stage: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized plan created target directory: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("oversized plan left journals: %v", got)
	}
}

// spec: target-profile-place/a-replaced-root-refuses-publish-and-prune
func TestPlaceRejectsReplacedRootBeforePublish(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	_, err := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultPlaceBeforePublish {
			return nil
		}
		backup := root + ".old"
		if renameErr := os.Rename(root, backup); renameErr != nil {
			return renameErr
		}
		return os.Mkdir(root, 0o755)
	}))
	if err == nil || !errors.Is(err, ErrGenerationConflict) || !strings.Contains(err.Error(), "mods/new.txt") {
		t.Fatalf("CommitPlace after root replacement = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods", "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root received a write: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("root replacement lost journal: %v", got)
	}
}

// spec: target-profile-place/a-replaced-root-refuses-publish-and-prune
func TestPlaceRejectsReplacedRootBeforePrune(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	staleBytes := []byte("stale package bytes\n")
	stalePath := filepath.Join(root, "stale.txt")
	if err := os.WriteFile(stalePath, staleBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	plan.Prunes = []PlacePrune{{
		Path: "stale.txt", Parent: plan.RootIdentity,
		Expected: Generation{Exists: true, Digest: Digest(staleBytes)}, Mode: 0o644,
	}}
	replacement := root + ".old"
	_, err := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultPlaceAfterOwnership {
			return nil
		}
		if err := os.Rename(root, replacement); err != nil {
			return err
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "stale.txt"), staleBytes, 0o644)
	}))
	if err == nil || !errors.Is(err, ErrGenerationConflict) || !strings.Contains(err.Error(), "stale.txt") {
		t.Fatalf("CommitPlace after root replacement before prune = %v", err)
	}
	assertFile(t, filepath.Join(replacement, "stale.txt"), string(staleBytes), 0o644)
	assertFile(t, filepath.Join(root, "stale.txt"), string(staleBytes), 0o644)
	if _, err := os.Lstat(filepath.Join(root, "mods", "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root received a published write: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("root replacement before prune lost journal: %v", got)
	}
}

func TestPlaceWriteParentProofBlocksReplacementAndRecovery(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	parentPath := filepath.Join(root, "mods")
	if err := os.Mkdir(parentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rootDir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	parentDir, err := rootDir.OpenPath("mods")
	if err != nil {
		_ = rootDir.Close()
		t.Fatal(err)
	}
	parentIdentity := parentDir.Identity()
	if err := parentDir.Close(); err != nil {
		_ = rootDir.Close()
		t.Fatal(err)
	}
	if err := rootDir.Close(); err != nil {
		t.Fatal(err)
	}
	plan.Writes[0].Parent = &parentIdentity
	var replacementPath string
	_, commitErr := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultPlaceBeforePublish || point.Path != "mods/new.txt" {
			return nil
		}
		if err := os.Rename(parentPath, parentPath+".old"); err != nil {
			return err
		}
		if err := os.Mkdir(parentPath, 0o755); err != nil {
			return err
		}
		replacementPath = filepath.Join(parentPath, "new.txt")
		return os.WriteFile(replacementPath, []byte("replacement owner\n"), 0o600)
	}))
	if commitErr == nil || !errors.Is(commitErr, ErrRecoveryBlocked) {
		t.Fatalf("CommitPlace with replaced parent = %v", commitErr)
	}
	if replacementPath == "" {
		t.Fatal("before-publish fault hook did not replace the parent")
	}
	assertFile(t, replacementPath, "replacement owner\n", 0o600)
	if _, err := os.Lstat(filepath.Join(parentPath+".old", "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replaced directory received a write: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("parent replacement did not retain the transaction journal: %v", got)
	}
	if err := Recover(context.Background(), stateDir); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("recovery with replaced pre-existing parent = %v", err)
	}
	assertFile(t, replacementPath, "replacement owner\n", 0o600)
	if got := journals(t, stateDir); len(got) != 1 {
		t.Fatalf("blocked recovery changed journal set: %v", got)
	}
}

func TestPlaceParentCreatedRejectsDirectoryAppearingAfterPlanning(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	plan.Writes[0].ParentCreated = true
	parentPath := filepath.Join(root, "mods")
	created := false
	_, err := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		if point.Stage != FaultAfterLocks {
			return nil
		}
		created = true
		return os.Mkdir(parentPath, 0o755)
	}))
	if !created {
		t.Fatal("after-locks fault hook did not create the parent")
	}
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("CommitPlace after parent appeared = %v", err)
	}
	if info, statErr := os.Stat(parentPath); statErr != nil || !info.IsDir() {
		t.Fatalf("independently-created parent was removed: info=%v err=%v", info, statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(parentPath, "new.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("CommitPlace wrote into the independently-created parent: %v", statErr)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("parent appearance left a journal: %v", got)
	}
}

// spec: target-profile-place/a-missing-approved-root-refuses
func TestPlaceMissingRootRefusesBeforeStage(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	if err := os.Rename(root, root+".moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitPlace(context.Background(), stateDir, plan); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("place accepted a missing approved root: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("missing root created journal: %v", got)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(stateDir), "place-stage")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root created stage: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root was created: %v", err)
	}
}

func TestPlacePreservesPublishAndPruneOrder(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	second := plan.Writes[0]
	second.Path = "mods/second.txt"
	plan.Writes = append(plan.Writes, second)
	rootDir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity := rootDir.Identity()
	if err := rootDir.Close(); err != nil {
		t.Fatal(err)
	}
	prunePaths := []string{"z-layout", "a-mod"}
	for _, path := range prunePaths {
		content := []byte(path + "\n")
		if err := os.WriteFile(filepath.Join(root, path), content, 0o644); err != nil {
			t.Fatal(err)
		}
		plan.Prunes = append(plan.Prunes, PlacePrune{
			Path: path, Parent: rootIdentity,
			Expected: Generation{Exists: true, Digest: Digest(content)}, Mode: 0o644,
		})
	}
	var published, pruned []string
	result, err := CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
		switch point.Stage {
		case FaultPlaceAfterPublish:
			published = append(published, point.Path)
		case FaultPlaceAfterPrune:
			pruned = append(pruned, point.Path)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(published, []string{"mods/new.txt", "mods/second.txt"}) ||
		!reflect.DeepEqual(pruned, prunePaths) {
		t.Fatalf("publication/prune order = %v / %v", published, pruned)
	}
	if !reflect.DeepEqual(result.Changed, []string{"mods/new.txt", "mods/second.txt", "z-layout", "a-mod"}) {
		t.Fatalf("changed paths = %v", result.Changed)
	}
}

func TestPlaceDirectoryPruneRequiresRecordedIdentityAndEmptiness(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(fmt.Sprintf("nonempty=%t", nonempty), func(t *testing.T) {
			root, stateDir, plan := newPlaceFixture(t, false)
			pathname := filepath.Join(root, "old-layout")
			if err := os.Mkdir(pathname, 0o755); err != nil {
				t.Fatal(err)
			}
			if nonempty {
				if err := os.WriteFile(filepath.Join(pathname, "foreign.txt"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			parent, err := fsboundary.OpenDir(root)
			if err != nil {
				t.Fatal(err)
			}
			parentID := parent.Identity()
			if err := parent.Close(); err != nil {
				t.Fatal(err)
			}
			directory, err := fsboundary.OpenDir(pathname)
			if err != nil {
				t.Fatal(err)
			}
			directoryID := directory.Identity()
			if err := directory.Close(); err != nil {
				t.Fatal(err)
			}
			plan.Prunes = []PlacePrune{{
				Path: "old-layout", Parent: parentID, Directory: &directoryID,
			}}
			result, err := CommitPlace(context.Background(), stateDir, plan)
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Lstat(pathname)
			if nonempty {
				if statErr != nil || !reflect.DeepEqual(result.Retained, []string{"old-layout"}) {
					t.Fatalf("non-empty directory was not retained: stat=%v result=%#v", statErr, result)
				}
				if _, err := os.Stat(filepath.Join(pathname, "foreign.txt")); err != nil {
					t.Fatalf("foreign file was removed: %v", err)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("empty recorded directory remains: %v", statErr)
			}
		})
	}
}

// spec: target-profile-place/exdev-after-earlier-writes-stays-pending-without-fallback
func TestPlaceEXDEVStaysPendingWithoutCopyFallback(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	second := plan.Writes[0]
	second.Path = "mods/second.txt"
	plan.Writes = append(plan.Writes, second)
	failRename := WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultPlaceBeforePublish && point.Index == 1 {
			return syscall.EXDEV
		}
		return nil
	})
	_, err := CommitPlace(context.Background(), stateDir, plan, failRename)
	if !errors.Is(err, syscall.EXDEV) || !strings.Contains(err.Error(), ".dependencytxn-") {
		t.Fatalf("EXDEV place commit = %v", err)
	}
	assertFile(t, filepath.Join(root, "mods", "new.txt"), "new package bytes\n", 0o644)
	if _, err := os.Lstat(filepath.Join(root, "mods", "second.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("EXDEV fell back to copying: %v", err)
	}
	pending := journals(t, stateDir)
	if len(pending) != 1 {
		t.Fatalf("EXDEV discarded transaction evidence: %v", pending)
	}
	journal, err := readJournal(pending[0])
	if err != nil {
		t.Fatal(err)
	}
	stageFile := filepath.Join(filepath.Dir(stateDir), filepath.FromSlash(journal.Place.Stage), journal.Place.Writes[1].StageName)
	assertFile(t, stageFile, "new package bytes\n", 0o644)
	if err := Recover(context.Background(), stateDir, failRename); !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("recovery did not refuse the same EXDEV rename: %v", err)
	}
	assertFile(t, stageFile, "new package bytes\n", 0o644)
	if got := journals(t, stateDir); !reflect.DeepEqual(got, pending) {
		t.Fatalf("recovery refusal changed pending journals: before=%v after=%v", pending, got)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods", "second.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery used a copy fallback: %v", err)
	}
}

// spec: target-profile-place/a-root-alias-or-replaced-destination-symlink-is-not-ownership-proof
func TestPlaceRootAliasDoesNotOpenOwnership(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, false)
	alias := root + "-alias"
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	plan.Root = alias
	if _, err := CommitPlace(context.Background(), stateDir, plan); err == nil {
		t.Fatal("place accepted a root alias")
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("root alias created a journal: %v", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root alias changed the target: %v", err)
	}
	if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root alias created ownership state: %v", err)
	}
}

// spec: target-profile-place/a-root-alias-or-replaced-destination-symlink-is-not-ownership-proof
func TestPlaceUsesOnlyTheCapturedContainedLink(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%t", replace), func(t *testing.T) {
			root, stateDir, plan := newPlaceFixture(t, false)
			targetDir := filepath.Join(root, "mods")
			if err := os.Mkdir(targetDir, 0o755); err != nil {
				t.Fatal(err)
			}
			actual := filepath.Join(targetDir, "actual.txt")
			other := filepath.Join(targetDir, "other.txt")
			link := filepath.Join(targetDir, "new.txt")
			for _, file := range []string{actual, other} {
				if err := os.WriteFile(file, []byte("prior\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("actual.txt", link); err != nil {
				t.Fatal(err)
			}
			capturedRoot, err := fsboundary.OpenDir(root)
			if err != nil {
				t.Fatal(err)
			}
			destination, err := fsboundary.CaptureDestination(capturedRoot, "mods/new.txt", fsboundary.LinkPolicyRepository)
			if err != nil {
				_ = capturedRoot.Close()
				t.Fatal(err)
			}
			plan.Writes[0].Path = "mods/new.txt"
			plan.Writes[0].Expected = Generation{Exists: true, Digest: Digest([]byte("prior\n"))}
			plan.Writes[0].ExpectedMode = 0o644
			plan.Writes[0].Link = destination.Link()
			_ = destination.Close()
			_ = capturedRoot.Close()
			plan.Directories = nil
			var errCommit error
			if replace {
				_, errCommit = CommitPlace(context.Background(), stateDir, plan, WithFaultHook(func(point FaultPoint) error {
					if point.Stage == FaultPlaceBeforePublish {
						if err := os.Remove(link); err != nil {
							return err
						}
						return os.Symlink("other.txt", link)
					}
					return nil
				}))
			} else {
				_, errCommit = CommitPlace(context.Background(), stateDir, plan)
			}
			if replace {
				if !errors.Is(errCommit, fsboundary.ErrGenerationConflict) || !strings.Contains(errCommit.Error(), "mods/new.txt") {
					t.Fatalf("replaced contained link commit = %v", errCommit)
				}
				assertFile(t, actual, "prior\n", 0o644)
				assertFile(t, other, "prior\n", 0o644)
				if _, err := os.Lstat(plan.Ownership.Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("link replacement created ownership state: %v", err)
				}
				if target, err := os.Readlink(link); err != nil || target != "other.txt" {
					t.Fatalf("replacement link = %q, %v; want other.txt", target, err)
				}
				if got := journals(t, stateDir); len(got) != 1 {
					t.Fatalf("link replacement discarded journal: %v", got)
				}
			} else {
				if errCommit != nil {
					t.Fatal(errCommit)
				}
				assertFile(t, actual, "new package bytes\n", 0o644)
				assertFile(t, other, "prior\n", 0o644)
				text, err := os.Readlink(link)
				if err != nil || text != "actual.txt" {
					t.Fatalf("captured destination link = %q, %v", text, err)
				}
			}
		})
	}
}

func TestPlaceComparesTheWholePriorOwnershipGeneration(t *testing.T) {
	root, stateDir, plan := newPlaceFixture(t, true)
	prior, err := os.ReadFile(plan.Ownership.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Ownership.Path, append(prior, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitPlace(context.Background(), stateDir, plan); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("place accepted a changed prior ownership generation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership conflict changed target: %v", err)
	}
	if got := journals(t, stateDir); len(got) != 0 {
		t.Fatalf("ownership conflict left journal: %v", got)
	}
}

func TestPlaceGenerationReaderRefusesCorruptJournal(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "dependencies")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, ".dependencytxn-corrupt.json")
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if keys, err := ReadPendingPlaceGenerationKeys(stateDir); err == nil || keys != nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("corrupt journal was ignored: keys=%v err=%v", keys, err)
	}
}
