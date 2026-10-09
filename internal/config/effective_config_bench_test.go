package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadEffectiveSmall(b *testing.B) {
	benchmarkLoadEffective(b, 2)
}

func BenchmarkLoadEffectiveLargeConfig(b *testing.B) {
	benchmarkLoadEffective(b, 50)
}

func benchmarkLoadEffective(b *testing.B, n int) {
	b.Helper()
	dir := b.TempDir()
	writeBenchConfig(b, dir, n)
	opts := LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "org.yaml"),
		GlobalPath:       filepath.Join(dir, "user.yaml"),
		RepoPath:         filepath.Join(dir, "repo.yaml"),
		LocalPath:        filepath.Join(dir, "local.yaml"),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ec := loadEffectiveForTest(context.Background(), opts)
		if len(ec.Errors) != 0 || ec.Merged == nil {
			b.Fatalf("effective config: errors=%v merged=%#v", ec.Errors, ec.Merged)
		}
		if len(ec.Merged.Registries) != 4*n {
			b.Fatalf("effective registry count=%d, want %d", len(ec.Merged.Registries), 4*n)
		}
	}
}

func BenchmarkOrganizationIncludeTreeDiskReads(b *testing.B) {
	dir := b.TempDir()
	root := filepath.Join(dir, "authority", "config.yaml")
	middle := filepath.Join(dir, "external", "middle.yaml")
	writeBenchFile(b, filepath.Join(dir, "external", "nested", "leaf.yaml"), "sources: {live: {directory: /live-registry}}\nresources: {live: [skill/review]}\n")
	writeBenchFile(b, middle, "include: nested/leaf.yaml\n")
	writeBenchFile(b, root, "apiVersion: agent-marketplace/v4\ninclude: "+middle+"\n")
	b.ReportAllocs()
	b.ResetTimer()
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
		if err != nil || len(cfg.Registries) != 1 || cfg.Registries[0].Skills.Include[0] != "review" {
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
	// Authored Git selections exercise configuration I/O without acquisition.
	// Equal origins in different layers still preserve four provenance entries.
	for _, layer := range []string{"org", "user", "repo", "local"} {
		data := "apiVersion: agent-marketplace/v4\n"
		if layer != "local" {
			data += "harnesses: [factory]\n"
		}
		if layer == "org" {
			data += "allowed_registries: ['^gitlab\\.example\\.com/']\n"
		}
		data += "sources:\n"
		for i := 0; i < n; i++ {
			data += fmt.Sprintf("  registry-%d: {git: git@gitlab.example.com:example-org/repo-%d.git, ref: main}\n", i, i)
		}
		data += "resources:\n"
		for i := 0; i < n; i++ {
			data += fmt.Sprintf("  registry-%d: [skill/skill-%d]\n", i, i)
		}
		writeBenchFile(b, filepath.Join(dir, layer+".yaml"), data)
	}
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

func BenchmarkReadOrganizationIntentV4Include(b *testing.B) {
	dir := b.TempDir()
	root := filepath.Join(dir, "organization.yaml")
	writeBenchFile(b, filepath.Join(dir, "fragment.yaml"), "telemetry: {level: off}\n")
	writeBenchFile(b, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [claude]\n")
	SetTestOrganizationConfigPath(b, root)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		layer, _ := readOrganizationIntent(root)
		if err := layer.Err(); err != nil {
			b.Fatal(err)
		}
		if !layer.Present() || len(layer.Reads) != 2 {
			b.Fatal("benchmark fixture must read root and included fragment")
		}
	}
}
