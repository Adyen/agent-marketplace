package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A TUI write to the personal-local layer keeps that layer out of Git at once,
// as sync and the CLI do, instead of waiting for the next sync; a write to
// another layer leaves .gitignore alone.
// spec: tui-interactions/personal-local-writes-are-git-ignored-at-once
func TestPersonalLocalWritesAreGitIgnoredAtOnce(t *testing.T) {
	ignored := func(t *testing.T, repo string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	wantIgnored := func(t *testing.T, repo string) {
		t.Helper()
		got := ignored(t, repo)
		for _, entry := range []string{".agents/config.local.yaml", ".agents/lock.local.yaml"} {
			if !strings.Contains(got, entry+"\n") {
				t.Fatalf(".gitignore lacks %s after a personal-local write:\n%s", entry, got)
			}
		}
	}

	t.Run("project toggle", func(t *testing.T) {
		f := newBulkFixture(t)
		act := f.projectEnableActions(t)[0]
		msg, ok := runDependencyScopedToggle(context.Background(), f.workspace, act, f.mutate())().(configWrittenMsg)
		if !ok || msg.err != nil {
			t.Fatalf("project toggle message = %#v", msg)
		}
		if got := ignored(t, f.repo); got != "" {
			t.Fatalf("a project write changed .gitignore:\n%s", got)
		}
	})
	t.Run("local toggle", func(t *testing.T) {
		f := newBulkFixture(t)
		act := f.projectEnableActions(t)[0]
		act.scope = scopeLocal
		msg, ok := runDependencyScopedToggle(context.Background(), f.workspace, act, f.mutate())().(configWrittenMsg)
		if !ok || msg.err != nil {
			t.Fatalf("local toggle message = %#v", msg)
		}
		wantIgnored(t, f.repo)
	})
	t.Run("local bulk toggle", func(t *testing.T) {
		f := newBulkFixture(t)
		acts := f.projectEnableActions(t)
		for i := range acts {
			acts[i].scope = scopeLocal
		}
		msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
		if !ok || msg.err != nil {
			t.Fatalf("local bulk toggle message = %#v", msg)
		}
		wantIgnored(t, f.repo)
	})
}
