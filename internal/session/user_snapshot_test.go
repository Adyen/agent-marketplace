package session_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func newUserFixture(t *testing.T) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))

	source := t.TempDir()
	writeUserFile(t, filepath.Join(source, "solo", "SKILL.md"), "---\nname: solo\ndescription: Solo skill\n---\n# Solo\n")
	writeUserFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")

	repo = t.TempDir()
	writeUserFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: [not\n")
	writeUserFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), "registries: [broken\n")
	t.Chdir(repo)
	return home, repo
}

func writeUserFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readUserFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// spec: gui-shell/launch-directory-does-not-change-the-gui-workspace
func TestReadUserSnapshotIgnoresCwdRoots(t *testing.T) {
	home, repo := newUserFixture(t)

	var repoReads, localReads atomic.Int32
	prevRepo, prevLocal := session.LoadRepoConfig, session.LoadLocalConfig
	session.LoadRepoConfig = func() (*config.Config, error) {
		repoReads.Add(1)
		return prevRepo()
	}
	session.LoadLocalConfig = func(string) (*config.LocalConfig, error) {
		localReads.Add(1)
		return nil, nil
	}
	t.Cleanup(func() { session.LoadRepoConfig, session.LoadLocalConfig = prevRepo, prevLocal })

	repoBefore := readUserFile(t, filepath.Join(repo, ".agents", "config.yaml"))
	localBefore := readUserFile(t, filepath.Join(repo, ".agents", "config.local.yaml"))
	userBefore := readUserFile(t, filepath.Join(home, ".agents", "config.yaml"))

	snapshot := session.ReadUserSnapshot(context.Background(), home, session.CatalogLoadRefresh)
	if len(snapshot.Errs) > 0 {
		t.Fatalf("user snapshot reported errors from excluded roots: %v", snapshot.Errs)
	}
	found := false
	for _, row := range snapshot.Rows {
		if row.Artifact.Name == "solo" {
			found = true
		}
	}
	if !found {
		t.Fatal("user snapshot did not resolve the user-root resource")
	}

	refresh := session.ReadUserSnapshot(context.Background(), home, session.CatalogLoadRefresh)
	if len(refresh.Rows) != len(snapshot.Rows) {
		t.Fatalf("refresh rows = %d, want %d", len(refresh.Rows), len(snapshot.Rows))
	}

	var stages []session.Snapshot
	for msg := range session.ReadUserSnapshotStages(context.Background(), home, session.CatalogLoadRefresh, 9) {
		stages = append(stages, msg)
	}
	if len(stages) == 0 {
		t.Fatal("no staged snapshots delivered")
	}
	for _, msg := range stages {
		if msg.Generation != 9 {
			t.Fatalf("stage generation = %d, want 9", msg.Generation)
		}
	}
	if last := stages[len(stages)-1]; last.Stage != session.SnapshotStageFinal {
		t.Fatalf("final stage = %v, want SnapshotStageFinal", last.Stage)
	}

	if repoReads.Load() != 0 || localReads.Load() != 0 {
		t.Fatalf("excluded root loaders invoked: repo=%d local=%d", repoReads.Load(), localReads.Load())
	}
	if string(readUserFile(t, filepath.Join(repo, ".agents", "config.yaml"))) != string(repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if string(readUserFile(t, filepath.Join(repo, ".agents", "config.local.yaml"))) != string(localBefore) {
		t.Fatal("personal-local config changed")
	}
	if string(readUserFile(t, filepath.Join(home, ".agents", "config.yaml"))) != string(userBefore) {
		t.Fatal("user config changed by a read")
	}
}

func TestReadUserSnapshotRejectsNonHome(t *testing.T) {
	newUserFixture(t)
	notHome := t.TempDir()
	snapshot := session.ReadUserSnapshot(context.Background(), notHome, session.CatalogLoadRefresh)
	if len(snapshot.Errs) == 0 {
		t.Fatal("non-HOME root accepted")
	}
	staged := session.ReadUserSnapshotStages(context.Background(), notHome, session.CatalogLoadRefresh, 4)
	msg, ok := <-staged
	if !ok || msg.Generation != 4 || len(msg.Errs) == 0 {
		t.Fatalf("fail-closed stage = %+v ok=%v, want generation 4 with errors", msg, ok)
	}
}
