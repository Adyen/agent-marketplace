package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

// spec: config-mutation/approved-layer-links-receive-real-updates
func TestV4HarnessEditPreservesUserDotfileLinkAndLockOwner(t *testing.T) {
	home, agents := boundaryUserLayer(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# keep this comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := SetHarnessesInConfig(link, []string{"windsurf"}); err != nil {
		t.Fatal(err)
	}
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link text = %q, %v", text, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "windsurf") || !strings.Contains(string(data), "# keep this comment") {
		t.Fatalf("target not updated faithfully: %q %v", data, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v, %v", info, err)
	}
	paths, err := ResolveConfigLockPaths(link)
	if err != nil || paths.CanonicalLock != filepath.Join(agents, "lock.yaml") {
		t.Fatalf("lock moved to content target: %#v %v", paths, err)
	}
	assertNoConfigTempResidue(t, agents)
	assertNoConfigTempResidue(t, filepath.Dir(target))
}

// spec: config-mutation/replaced-publication-parents-and-leaves-are-refused
func TestManagedEditConflictPreservesEveryLayer(t *testing.T) {
	home, userAgents := boundaryUserLayer(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	_, repoAgents := boundaryLayer(t)
	const original = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	const desired = "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\n"
	user, repo := filepath.Join(userAgents, "config.yaml"), filepath.Join(repoAgents, "config.yaml")
	var files []dependencytxn.DesiredFile
	for _, path := range []string{user, repo} {
		if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
			t.Fatal(err)
		}
		_, generation, mode, err := dependencytxn.ReadSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, dependencytxn.DesiredFile{Path: path, Expected: generation, ExpectedMode: mode, Bytes: []byte(desired), Mode: mode})
	}
	if err := os.WriteFile(repo, []byte("concurrent edit\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	userBefore := v4OnlyFileSnapshot(t, user)
	repoBefore := v4OnlyFileSnapshot(t, repo)
	state, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dependencytxn.Commit(context.Background(), state, files); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("stale edit was not refused: %v", err)
	}
	for path, want := range map[string]string{user: original, repo: "concurrent edit\n"} {
		if got := readConfigString(t, path); got != want {
			t.Fatalf("refusal changed %s: %q", path, got)
		}
	}
	v4OnlyAssertUnchanged(t, user, userBefore)
	v4OnlyAssertUnchanged(t, repo, repoBefore)
	assertNoConfigTempResidue(t, userAgents)
	assertNoConfigTempResidue(t, repoAgents)
	_, generation, mode, err := dependencytxn.ReadSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	files[1].Expected, files[1].ExpectedMode = generation, mode
	if _, err := dependencytxn.Commit(context.Background(), state, files); err != nil {
		t.Fatalf("valid multi-layer edit control: %v", err)
	}
	for _, path := range []string{user, repo} {
		if got := readConfigString(t, path); got != desired {
			t.Fatalf("positive control did not publish %s: %q", path, got)
		}
	}
}

// spec: config-mutation/cross-layer-object-aliases-are-refused
func TestManagedEditRejectsCrossLayerAliases(t *testing.T) {
	_, agents := boundaryLayer(t)
	root, local := filepath.Join(agents, "config.yaml"), filepath.Join(agents, "config.local.yaml")
	before := v4OnlyWriteFixture(t, root, "apiVersion: agent-marketplace/v4\n")
	if err := os.Symlink("config.yaml", local); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	_, err := dependencytxn.Commit(context.Background(), state, []dependencytxn.DesiredFile{
		{Path: root, Bytes: []byte("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\n"), Mode: 0o640},
		{Path: local, Bytes: []byte("apiVersion: agent-marketplace/v4\nmaterialization: {mode: symlink}\n"), Mode: 0o640},
	})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("object alias not refused: %v", err)
	}
	v4OnlyAssertUnchanged(t, root, before)
	if text, err := os.Readlink(local); err != nil || text != "config.yaml" {
		t.Fatalf("alias changed: %q %v", text, err)
	}
	assertNoConfigTempResidue(t, agents)
}
