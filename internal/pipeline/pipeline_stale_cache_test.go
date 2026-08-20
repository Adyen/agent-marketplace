package pipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v\n%s", args, dir, err, out)
	}
}

// seedGitRegistry creates a bare git registry at origin containing one skill.
func seedGitRegistry(t *testing.T, origin string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	runGitForTest(t, "", "init", "--initial-branch=main", source)
	writeSkill(t, source, "cached-skill", "Cached skill", []string{"test"})
	runGitForTest(t, source, "add", ".")
	runGitForTest(t, source, "commit", "-m", "add skill")
	runGitForTest(t, "", "clone", "--bare", source, origin)
}

func staleCacheLoaders(repoCfg *config.Config) Loaders {
	return Loaders{
		Org:    func() (*config.Config, error) { return &config.Config{}, nil },
		Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
		Repo:   func() (*config.Config, error) { return repoCfg, nil },
		Local:  func() (*config.LocalConfig, error) { return nil, nil },
	}
}

// TestSyncReportsStaleCacheFallbackAsWarning pins the pipeline contract behind
// the CLI's partial-outcome reporting: when a registry fetch fails but a warm
// cache exists, sync succeeds and the typed *cache.StaleCacheFallbackWarning
// rides in SyncResult.Warnings — a caller that only prints errors would claim
// a clean refresh from stale data.
func TestSyncReportsStaleCacheFallbackAsWarning(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	origin := filepath.Join(t.TempDir(), "stale-reg.git")
	seedGitRegistry(t, origin)

	repoCfg := &config.Config{
		APIVersion: config.APIVersionV3,
		Registries: []config.RegistryEntry{{
			URL: origin, Ref: "main", SourceHarness: "claude",
			Skills: config.KindSelector{Include: []string{"cached-skill"}},
		}},
	}
	loaders := staleCacheLoaders(repoCfg)

	// First sync warms the cache and materializes from the live registry.
	first := Sync(context.Background(), ResolveOptions{RepoRoot: repoRoot, NoBackup: true, Loaders: loaders})
	if len(first.Errors) != 0 {
		t.Fatalf("initial sync failed: %v", first.Errors)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "cached-skill", "SKILL.md")); err != nil {
		t.Fatalf("initial sync did not materialize the skill: %v", err)
	}

	// Break the remote: the fetch fails, the warm cache must serve the run.
	if err := os.Rename(origin, origin+".offline"); err != nil {
		t.Fatalf("rename origin: %v", err)
	}

	second := Sync(context.Background(), ResolveOptions{RepoRoot: repoRoot, NoBackup: true, Loaders: loaders})
	if len(second.Errors) != 0 {
		t.Fatalf("stale-cache sync must succeed from the warm cache, got errors: %v", second.Errors)
	}
	var typed *cache.StaleCacheFallbackWarning
	found := false
	for _, warning := range second.Warnings {
		if errors.As(warning, &typed) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("stale-cache fallback not reported as a typed warning; warnings: %v", second.Warnings)
	}
	if typed.Ref != "main" {
		t.Fatalf("stale warning lost the ref identity: %+v", typed)
	}
	resolved := false
	for _, artifact := range second.Resolved {
		if artifact.Name == "cached-skill" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("stale-cache sync did not resolve the cached skill: %#v", second.Resolved)
	}
}

// TestUpdateReportsStaleCacheFallbackAsPartialOutcome pins Update: both the
// bump-stage fetch and the sync-stage fetch feed UpdateResult.StaleCaches so
// the CLI can print the partial outcome instead of a bare "Updated N".
func TestUpdateReportsStaleCacheFallbackAsPartialOutcome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	// The machine-wide target harness set comes from the user-personal layer.
	userCfgPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userCfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfgPath, []byte("apiVersion: agent-marketplace/v3\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origin := filepath.Join(t.TempDir(), "stale-update-reg.git")
	seedGitRegistry(t, origin)

	repoCfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(repoCfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	repoCfgBody := "apiVersion: agent-marketplace/v3\nregistries:\n  - url: " + origin + "\n    ref: main\n    skills:\n      include:\n        - cached-skill\n"
	if err := os.WriteFile(repoCfgPath, []byte(repoCfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Warm the cache through a first update.
	first, err := Update(context.Background(), UpdateOptions{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("initial update failed: %v", err)
	}
	if len(first.StaleCaches) != 0 {
		t.Fatalf("initial update against a live registry reported stale caches: %v", first.StaleCaches)
	}

	// Break the remote: update must complete partially and say so.
	if err := os.Rename(origin, origin+".offline"); err != nil {
		t.Fatalf("rename origin: %v", err)
	}
	second, err := Update(context.Background(), UpdateOptions{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("update with a warm cache must complete partially, not fail: %v", err)
	}
	if len(second.StaleCaches) != 1 {
		t.Fatalf("update stale caches = %v, want exactly one typed warning", second.StaleCaches)
	}
	var typed *cache.StaleCacheFallbackWarning
	if !errors.As(second.StaleCaches[0], &typed) {
		t.Fatalf("stale-cache fallback is not a typed *cache.StaleCacheFallbackWarning: %T %v", second.StaleCaches[0], second.StaleCaches[0])
	}
}
