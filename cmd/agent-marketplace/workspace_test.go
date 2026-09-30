package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// mustWorkspace builds a repository-root pipeline.Workspace for tests that do
// not exercise workspace validation itself.
func mustWorkspace(t testing.TB, repoRoot string) pipeline.Workspace {
	t.Helper()
	ws, err := pipeline.NewWorkspace(repoRoot, "")
	if err != nil {
		t.Fatalf("pipeline.NewWorkspace(%q): %v", repoRoot, err)
	}
	return ws
}
