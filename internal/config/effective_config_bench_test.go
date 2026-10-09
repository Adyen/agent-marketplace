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
		_ = loadEffectiveForTest(context.Background(), opts)
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
		_ = loadEffectiveForTest(context.Background(), opts)
	}
}

func BenchmarkOrganizationIncludeTreeDiskReads(b *testing.B) {
	dir := b.TempDir()
	root := filepath.Join(dir, "authority", "config.yaml")
	middle := filepath.Join(dir, "external", "middle.yaml")
	writeBenchFile(b, filepath.Join(dir, "external", "nested", "leaf.yaml"), "registries:\n  - path: /live-registry\n    skills: {include: [review]}\n")
	writeBenchFile(b, middle, "include: nested/leaf.yaml\n")
	writeBenchFile(b, root, "apiVersion: agent-marketplace/v2\ninclude: "+middle+"\n")
	b.ReportAllocs()
	for b.Loop() {
		data, err := readConfigFile(root)
		if err != nil {
			b.Fatal(err)
		}
		li, err := readLayerIntent(root, "", data, organizationLayer, nil)
		if err != nil {
			b.Fatal(err)
		}
		cfg, err := li.bind(nil)
		if err != nil || len(cfg.Registries) != 1 {
			b.Fatalf("load organization include tree: %v", err)
		}
	}
}

func BenchmarkV4EmptyPolicyComposition(b *testing.B) {
	dir := b.TempDir()
	root := filepath.Join(dir, "organization.yaml")
	writeBenchFile(b, filepath.Join(dir, "policy.yaml"), "allowed_registries: []\nallowed_marketplaces: []\n")
	writeBenchFile(b, root, "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\nharnesses: [devin]\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		data, err := readConfigFile(root)
		if err != nil {
			b.Fatal(err)
		}
		layer, err := readLayerIntent(root, "", data, organizationLayer, nil)
		if err != nil {
			b.Fatal(err)
		}
		settings := layer.Settings()
		if settings.AllowedRegistries == nil || settings.AllowedMarketplaces == nil ||
			len(*settings.AllowedRegistries) != 0 || len(*settings.AllowedMarketplaces) != 0 {
			b.Fatal("composed fixture must retain both declared empty policies")
		}
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
