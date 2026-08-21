package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// writeSkill creates a registry skill directory with the given manifest bytes.
func writeSkill(t *testing.T, registry, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(registry, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoverRejectsSkillSymlinkOutsideRegistry(t *testing.T) {
	registry := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("must not be copied"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := writeSkill(t, registry, "external", "")
	// A relative traversal, so the rejection does not depend on the target being
	// an absolute path.
	rel, err := filepath.Rel(skillDir, filepath.Join(external, "sentinel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Discover(registry, "claude")
	if err == nil || !strings.Contains(err.Error(), "resolves outside the registry root") {
		t.Fatalf("Discover error = %v, want skill symlink containment rejection", err)
	}
	if len(loaded.Entries) != 0 || len(loaded.Resources) != 0 {
		t.Fatalf("rejected registry returned resources: entries=%v resources=%v", loaded.Entries, loaded.Resources)
	}
}

// TestDiscoverRejectsAbsoluteSkillSymlink pins the deliberately stricter rule
// for absolute targets: they cannot be followed from a directory descriptor, so
// they are refused even when they point back inside the registry.
func TestDiscoverRejectsAbsoluteSkillSymlink(t *testing.T) {
	registry := t.TempDir()
	real := writeSkill(t, registry, "real", "---\nname: real\ndescription: Real skill\n---\n\nBody\n")
	skillDir := writeSkill(t, registry, "aliased", "")
	if err := os.Symlink(filepath.Join(real, "SKILL.md"), filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(registry, "claude")
	if err == nil || !strings.Contains(err.Error(), "absolute symlink") {
		t.Fatalf("Discover error = %v, want absolute symlink rejection", err)
	}
}

// TestDiscoverDoesNotBlockOnFIFOManifest is the discovery-side regression for
// the hang: opening a FIFO for reading waits for a writer forever, so a registry
// could freeze `list`, `status`, and `sync` alike.
func TestDiscoverDoesNotBlockOnFIFOManifest(t *testing.T) {
	registry := t.TempDir()
	skillDir := writeSkill(t, registry, "trap", "")
	mkfifo(t, filepath.Join(skillDir, "SKILL.md"))

	done := make(chan error, 1)
	go func() {
		_, err := Discover(registry, "claude")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
			t.Fatalf("Discover error = %v, want unsupported file type", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Discover blocked on a FIFO manifest")
	}
}

func TestDiscoverAllowsContainedRelativeSymlink(t *testing.T) {
	registry := t.TempDir()
	shared := filepath.Join(registry, "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "SKILL.md"), []byte("---\nname: shared\ndescription: Shared skill\n---\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := writeSkill(t, registry, "linked", "")
	if err := os.Symlink("../../shared/SKILL.md", filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Discover(registry, "claude")
	if err != nil {
		t.Fatalf("Discover rejected a contained relative symlink: %v", err)
	}
	if len(loaded.Entries) == 0 {
		t.Fatal("no entries discovered through a contained symlink")
	}
}

func TestDiscoverSetsSourceRootForConfinedSupportReads(t *testing.T) {
	registry := t.TempDir()
	writeSkill(t, registry, "provenance", "---\nname: provenance\ndescription: Provenance\n---\n\nBody\n")

	loaded, err := Discover(registry, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) == 0 {
		t.Fatal("no entries discovered")
	}
	want, err := filepath.Abs(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range loaded.Entries {
		if entry.SourceRoot != want {
			t.Fatalf("entry %q SourceRoot = %q, want %q", entry.Name, entry.SourceRoot, want)
		}
	}
}

func TestLoadRegistriesRejectsRemoteSymlink(t *testing.T) {
	registry := t.TempDir()
	target := filepath.Join(registry, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Contained and relative — a remote registry may not use symlinks at all.
	if err := os.Symlink("target", filepath.Join(registry, "link")); err != nil {
		t.Fatal(err)
	}

	result := loadRemoteRegistry(t, registry)
	if len(result.Errors) == 0 {
		t.Fatal("LoadRegistries accepted a remote registry containing a symlink")
	}
	if !strings.Contains(result.Errors[0].Error(), "remote registries do not allow symlinks") {
		t.Fatalf("LoadRegistries error = %v, want actionable remote symlink rejection", result.Errors[0])
	}
}

// TestLoadRegistriesRejectsRemoteSpecialFile covers the other half of the remote
// policy: a special file anywhere in a cloned tree is reported before ingestion
// reads anything.
func TestLoadRegistriesRejectsRemoteSpecialFile(t *testing.T) {
	registry := t.TempDir()
	writeSkill(t, registry, "ok", "---\nname: ok\ndescription: Fine\n---\n\nBody\n")
	mkfifo(t, filepath.Join(registry, "skills", "ok", "pipe"))

	done := make(chan LoadResult, 1)
	go func() { done <- loadRemoteRegistry(t, registry) }()
	select {
	case result := <-done:
		if len(result.Errors) == 0 {
			t.Fatal("LoadRegistries accepted a remote registry containing a FIFO")
		}
		if !strings.Contains(result.Errors[0].Error(), "unsupported file type") {
			t.Fatalf("LoadRegistries error = %v, want actionable special-file rejection", result.Errors[0])
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadRegistries blocked on a FIFO in a remote registry")
	}
}

// TestLoadRegistriesIgnoresGitMetadataForRemotePolicy keeps the eager remote
// walk scoped to registry content: `.git` is a clone artifact this tool created,
// it is never ingested, and walking it would make every refresh pay for the
// whole object store.
func TestLoadRegistriesIgnoresGitMetadataForRemotePolicy(t *testing.T) {
	registry := t.TempDir()
	writeSkill(t, registry, "ok", "---\nname: ok\ndescription: Fine\n---\n\nBody\n")
	gitDir := filepath.Join(registry, ".git", "objects")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../skills/ok/SKILL.md", filepath.Join(gitDir, "link")); err != nil {
		t.Fatal(err)
	}

	result := loadRemoteRegistry(t, registry)
	for _, err := range result.Errors {
		if strings.Contains(err.Error(), "symlink") {
			t.Fatalf("remote validation walked .git: %v", err)
		}
	}
}

func loadRemoteRegistry(t *testing.T, cachePath string) LoadResult {
	t.Helper()
	return LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{
		URL:           "https://example.invalid/registry.git",
		Ref:           "main",
		SourceHarness: "claude",
	}}}, "", LoadModeRefresh, WithCloneOrPull(func(_, _ string) (string, error) {
		return cachePath, nil
	}))
}
