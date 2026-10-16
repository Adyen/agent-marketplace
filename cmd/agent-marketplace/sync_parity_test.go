package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestSyncCLIAndTUIParity(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, sharedRegistry := range []bool{false, true} {
		name := "organization-only"
		if sharedRegistry {
			name = "organization-and-locked-repo"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			origin, working := createBareRepoWithWorkingClone(t, "parity.git")
			writeSkill(t, working, "review", "Review", nil)
			commitAndPushAll(t, working, "seed")
			harnesses := "apiVersion: agent-marketplace/v4\nharnesses: [factory, devin, codex, claude, cursor, gemini, windsurf]\n"
			registry := "sources:\n  tools:\n    git: file://" + origin + "\n    ref: main\nresources:\n  tools:\n    include: [skill/review]\n"
			repo := filepath.Join(t.TempDir(), "repo")
			repoConfig := harnesses
			if sharedRegistry {
				repoConfig += registry
			}
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), repoConfig)
			authority := canonicalTempDir(t)
			orgPath := filepath.Join(authority, "config.yaml")
			writeConfig(t, orgPath, harnesses+registry+"    required: true\n")
			if err := os.Chmod(authority, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
			config.SetTestOrganizationConfigPath(t, orgPath)
			withWorkingDir(t, repo)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("CLI sync: %v\n%s", err, output)
			}
			link := filepath.Join(home, ".factory", "skills", "review")
			before, err := os.Readlink(link)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, release := pipeline.ResolveWithExactLockLease(context.Background(), pipeline.ResolveOptions{Workspace: mustWorkspace(t, repo), ReadOnlyExactLocks: true})
				report, err := materialize.Run(context.Background(), repo, result.Resolved, materialize.WithDryRun(), materialize.WithMaterializationModes(result.MaterializationModes))
				release()
				if err != nil || len(result.Errors) != 0 {
					t.Fatalf("TUI drift check: %v, %v", err, result.Errors)
				}
				if !report.InSync() {
					t.Errorf("TUI reports drift after CLI sync: writes=%d, removals=%d", report.TotalWritten(), report.TotalRemoved())
				}
				if _, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repo), false); err != nil {
					t.Fatal(err)
				}
				assertLink := func() {
					t.Helper()
					after, err := os.Readlink(link)
					if err != nil || after != before {
						t.Fatalf("sync changed symlink: %q -> %q (%v)", before, after, err)
					}
				}
				assertLink()
				for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}, {"sync", "--offline"}, {"sync"}} {
					if output, err := executeCommand(args...); err != nil {
						t.Fatalf("%v after TUI sync: %v\n%s", args, err, output)
					}
					assertLink()
				}
			}
			if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
				t.Fatalf("consumer created organization lock: %v", err)
			}
		})
	}
}
