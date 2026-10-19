package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func writeGitCollection(t *testing.T, working, name string, members ...string) {
	t.Helper()
	dir := filepath.Join(working, "collections", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\nresources:\n"
	for _, member := range members {
		body += "  - name: " + member + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "COLLECTION.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func listRowStatuses(t *testing.T, output, kind, name string) []string {
	t.Helper()
	_, rows := parseTable(t, output)
	var statuses []string
	for _, row := range rows {
		fields := strings.Split(row, "|")
		if len(fields) >= 4 && fields[1] == kind && fields[2] == name {
			statuses = append(statuses, fields[len(fields)-1])
		}
	}
	return statuses
}

func listRowStatus(t *testing.T, output, kind, name string) string {
	t.Helper()
	statuses := listRowStatuses(t, output, kind, name)
	if len(statuses) != 1 {
		t.Fatalf("want exactly one %s %q row, got %d in list output:\n%s", kind, name, len(statuses), output)
	}
	return statuses[0]
}

// spec: resource-listing/selected-git-collection-listed-enabled
// spec: resource-listing/unselected-collection-listed-available
func TestListShowsSelectedGitCollectionEnabledAndUnselectedAvailable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "tools-collections.git")
	writeSkill(t, working, "alpha", "Alpha", []string{"tools"})
	writeSkill(t, working, "beta", "Beta", []string{"tools"})
	writeGitCollection(t, working, "bundle", "alpha")
	writeGitCollection(t, working, "other", "beta")
	commitAndPushAll(t, working, "Add collections")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  tools: {git: file://`+origin+`, ref: main}
resources:
  tools: [collection/bundle]
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("expected list command to succeed: %v\n%s", err, output)
	}
	if got := listRowStatus(t, output, "collection", "bundle"); got != "enabled" {
		t.Fatalf("selected collection status = %q, want enabled\n%s", got, output)
	}
	if got := listRowStatus(t, output, "collection", "other"); got != "available" {
		t.Fatalf("unselected collection status = %q, want available\n%s", got, output)
	}
	if got := listRowStatus(t, output, "skill", "alpha"); got != "enabled" {
		t.Fatalf("collection member status = %q, want enabled\n%s", got, output)
	}
	if got := listRowStatuses(t, output, "collection", "bundle"); len(got) != 1 {
		t.Fatalf("selected collection must be listed once, got %v:\n%s", got, output)
	}
}

// spec: resource-listing/synced-marketplace-selection-is-listed-enabled
// spec: resource-listing/selected-marketplace-collection-listed-enabled
// spec: resource-listing/list-downloads-nothing-and-writes-nothing
func TestListShowsSyncedMarketplaceCollectionEnabledWithoutDownloads(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceCollectionServer(t, "mp", "bundle", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", server.URL+"/r/tools/collection/bundle", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace collection: %v\n%s", err, output)
	}
	configBefore := readTestFile(t, configPath)
	lockBefore := readTestFile(t, lockPath)

	var downloads atomic.Int32
	inner := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") {
			downloads.Add(1)
		}
		inner.ServeHTTP(w, r)
	})

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, output)
	}
	if got := listRowStatus(t, output, "collection", "bundle"); got != "enabled" {
		t.Fatalf("installed Marketplace collection status = %q, want enabled\n%s", got, output)
	}
	if got := listRowStatus(t, output, "skill", "review"); got != "enabled" {
		t.Fatalf("installed Marketplace collection member status = %q, want enabled\n%s", got, output)
	}
	if strings.Contains(output, "out of sync") {
		t.Fatalf("list reported drift for a synced Marketplace collection:\n%s", output)
	}
	if got := downloads.Load(); got != 0 {
		t.Fatalf("list downloaded %d Marketplace artifact(s); it must resolve from the verified cache", got)
	}
	if !bytes.Equal(configBefore, readTestFile(t, configPath)) {
		t.Fatal("list rewrote the configuration")
	}
	if !bytes.Equal(lockBefore, readTestFile(t, lockPath)) {
		t.Fatal("list rewrote the lock")
	}
	if checkOutput, checkErr := executeCommand("sync", "--check"); checkErr != nil {
		t.Fatalf("sync --check disagreed with list: %v\n%s", checkErr, checkOutput)
	}
}
