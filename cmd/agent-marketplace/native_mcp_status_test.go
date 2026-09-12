package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMCPStatusDestinations(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	rows, err := buildHarnessTargetRows(repo, []string{"claude", "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	for _, row := range rows {
		switch strings.ToLower(row.Target) {
		case "claude", "claude code":
			if !strings.Contains(row.RepoRoot, filepath.Join(repo, ".mcp.json")) || !strings.Contains(row.MachineRoot, filepath.Join(home, ".claude.json")) {
				t.Fatalf("Claude native paths not reported: %+v", row)
			}
		case "gemini", "gemini cli":
			if !strings.Contains(row.RepoRoot, filepath.Join(repo, ".gemini/settings.json")) || !strings.Contains(row.MachineRoot, filepath.Join(home, ".gemini/settings.json")) {
				t.Fatalf("Gemini native paths not reported: %+v", row)
			}
		default:
			t.Fatalf("unexpected target: %+v", row)
		}
	}
}
