package dependencytxn

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// repoLayer builds a repository layer fixture: <base>/repo with an .agents
// directory, and returns (repo root, .agents dir, state dir).
func repoLayer(t *testing.T) (string, string, string) {
	t.Helper()
	root, state := testDirs(t)
	repo := filepath.Join(root, "repo")
	agents := filepath.Join(repo, ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, agents, state
}

// userLayer builds the user layer fixture inside the sandboxed HOME that
// testDirs installs, and returns (home, .agents dir, state dir).
func userLayer(t *testing.T) (string, string, string) {
	t.Helper()
	_, state := testDirs(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(home, ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, agents, state
}

func lstatInode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat payload for %s", path)
	}
	return stat.Ino
}

// assertNoStagingResidue fails when dir retains staged transaction temps.
func assertNoStagingResidue(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dependencytxn-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("staged transaction residue in %s: %s", dir, entry.Name())
		}
	}
}

func TestClassifyDestinationAuthority(t *testing.T) {
	_, state := testDirs(t)
	_ = state
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")

	for _, tc := range []struct {
		path string
		want Authority
	}{
		{filepath.Join(home, ".agents", "config.yaml"), AuthorityUser},
		{filepath.Join(home, ".agents", "lock.yaml"), AuthorityUser},
		{filepath.Join(home, ".agents", "config.local.yaml"), AuthorityUser},
		{filepath.Join(home, ".agents", "lock.local.yaml"), AuthorityUser},
		{filepath.Join(repo, ".agents", "config.yaml"), AuthorityRepository},
		{filepath.Join(repo, ".agents", "lock.yaml"), AuthorityRepository},
		{filepath.Join(repo, ".agents", "config.local.yaml"), AuthorityRepository},
		{filepath.Join(repo, ".agents", "lock.local.yaml"), AuthorityRepository},
		{filepath.Join(repo, ".agents", "other.yaml"), AuthorityLegacy},
		{filepath.Join(repo, "agents", "config.yaml"), AuthorityLegacy},
		{filepath.Join(repo, "config.yaml"), AuthorityLegacy},
		{filepath.Join(string(filepath.Separator), "etc", "agent-marketplace", "config.yaml"), AuthorityLegacy},
		{filepath.Join(string(filepath.Separator), "etc", "agent-marketplace", "lock.yaml"), AuthorityLegacy},
		{"relative/.agents/config.yaml", AuthorityLegacy},
	} {
		if got := ClassifyDestination(tc.path); got != tc.want {
			t.Errorf("ClassifyDestination(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCommitHonorsUserDotfileLink(t *testing.T) {
	home, agents, state := userLayer(t)
	dotfiles := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(dotfiles, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dotfiles, "config.yaml")
	mustWrite(t, target, []byte("apiVersion: agent-marketplace/v2\n"), 0o600)
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	linkInode := lstatInode(t, link)

	_, err := Commit(context.Background(), state, []DesiredFile{{
		Path:     link,
		Expected: generation(t, link),
		Bytes:    []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"),
		Mode:     0o600,
	}})
	if err != nil {
		t.Fatalf("Commit through approved user dotfile link: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("user config link was replaced: %v, %v", info, err)
	}
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link text = %q, %v; want %q", text, err, target)
	}
	if got := lstatInode(t, link); got != linkInode {
		t.Fatalf("link inode changed: %d -> %d", linkInode, got)
	}
	assertFile(t, target, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n", 0o600)
	if _, err := os.Lstat(filepath.Join(agents, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	assertNoStagingResidue(t, agents)
	assertNoStagingResidue(t, dotfiles)
	if err := CheckPending(state); err != nil {
		t.Fatalf("pending transaction after commit: %v", err)
	}
}

func TestCommitHonorsContainedRepositoryLinks(t *testing.T) {
	repo, agents, state := repoLayer(t)
	shared := filepath.Join(agents, "shared")
	if err := os.MkdirAll(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	configTarget := filepath.Join(shared, "base.yaml")
	mustWrite(t, configTarget, []byte("apiVersion: agent-marketplace/v2\n"), 0o644)
	configLink := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(filepath.Join("shared", "base.yaml"), configLink); err != nil {
		t.Fatal(err)
	}
	lockTarget := filepath.Join(shared, "lock.base.yaml")
	mustWrite(t, lockTarget, []byte("version: v1\n"), 0o644)
	lockLink := filepath.Join(agents, "lock.yaml")
	if err := os.Symlink(filepath.Join("shared", "lock.base.yaml"), lockLink); err != nil {
		t.Fatal(err)
	}

	_, err := Commit(context.Background(), state, []DesiredFile{
		{Path: configLink, Expected: generation(t, configLink), Bytes: []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), Mode: 0o644},
		{Path: lockLink, Expected: generation(t, lockLink), Bytes: []byte("version: v1\nrequirements: []\n"), Mode: 0o644},
	})
	if err != nil {
		t.Fatalf("Commit through contained repository links: %v", err)
	}

	for link, target := range map[string]string{configLink: filepath.Join("shared", "base.yaml"), lockLink: filepath.Join("shared", "lock.base.yaml")} {
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s was replaced: %v, %v", link, info, err)
		}
		if text, err := os.Readlink(link); err != nil || text != target {
			t.Fatalf("%s text = %q, %v; want %q", link, text, err, target)
		}
	}
	assertFile(t, configTarget, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n", 0o644)
	assertFile(t, lockTarget, "version: v1\nrequirements: []\n", 0o644)
	assertNoStagingResidue(t, agents)
	assertNoStagingResidue(t, shared)
	if err := CheckPending(state); err != nil {
		t.Fatalf("pending transaction after commit: %v", err)
	}
	_ = repo
}

func TestCommitRejectsEscapingRepositoryLink(t *testing.T) {
	_, agents, state := repoLayer(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	mustWrite(t, outside, []byte("untouched\n"), 0o644)
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := Commit(context.Background(), state, []DesiredFile{{
		Path:  link,
		Bytes: []byte("escape\n"),
		Mode:  0o644,
	}})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	assertFile(t, outside, "untouched\n", 0o644)
	info, lerr := os.Lstat(link)
	if lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("escaping link was replaced: %v, %v", info, lerr)
	}
	assertNoStagingResidue(t, agents)
	if err := CheckPending(state); err != nil {
		t.Fatalf("escaping link left a pending transaction: %v", err)
	}
}

func TestCommitRejectsMultiplyLinkedTarget(t *testing.T) {
	_, agents, state := repoLayer(t)
	target := filepath.Join(agents, "shared.yaml")
	mustWrite(t, target, []byte("shared\n"), 0o644)
	if err := os.Link(target, filepath.Join(agents, "hard.yaml")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(filepath.Join("shared.yaml"), link); err != nil {
		t.Fatal(err)
	}

	_, err := Commit(context.Background(), state, []DesiredFile{{
		Path:  link,
		Bytes: []byte("rewrite\n"),
		Mode:  0o644,
	}})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	assertFile(t, target, "shared\n", 0o644)
	assertNoStagingResidue(t, agents)
}

func TestCommitRejectsCrossLayerObjectAlias(t *testing.T) {
	t.Run("two layer destinations resolving to one object", func(t *testing.T) {
		_, agents, state := repoLayer(t)
		config := filepath.Join(agents, "config.yaml")
		mustWrite(t, config, []byte("apiVersion: agent-marketplace/v2\n"), 0o644)
		local := filepath.Join(agents, "config.local.yaml")
		if err := os.Symlink("config.yaml", local); err != nil {
			t.Fatal(err)
		}

		_, err := Commit(context.Background(), state, []DesiredFile{
			{Path: config, Expected: generation(t, config), Bytes: []byte("config write\n"), Mode: 0o644},
			{Path: local, Expected: generation(t, local), Bytes: []byte("local write\n"), Mode: 0o644},
		})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v, want ErrUnsafePath", err)
		}
		assertFile(t, config, "apiVersion: agent-marketplace/v2\n", 0o644)
		assertNoStagingResidue(t, agents)
		if err := CheckPending(state); err != nil {
			t.Fatalf("aliased transaction left residue: %v", err)
		}
	})

	t.Run("desired destination aliasing a read assertion", func(t *testing.T) {
		home, agents, state := userLayer(t)
		repo := filepath.Join(home, "repo")
		repoAgents := filepath.Join(repo, ".agents")
		if err := os.MkdirAll(repoAgents, 0o755); err != nil {
			t.Fatal(err)
		}
		repoConfig := filepath.Join(repoAgents, "config.yaml")
		mustWrite(t, repoConfig, []byte("apiVersion: agent-marketplace/v2\n"), 0o644)
		userConfig := filepath.Join(agents, "config.yaml")
		if err := os.Symlink(repoConfig, userConfig); err != nil {
			t.Fatal(err)
		}

		_, err := Commit(context.Background(), state,
			[]DesiredFile{{Path: userConfig, Expected: generation(t, userConfig), Bytes: []byte("user write\n"), Mode: 0o644}},
			WithReadAssertions([]ReadAssertion{{Path: repoConfig, RequestedPath: repoConfig, Expected: generation(t, repoConfig)}}),
		)
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v, want ErrUnsafePath", err)
		}
		assertFile(t, repoConfig, "apiVersion: agent-marketplace/v2\n", 0o644)
		assertNoStagingResidue(t, agents)
		assertNoStagingResidue(t, repoAgents)
		if err := CheckPending(state); err != nil {
			t.Fatalf("aliased transaction left residue: %v", err)
		}
	})
}

// TestCommitAllowsHonoredLinkReadAssertion mirrors the orchestrated lock
// flow: a managed destination behind an approved link is committed while a
// read assertion names the same object by its resolved target path (the
// config loader records reads at their canonical location). One layer
// identity, one object: not an alias.
func TestCommitAllowsHonoredLinkReadAssertion(t *testing.T) {
	_, agents, state := userLayer(t)
	target := filepath.Join(filepath.Dir(agents), "dotfiles", "agents.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, target, []byte("apiVersion: agent-marketplace/v2\n"), 0o644)
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(context.Background(), state,
		[]DesiredFile{{Path: link, Expected: generation(t, link), Bytes: []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), Mode: 0o644}},
		WithReadAssertions([]ReadAssertion{{Path: target, RequestedPath: link, Expected: generation(t, link)}}),
	); err != nil {
		t.Fatalf("honored link with resolved read assertion: %v", err)
	}
	assertFile(t, target, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n", 0o644)
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link replaced: text=%q err=%v", text, err)
	}
	assertNoStagingResidue(t, agents)
}

func TestRecoverPublishesThroughContainedLink(t *testing.T) {
	_, agents, state := repoLayer(t)
	target := filepath.Join(agents, "shared.yaml")
	mustWrite(t, target, []byte("old\n"), 0o644)
	link := filepath.Join(agents, "lock.yaml")
	if err := os.Symlink("shared.yaml", link); err != nil {
		t.Fatal(err)
	}

	crashed := false
	_, err := Commit(context.Background(), state,
		[]DesiredFile{{Path: link, Expected: generation(t, link), Bytes: []byte("new\n"), Mode: 0o644}},
		WithFaultHook(func(point FaultPoint) error {
			if point.Stage == FaultAfterJournal && !crashed {
				crashed = true
				return fmt.Errorf("simulated crash")
			}
			return nil
		}),
	)
	if err == nil || !crashed {
		t.Fatalf("crash injection err = %v", err)
	}
	assertFile(t, target, "old\n", 0o644)
	if err := CheckPending(state); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("pending = %v, want ErrRecoveryRequired", err)
	}

	if err := Recover(context.Background(), state); err != nil {
		t.Fatalf("Recover through contained link: %v", err)
	}
	assertFile(t, target, "new\n", 0o644)
	info, lerr := os.Lstat(link)
	if lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("recovery replaced the link: %v, %v", info, lerr)
	}
	assertNoStagingResidue(t, agents)
	if err := CheckPending(state); err != nil {
		t.Fatalf("pending after recovery: %v", err)
	}
}

func TestReadSnapshotHonorsApprovedLayerLinks(t *testing.T) {
	t.Run("user dotfile link", func(t *testing.T) {
		home, agents, _ := userLayer(t)
		target := filepath.Join(home, "dotfiles", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, target, []byte("linked\n"), 0o600)
		link := filepath.Join(agents, "config.yaml")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		raw, gen, mode, err := ReadSnapshot(link)
		if err != nil {
			t.Fatalf("ReadSnapshot: %v", err)
		}
		if string(raw) != "linked\n" || !gen.Exists || gen.Digest != Digest(raw) || mode != 0o600 {
			t.Fatalf("snapshot = %q %#v %o", raw, gen, mode)
		}
	})

	t.Run("contained repository lock link", func(t *testing.T) {
		_, agents, _ := repoLayer(t)
		target := filepath.Join(agents, "shared", "lock.yaml")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, target, []byte("version: v1\n"), 0o644)
		link := filepath.Join(agents, "lock.yaml")
		if err := os.Symlink(filepath.Join("shared", "lock.yaml"), link); err != nil {
			t.Fatal(err)
		}
		raw, gen, _, err := ReadSnapshot(link)
		if err != nil {
			t.Fatalf("ReadSnapshot: %v", err)
		}
		if string(raw) != "version: v1\n" || !gen.Exists {
			t.Fatalf("snapshot = %q %#v", raw, gen)
		}
	})

	t.Run("escaping repository link refused", func(t *testing.T) {
		_, agents, _ := repoLayer(t)
		outside := filepath.Join(t.TempDir(), "outside.yaml")
		mustWrite(t, outside, []byte("out\n"), 0o644)
		link := filepath.Join(agents, "config.yaml")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := ReadSnapshot(link); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("missing layer destination", func(t *testing.T) {
		_, agents, _ := repoLayer(t)
		raw, gen, mode, err := ReadSnapshot(filepath.Join(agents, "config.yaml"))
		if err != nil || gen.Exists || raw != nil || mode != 0 {
			t.Fatalf("snapshot = %q %#v %o, %v", raw, gen, mode, err)
		}
	})
}

// TestTransactionDescriptorParentReplacement pins the deterministic
// replacement barriers of a managed config/lock transaction: a swap of the
// .agents parent or the destination leaf at capture, staging open, or
// publication must either stay bound to the captured object or fail with a
// stable conflict, and must never touch the replacement tree or leave
// staging residue behind.
func TestTransactionDescriptorParentReplacement(t *testing.T) {
	oldBytes := []byte("apiVersion: agent-marketplace/v2\n")
	newBytes := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")

	// swapAgents renames the live .agents aside and plants a replacement
	// tree. The returned restore removes the replacement and reinstates the
	// original directory.
	swapAgents := func(t *testing.T, repo string) (live, aside string, restore func()) {
		t.Helper()
		live = filepath.Join(repo, ".agents")
		aside = filepath.Join(repo, ".agents-aside")
		if err := os.Rename(live, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(live, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(live, "config.yaml"), []byte("replacement tree\n"), 0o644)
		return live, aside, func() {
			if err := os.RemoveAll(live); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(aside, live); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("capture", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		config := filepath.Join(agents, "config.yaml")
		mustWrite(t, config, oldBytes, 0o644)
		expected := generation(t, config)

		live, aside, restore := swapAgents(t, repo)
		_, err := Commit(context.Background(), state, []DesiredFile{{Path: config, Expected: expected, Bytes: newBytes, Mode: 0o644}})
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("error = %v, want ErrGenerationConflict", err)
		}
		assertFile(t, filepath.Join(live, "config.yaml"), "replacement tree\n", 0o644)
		assertFile(t, filepath.Join(aside, "config.yaml"), string(oldBytes), 0o644)
		assertNoStagingResidue(t, live)
		assertNoStagingResidue(t, aside)
		if err := CheckPending(state); err != nil {
			t.Fatalf("pending after capture conflict: %v", err)
		}
		restore()
	})

	t.Run("open", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		config := filepath.Join(agents, "config.yaml")
		mustWrite(t, config, oldBytes, 0o644)

		swapped := false
		var live, aside string
		_, err := Commit(context.Background(), state,
			[]DesiredFile{{Path: config, Expected: generation(t, config), Bytes: newBytes, Mode: 0o644}},
			WithFaultHook(func(point FaultPoint) error {
				if point.Stage == FaultAfterCAS && !swapped {
					swapped = true
					var restore func()
					live, aside, restore = swapAgents(t, repo)
					_ = restore
				}
				return nil
			}),
		)
		if !swapped {
			t.Fatal("fault hook did not fire")
		}
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("error = %v, want ErrGenerationConflict", err)
		}
		assertFile(t, filepath.Join(live, "config.yaml"), "replacement tree\n", 0o644)
		assertFile(t, filepath.Join(aside, "config.yaml"), string(oldBytes), 0o644)
		assertNoStagingResidue(t, live)
		assertNoStagingResidue(t, aside)
		if err := CheckPending(state); err != nil {
			t.Fatalf("pending after staging-open conflict: %v", err)
		}
		if err := os.RemoveAll(live); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(aside, live); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("publication", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		config := filepath.Join(agents, "config.yaml")
		mustWrite(t, config, oldBytes, 0o644)

		swapped := false
		var live, aside string
		_, err := Commit(context.Background(), state,
			[]DesiredFile{{Path: config, Expected: generation(t, config), Bytes: newBytes, Mode: 0o644}},
			WithFaultHook(func(point FaultPoint) error {
				if point.Stage == FaultBeforePublish && !swapped {
					swapped = true
					var restore func()
					live, aside, restore = swapAgents(t, repo)
					_ = restore
				}
				return nil
			}),
		)
		if !swapped {
			t.Fatal("fault hook did not fire")
		}
		// The journal is already published at the publication barrier, so the
		// engine reports its stable recovery-blocked conflict and fails
		// closed until the captured state is restored.
		if !errors.Is(err, ErrRecoveryBlocked) {
			t.Fatalf("error = %v, want ErrRecoveryBlocked", err)
		}
		assertFile(t, filepath.Join(live, "config.yaml"), "replacement tree\n", 0o644)
		assertFile(t, filepath.Join(aside, "config.yaml"), string(oldBytes), 0o644)
		assertNoStagingResidue(t, live)
		if err := CheckPending(state); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("pending = %v, want ErrRecoveryRequired", err)
		}

		// While the replacement tree is live, recovery cannot classify the
		// foreign bytes and must fail closed without touching either tree.
		if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
			t.Fatalf("recover while swapped = %v, want ErrRecoveryBlocked", err)
		}
		assertFile(t, filepath.Join(live, "config.yaml"), "replacement tree\n", 0o644)
		assertFile(t, filepath.Join(aside, "config.yaml"), string(oldBytes), 0o644)

		if err := os.RemoveAll(live); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(aside, live); err != nil {
			t.Fatal(err)
		}
		if err := Recover(context.Background(), state); err != nil {
			t.Fatalf("recover after restore: %v", err)
		}
		assertFile(t, filepath.Join(live, "config.yaml"), string(newBytes), 0o644)
		assertNoStagingResidue(t, live)
		if err := CheckPending(state); err != nil {
			t.Fatalf("pending after recovery: %v", err)
		}
	})

	t.Run("leaf", func(t *testing.T) {
		_, agents, state := repoLayer(t)
		config := filepath.Join(agents, "config.yaml")
		mustWrite(t, config, oldBytes, 0o644)

		replaced := false
		intruder := filepath.Join(agents, "intruder.yaml")
		_, err := Commit(context.Background(), state,
			[]DesiredFile{{Path: config, Expected: generation(t, config), Bytes: newBytes, Mode: 0o644}},
			WithFaultHook(func(point FaultPoint) error {
				if point.Stage == FaultBeforePublish && !replaced {
					replaced = true
					mustWrite(t, intruder, []byte("intruder\n"), 0o644)
					if err := os.Rename(intruder, config); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}),
		)
		if !replaced {
			t.Fatal("fault hook did not fire")
		}
		if !errors.Is(err, ErrRecoveryBlocked) {
			t.Fatalf("error = %v, want ErrRecoveryBlocked", err)
		}
		assertFile(t, config, "intruder\n", 0o644)
		if err := CheckPending(state); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("pending = %v, want ErrRecoveryRequired", err)
		}

		// The intruder is foreign to the journal: recovery refuses until the
		// operator restores the captured state, then completes.
		if err := Recover(context.Background(), state); !errors.Is(err, ErrRecoveryBlocked) {
			t.Fatalf("recover with intruder = %v, want ErrRecoveryBlocked", err)
		}
		assertFile(t, config, "intruder\n", 0o644)
		if err := os.Remove(config); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, config, oldBytes, 0o644)
		if err := Recover(context.Background(), state); err != nil {
			t.Fatalf("recover after restore: %v", err)
		}
		assertFile(t, config, string(newBytes), 0o644)
		assertNoStagingResidue(t, agents)
	})
}

// TestCommitManagedStageFailureLeavesNoResidue pins the cleanup ordering of
// a managed commit that fails after its staged file exists: the staged temp
// must be unlinked while its staging descriptor is still open, leaving the
// destination untouched and the layer directory free of residue.
func TestCommitManagedStageFailureLeavesNoResidue(t *testing.T) {
	_, agents, state := repoLayer(t)
	config := filepath.Join(agents, "config.yaml")
	oldBytes := []byte("apiVersion: agent-marketplace/v2\n")
	newBytes := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	mustWrite(t, config, oldBytes, 0o644)

	boom := errors.New("injected post-stage failure")
	fired := false
	_, err := Commit(context.Background(), state,
		[]DesiredFile{{Path: config, Expected: generation(t, config), Bytes: newBytes, Mode: 0o644}},
		WithFaultHook(func(point FaultPoint) error {
			if point.Stage == FaultAfterStage && point.Index == 0 {
				fired = true
				return boom
			}
			return nil
		}),
	)
	if !fired {
		t.Fatal("fault hook did not fire")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want injected failure", err)
	}
	assertFile(t, config, string(oldBytes), 0o644)
	assertNoStagingResidue(t, agents)
	if err := CheckPending(state); err != nil {
		t.Fatalf("pending after post-stage failure: %v", err)
	}
}
