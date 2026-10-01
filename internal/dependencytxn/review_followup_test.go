package dependencytxn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHomeDestinationRetainsRepositoryAuthority(t *testing.T) {
	_, agents, state := userLayer(t)
	outside := filepath.Join(t.TempDir(), "external.yaml")
	mustWrite(t, outside, []byte("preserve\n"), 0o640)
	for _, name := range []string{"config.local.yaml", "lock.local.yaml"} {
		path := filepath.Join(agents, name)
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if ClassifyDestination(path) != AuthorityRepository {
			t.Fatalf("personal-local destination inherited user authority: %s", name)
		}
		if _, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Bytes: []byte("write\n"), Mode: 0o640}}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("local-home escape was not refused: %v", err)
		}
	}
	assertFile(t, outside, "preserve\n", 0o640)
	assertNoStagingResidue(t, agents)
}

func TestExistingExternalConfigLinkIsNotDangling(t *testing.T) {
	_, agents, _ := repoLayer(t)
	outside := filepath.Join(t.TempDir(), "external.yaml")
	mustWrite(t, outside, []byte("preserve\n"), 0o600)
	path := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := ReadSnapshot(path)
	var dangling *danglingManagedConfigLinkError
	if !errors.Is(err, ErrUnsafePath) || errors.As(err, &dangling) {
		t.Fatalf("existing external config misclassified as dangling: %v", err)
	}
	assertFile(t, outside, "preserve\n", 0o600)
}

func TestSnapshotDetectsDanglingLayerParent(t *testing.T) {
	_, agents, _ := repoLayer(t)
	parent := filepath.Join(filepath.Dir(agents), "dangling", ".agents")
	if err := os.MkdirAll(filepath.Dir(parent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-parent", parent); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := ReadSnapshot(filepath.Join(parent, "config.yaml"))
	var dangling *danglingManagedConfigLinkError
	if !errors.Is(err, ErrUnsafePath) || !errors.As(err, &dangling) {
		t.Fatalf("dangling parent lost its typed condition: %v", err)
	}
	if text, err := os.Readlink(parent); err != nil || text != "missing-parent" {
		t.Fatalf("refusal changed parent link: %q %v", text, err)
	}
}

func TestRecoverUserLinkFromTargetParentStaging(t *testing.T) {
	home, agents, state := userLayer(t)
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, target, []byte("original\n"), 0o600)
	path := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("publication interrupted")
	_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Bytes: []byte("updated\n"), Mode: 0o600}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterJournal {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatalf("did not reach journaled target-parent staging: %v", err)
	}
	assertFile(t, target, "original\n", 0o600)
	if err := Recover(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "updated\n", 0o600)
	if text, err := os.Readlink(path); err != nil || text != target {
		t.Fatalf("recovery changed logical link: %q %v", text, err)
	}
	assertNoStagingResidue(t, filepath.Dir(target))
	if err := CheckPending(state); err != nil {
		t.Fatalf("recovery left a journal: %v", err)
	}
}
