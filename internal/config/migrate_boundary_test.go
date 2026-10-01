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

// TestMigrateV2PreservesUserDotfileLink runs the legacy structural
// migration against a user config behind an approved dotfile link: the link
// must stay a link with its captured text and the target must receive the
// migrated document.
func TestMigrateV2PreservesUserDotfileLink(t *testing.T) {
	home, agents := boundaryUserLayer(t)
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	// A legacy collections list requires structural migration.
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n# keep this comment\ncollections:\n  - shared-tools\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateConfigFiles(MigratePaths{User: link}, nil)
	if err != nil {
		t.Fatalf("MigrateConfigFiles through dotfile link: %v", err)
	}
	if !result.UserMigrated {
		t.Fatalf("user config was not migrated: %#v", result)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: %v, %v", info, err)
	}
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link text = %q, %v", text, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "include:") || !strings.Contains(string(data), "shared-tools") {
		t.Fatalf("collections were not migrated: %q", data)
	}
	if !strings.Contains(string(data), "# keep this comment") {
		t.Fatalf("comment was not preserved: %q", data)
	}
	assertNoConfigTempResidue(t, agents)
	assertNoConfigTempResidue(t, filepath.Dir(target))
}

// TestMigrateV2RejectsEscapingRepositoryLink verifies the legacy migration
// fails before touching an escaping repository link or its target.
func TestMigrateV2RejectsEscapingRepositoryLink(t *testing.T) {
	_, agents := boundaryLayer(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("registries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := MigrateConfigFiles(MigratePaths{Repo: link}, []string{"factory"})
	if err == nil {
		t.Fatal("migration through an escaping repository link succeeded")
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil || string(data) != "registries: []\n" {
		t.Fatalf("outside file changed: %q, %v", data, readErr)
	}
	info, lerr := os.Lstat(link)
	if lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("escaping link was replaced: %v, %v", info, lerr)
	}
	assertNoConfigTempResidue(t, agents)
}

// TestMigrateV4PreservesUserDotfileLink runs the v4 migration against a
// linked user config: the link survives and the target holds the v4
// document after the journaled transaction commits.
func TestMigrateV4PreservesUserDotfileLink(t *testing.T) {
	home, agents := boundaryUserLayer(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateConfigFilesToV4WithEvidence(context.Background(), MigratePaths{User: link}, V4MigrationUser, false, nil)
	if err != nil {
		t.Fatalf("v4 migration through dotfile link: %v", err)
	}
	if result == nil || !result.UserMigrated {
		t.Fatalf("user config was not migrated: %#v", result)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: %v, %v", info, err)
	}
	if text, err := os.Readlink(link); err != nil || text != target {
		t.Fatalf("link text = %q, %v", text, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "agent-marketplace/v4") {
		t.Fatalf("target content = %q", data)
	}
	assertNoConfigTempResidue(t, agents)
	assertNoConfigTempResidue(t, filepath.Dir(target))
}

func TestManagedMigrationConflictPreservesEveryLayer(t *testing.T) {
	home, userAgents := boundaryUserLayer(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	_, repoAgents := boundaryLayer(t)
	const original = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ncollections: [shared-tools]\n"
	user := filepath.Join(userAgents, "config.yaml")
	repo := filepath.Join(repoAgents, "config.yaml")
	for _, path := range []string{user, repo} {
		if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	files := make(map[string]*migrationFile)
	for _, item := range []struct {
		name string
		path string
		kind layerKind
	}{{"user", user, userLayer}, {"repo", repo, repoLayer}} {
		file, err := loadMigrationFile(item.path, item.kind)
		if err != nil {
			t.Fatal(err)
		}
		file.doc, err = parseConfigDocument(item.path, []byte("apiVersion: agent-marketplace/v2\nharnesses: [windsurf]\ncollections: [shared-tools]\n"))
		if err != nil {
			t.Fatal(err)
		}
		file.modified = true
		files[item.name] = file
	}
	if err := os.WriteFile(repo, []byte("concurrent edit\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	var result MigrateResult
	if err := writeMigratedFilesAtomically(files, &result); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("stale migration was not refused: %v", err)
	}
	for path, want := range map[string]string{user: original, repo: "concurrent edit\n"} {
		if raw, err := os.ReadFile(path); err != nil || string(raw) != want {
			t.Fatalf("refusal partially migrated %s: %q %v", path, raw, err)
		}
	}
	if result.UserMigrated || result.RepoMigrated {
		t.Fatalf("refusal marked layers migrated: %+v", result)
	}
	assertNoConfigTempResidue(t, userAgents)
	assertNoConfigTempResidue(t, repoAgents)
	if err := os.WriteFile(repo, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if result, err := MigrateConfigFiles(MigratePaths{User: user, Repo: repo}, nil); err != nil || !result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("valid multi-layer migration control: %+v %v", result, err)
	}
}
