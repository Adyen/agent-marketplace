package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
)

func BenchmarkSupportedHarnessTargets(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = SupportedHarnessTargets()
	}
}

func BenchmarkDetectInstalledHarnessesInHome(b *testing.B) {
	home := b.TempDir()
	b.Setenv("CODEX_HOME", "")
	for _, root := range []string{".factory", ".claude", ".cursor", ".codeium/windsurf", ".gemini", ".config/devin", ".codex"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(root)), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := harness.DetectInstalled(home); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlanConfigMigration(b *testing.B) {
	root := b.TempDir()
	userPath := filepath.Join(root, "home", ".agents", "config.yaml")
	repoPath := filepath.Join(root, "repo", ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("# user\napiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("# repo\napiVersion: agent-marketplace/v2\nharnesses: [cursor]\nregistries: []\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	paths := MigratePaths{User: userPath, Repo: repoPath}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := PlanConfigMigration(paths, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetHarnessesInConfig(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# user\napiVersion: agent-marketplace/v3\nharnesses: [factory]\nregistries: []\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	selections := [][]string{{"cursor"}, {"factory"}}
	iteration := 0
	b.ReportAllocs()
	for b.Loop() {
		if err := SetHarnessesInConfig(path, selections[iteration%len(selections)]); err != nil {
			b.Fatal(err)
		}
		iteration++
	}
}
