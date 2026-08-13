package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadEffectiveSmall(b *testing.B) {
	dir := b.TempDir()
	writeBenchConfig(b, dir, 2)
	opts := LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "org.yaml"),
		GlobalPath:       filepath.Join(dir, "user.yaml"),
		RepoPath:         filepath.Join(dir, "repo.yaml"),
		LocalPath:        filepath.Join(dir, "local.yaml"),
	}

	for b.Loop() {
		_ = LoadEffective(context.Background(), opts)
	}
}

func BenchmarkLoadEffectiveLargeConfig(b *testing.B) {
	dir := b.TempDir()
	writeBenchConfig(b, dir, 50)
	opts := LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "org.yaml"),
		GlobalPath:       filepath.Join(dir, "user.yaml"),
		RepoPath:         filepath.Join(dir, "repo.yaml"),
		LocalPath:        filepath.Join(dir, "local.yaml"),
	}

	for b.Loop() {
		_ = LoadEffective(context.Background(), opts)
	}
}

func writeBenchConfig(b testing.TB, dir string, n int) {
	b.Helper()
	org := "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\nallowed_registries:\n  - ^gitlab\\.example\\.com/\nregistries:\n"
	user := "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\nregistries:\n"
	repo := "apiVersion: agent-marketplace/v3\nregistries:\n"
	local := "apiVersion: agent-marketplace/v3\nregistries:\n"

	for i := 0; i < n; i++ {
		url := fmt.Sprintf("git@gitlab.example.com:example-org/repo-%d.git", i)
		entry := fmt.Sprintf("  - url: %s\n    ref: main\n    skills:\n      include:\n        - skill-%d\n", url, i)
		org += entry
		user += entry
		repo += entry
		local += entry
	}

	writeBenchFile(b, filepath.Join(dir, "org.yaml"), org)
	writeBenchFile(b, filepath.Join(dir, "user.yaml"), user)
	writeBenchFile(b, filepath.Join(dir, "repo.yaml"), repo)
	writeBenchFile(b, filepath.Join(dir, "local.yaml"), local)
}

func writeBenchFile(b testing.TB, path, content string) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatalf("write file: %v", err)
	}
}
