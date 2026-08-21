package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	pkgcatalog "github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/registryfs"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// TestE2E exercises the full catalog→resolve→materialize→verify pipeline.
func TestE2E(t *testing.T) {
	// Setup: temp repo root + temp home dir + fake cache with Claude-format skill.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	// Create a Claude-format skill directory in a fake cache location.
	// Claude layout requires: skills/<name>/SKILL.md + .claude-plugin/plugin.json
	cacheDir := t.TempDir()
	skillDir := filepath.Join(cacheDir, "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillMD := `---
name: my-skill
description: A test skill
metadata:
  tags:
    - test
---

# My Skill

This is the body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	// Claude autodetect requires .claude-plugin/plugin.json
	if err := os.MkdirAll(filepath.Join(cacheDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test","version":"1.0"}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	// Load catalog via HCC pipeline.
	cat, err := pkgcatalog.Load(registryfs.Local(cacheDir), string(hcc.TargetClaude))
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("expected 1 catalog entry, got %d", len(cat.Entries))
	}

	// Build catalog entries for resolver.
	catalogEntries := map[string][]pkgcatalog.Entry{
		"test-catalog": cat.Entries,
	}

	// Construct a minimal config for resolver.
	rawCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"claude", "factory"},
		Registries: []config.RegistryEntry{
			{
				URL:        "https://example.com/test.git",
				Ref:        "main",
				Skills:     config.KindSelector{Include: []string{"my-skill"}},
				Source:     config.RepoCommitted,
				CatalogKey: "test-catalog",
			},
		},
	}
	cfg := config.Merge(nil, rawCfg)
	if len(cfg.Registries) > 0 {
		cfg.Registries[0].CatalogKey = "test-catalog"
	}

	// Build resource map for resolver.WithResources fallback.
	resourceCatalogs := map[string]map[string]ir.Resource{
		"test-catalog": pkgcatalog.Resources(cat),
	}

	resolved, resolveErrs := resolver.Resolve(cfg, catalogEntries, nil, resolver.WithResources(resourceCatalogs))
	if len(resolveErrs) > 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved skill, got %d", len(resolved))
	}
	if resolved[0].Resource == nil {
		t.Fatal("expected Resource to be populated")
	}

	// Step 4: materialize.Run → assert both targets have correct files.
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("materialize.Run: %v", err)
	}

	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetFactory} {
		tr, ok := report.PerTarget[target]
		if !ok {
			t.Fatalf("expected target report for %s", target)
		}
		if len(tr.Written) == 0 {
			t.Fatalf("target %s: expected at least one written file", target)
		}
	}

	// Step 5: Run again → Skipped (idempotent).
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second materialize.Run: %v", err)
	}
	if report2.TotalWritten() != 0 {
		t.Fatalf("second run: expected 0 written, got %d", report2.TotalWritten())
	}
	if report2.TotalSkipped() == 0 {
		t.Fatalf("second run: expected some skipped, got %d", report2.TotalSkipped())
	}

	// Step 6: Remove skill from list, Run again → stale files Removed.
	report3, err := Run(context.Background(), repoRoot, nil) // no skills
	if err != nil {
		t.Fatalf("third materialize.Run: %v", err)
	}
	totalRemoved := report3.TotalRemoved()
	if totalRemoved == 0 {
		t.Fatal("expected stale files to be removed when no skills are resolved")
	}
}
