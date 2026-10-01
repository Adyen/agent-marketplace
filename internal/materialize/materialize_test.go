package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// hasDiagCode returns true if diags contains a diagnostic with the given code.
func hasDiagCode(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestRunEmptyResolvedList(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	report, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	// With no resolved skills and no existing output directories,
	// there should be no per-target reports (pruning is only added
	// when an output directory already exists).
	// If the default Claude output dir already existed on disk,
	// a report entry would be present — but since repoRoot is a
	// temp dir, .claude shouldn't exist yet.
	for _, tr := range report.PerTarget {
		if len(tr.Written) != 0 || len(tr.Removed) != 0 {
			t.Fatalf("expected no written/removed for empty list, got Written=%d Removed=%d",
				len(tr.Written), len(tr.Removed))
		}
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %d", len(report.Diags))
	}
}

func TestRunResourceNilSkipped(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "no-resource", Resource: nil},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The Claude target is still created for pruning even with nil resource.
	// No files should be written or removed.
	tr, hasTarget := report.PerTarget[hcc.TargetClaude]
	if hasTarget {
		if len(tr.Written) != 0 || len(tr.Removed) != 0 {
			t.Fatalf("expected no written/removed files for nil resource, got Written=%d Removed=%d",
				len(tr.Written), len(tr.Removed))
		}
	}

	// Should have a warning diagnostic about nil resource
	hasWarning := false
	for _, d := range report.Diags {
		if d.Code == "materialize.resource.nil" && d.Severity == diag.SeverityWarning {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Fatal("expected warning diagnostic for nil resource")
	}
}

func TestMarketplaceInstalledNameCompilesWithoutMutatingCanonicalResource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	canonical := ir.NewSkill("original", "Original skill", "Original body", nil, nil, ir.SourceRef{})
	resolved := resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "review-copy", Resource: canonical},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
		IsMarketplace: true, MarketplaceResourceName: "original", InstalledName: "review-copy",
	}
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatal(err)
	}
	if hasDiagCode(report.Diags, "materialize.marketplace.alias-invalid") {
		t.Fatalf("aliased materialization diagnostics=%v", report.Diags)
	}
	content, err := os.ReadFile(filepath.Join(repo, ".factory", "skills", "review-copy", "SKILL.md"))
	if err != nil || !strings.Contains(string(content), "name: review-copy") || !strings.Contains(string(content), "Original body") {
		t.Fatalf("aliased skill content = %q, %v", content, err)
	}
	if canonical.Name() != "original" {
		t.Fatalf("compiler changed verified canonical name to %q", canonical.Name())
	}
	mcp := ir.NewMcpServer("original-server", "echo", nil, nil, nil, ir.SourceRef{})
	mcpRepo := t.TempDir()
	mcpResolved := resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Kind: ir.KindMcpServer, Name: "server-copy", Resource: mcp},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
		IsMarketplace: true, MarketplaceResourceName: "original-server", InstalledName: "server-copy",
	}
	report, err = Run(context.Background(), mcpRepo, []resolver.ResolvedArtifact{mcpResolved})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(mcpRepo, ".factory", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Servers["server-copy"]; !ok || doc.Servers["original-server"] != nil || mcp.Name() != "original-server" {
		t.Fatalf("aliased MCP output=%s, canonical=%q, diagnostics=%v", data, mcp.Name(), report.Diags)
	}
	parent := t.TempDir()
	invalidRoot := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(invalidRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "sentinel")
	if err := os.WriteFile(outside, []byte("handwritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), invalidRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(filepath.Join(invalidRoot, ".factory", "skills", "review-copy", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	resolved.Name, resolved.InstalledName = "../../../sentinel", "../../../sentinel"
	report, err = Run(context.Background(), invalidRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.marketplace.alias-invalid") {
		t.Fatalf("invalid installed name diagnostics=%v", report.Diags)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "handwritten" {
		t.Fatalf("invalid alias changed outside sentinel: %q, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(invalidRoot, ".factory", "skills", "sentinel")); !os.IsNotExist(err) {
		t.Fatalf("invalid alias produced an output: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(invalidRoot, ".factory", "skills", "review-copy", "SKILL.md")); err != nil || string(got) != string(previous) {
		t.Fatalf("invalid alias pruned previously valid managed output: %q, %v", got, err)
	}
}

func TestRunValidResourceOneTarget(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("test-skill", "A test skill", "Test body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "test-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tr, ok := report.PerTarget[hcc.TargetClaude]
	if !ok {
		t.Fatal("expected Claude target report")
	}

	if len(tr.Written) == 0 {
		t.Fatalf("expected at least one written file, got %d", len(tr.Written))
	}

	// Verify the output directory exists and contains files
	claudeDir := filepath.Join(repoRoot, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Fatalf("expected .claude directory to exist at %s", claudeDir)
	}
}

func TestRunIdempotentSecondRunSkips(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("idempotent-skill", "Idempotent test", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "idempotent-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	// First run: writes
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatalf("first run: expected at least one written file")
	}

	// Second run: should skip (same content, sha256 match)
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}
	if report2.TotalWritten() != 0 {
		t.Fatalf("second run: expected 0 written (idempotent), got %d", report2.TotalWritten())
	}
	if report2.TotalSkipped() == 0 {
		t.Fatalf("second run: expected at least one skipped file, got %d", report2.TotalSkipped())
	}
}

func TestRunNilHarnessesDefaultsToClaude(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("default-target-skill", "Default target", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "default-target-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: nil, // nil defaults to [TargetClaude]
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, ok := report.PerTarget[hcc.TargetClaude]
	if !ok {
		t.Fatal("expected Claude target report when Harnesses is nil")
	}
}

func TestRunEmptyHarnessesSkipsSkill(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	irSkill := ir.NewSkill("opted-out-skill", "Opted out", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "opted-out-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{}, // empty = explicitly opted out
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The skill is opted out but the default target may appear for pruning.
	// The key assertion: no files should be written for this skill.
	for _, tr := range report.PerTarget {
		if len(tr.Written) != 0 {
			t.Fatalf("expected no written files for opted-out skill, got %v", tr.Written)
		}
	}
}

func TestRunMultipleTargets(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("multi-target-skill", "Multi target", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "multi-target-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude, hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetFactory} {
		tr, ok := report.PerTarget[target]
		if !ok {
			t.Fatalf("expected %s target report", target)
		}
		if len(tr.Written) == 0 {
			t.Fatalf("expected at least one written file for %s", target)
		}
	}
}

func TestRunPruneStaleFiles(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// First: materialize a skill
	irSkill1 := ir.NewSkill("keep-skill", "Keep", "Body", nil, nil, ir.SourceRef{})
	resolved1 := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "keep-skill", Resource: irSkill1},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved1)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	// Write a stale managed file (carries the ownership marker) in the Claude
	// output directory — this must be pruned.
	claudeDir := filepath.Join(repoRoot, ".claude")
	staleFile := filepath.Join(claudeDir, "skills", "stale-output.md")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	staleManaged := "---\n" + managedByField + ": " + managedByValue + "\n---\n\nstale\n"
	if err := os.WriteFile(staleFile, []byte(staleManaged), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	// Write a user-owned file (no ownership marker) — this must be preserved.
	userFile := filepath.Join(claudeDir, "skills", "my-notes.md")
	if err := os.WriteFile(userFile, []byte("# my notes\n"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	// Second run: same skill; stale managed file removed, user file preserved.
	report2, err := Run(context.Background(), repoRoot, resolved1)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// Stale managed file should have been removed
	removed := slices.Contains(report2.PerTarget[hcc.TargetClaude].Removed, filepath.Join("skills", "stale-output.md"))
	if !removed {
		t.Fatalf("expected skills/stale-output.md to be removed, got Removed=%v", report2.PerTarget[hcc.TargetClaude].Removed)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatal("expected stale managed file to be removed from disk")
	}

	// User-owned file must survive.
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("expected user-owned file to be preserved, got err=%v", err)
	}
}

func TestRunPrunesArtifactsForRemovedHarness(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("shared-skill", "Shared", "Body", nil, nil, ir.SourceRef{})
	resolvedWithFactoryAndWindsurf := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "shared-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory, hcc.TargetWindsurf},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolvedWithFactoryAndWindsurf)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	windsurfSkillLink := filepath.Join(repoRoot, ".windsurf", "skills", "shared-skill")
	if _, err := os.Lstat(windsurfSkillLink); err != nil {
		t.Fatalf("expected windsurf managed skill link to exist: %v", err)
	}

	// Add a user-owned (non-symlink) skill directory to verify FIX-001
	// protections still apply when pruning a removed harness.
	userSkillMD := filepath.Join(repoRoot, ".windsurf", "skills", "my-manual-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userSkillMD), 0o755); err != nil {
		t.Fatalf("mkdir user skill: %v", err)
	}
	if err := os.WriteFile(userSkillMD, []byte("# Manual skill\n"), 0o644); err != nil {
		t.Fatalf("write user skill: %v", err)
	}

	// Simulate removing windsurf from config.harnesses and syncing again.
	resolvedWithFactoryOnly := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "shared-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report2, err := Run(context.Background(), repoRoot, resolvedWithFactoryOnly)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	if _, ok := report2.PerTarget[hcc.TargetWindsurf]; !ok {
		t.Fatal("expected windsurf target report when pruning removed harness artifacts")
	}
	if _, err := os.Lstat(windsurfSkillLink); !os.IsNotExist(err) {
		t.Fatalf("expected stale windsurf managed skill to be removed, got err=%v", err)
	}
	if _, err := os.Stat(userSkillMD); os.IsNotExist(err) {
		t.Fatal("expected user-owned windsurf skill directory to be preserved")
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "shared-skill")); err != nil {
		t.Fatalf("expected factory managed skill to remain: %v", err)
	}
}

func TestRunDoesNotPruneHarnessOwnedRootFiles(t *testing.T) {
	tests := []struct {
		name      string
		target    hcc.Target
		rootDir   string
		ownedFile string
	}{
		{name: "claude", target: hcc.TargetClaude, rootDir: ".claude", ownedFile: "settings.json"},
		{name: "factory", target: hcc.TargetFactory, rootDir: ".factory", ownedFile: "settings.json"},
		{name: "cursor", target: hcc.TargetCursor, rootDir: ".cursor", ownedFile: "settings.json"},
		{name: "gemini", target: hcc.TargetGemini, rootDir: ".gemini", ownedFile: "settings.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			irSkill := ir.NewSkill("keep-skill", "Keep", "Body", nil, nil, ir.SourceRef{})
			resolved := []resolver.ResolvedArtifact{
				{
					Entry:     catalog.Entry{Name: "keep-skill", Resource: irSkill},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{tt.target},
				},
			}

			report1, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("first run: expected no error, got %v", err)
			}
			if report1.TotalWritten() == 0 {
				t.Fatal("first run: expected files to be written")
			}

			harnessDir := filepath.Join(repoRoot, tt.rootDir)
			ownedPath := filepath.Join(harnessDir, tt.ownedFile)
			if err := os.WriteFile(ownedPath, []byte("keep"), 0o644); err != nil {
				t.Fatalf("write owned file: %v", err)
			}
			// Non-symlink skill directories are user-owned and must be preserved.
			staleGeneratedPath := filepath.Join(harnessDir, "skills", "stale-generated", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(staleGeneratedPath), 0o755); err != nil {
				t.Fatalf("mkdir stale generated file: %v", err)
			}
			if err := os.WriteFile(staleGeneratedPath, []byte("stale"), 0o644); err != nil {
				t.Fatalf("write stale generated file: %v", err)
			}
			// Also create a stale managed symlink skill to verify managed
			// symlinks are still pruned. The symlink must target the cache
			// directory so it is recognized as agent-marketplace-managed.
			cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
			staleSymlinkPath := filepath.Join(harnessDir, "skills", "stale-symlink")
			if err := os.Symlink(filepath.Join(cacheDir, "compiled", "stale"), staleSymlinkPath); err != nil {
				t.Fatalf("create stale managed symlink: %v", err)
			}
			// Also create a user-owned symlink that should be preserved.
			userSymlinkPath := filepath.Join(harnessDir, "skills", "user-skill-link")
			if err := os.Symlink("/nonexistent", userSymlinkPath); err != nil {
				t.Fatalf("create user symlink: %v", err)
			}

			report2, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("second run: expected no error, got %v", err)
			}
			if _, err := os.Stat(ownedPath); err != nil {
				t.Fatalf("expected harness-owned root file to remain: %v", err)
			}
			// Non-symlink skill directories are user-owned and preserved.
			if _, err := os.Stat(staleGeneratedPath); os.IsNotExist(err) {
				t.Fatalf("expected non-symlink skill directory to be preserved (user-owned)")
			}
			// Stale managed symlink skills should still be pruned.
			if _, err := os.Lstat(staleSymlinkPath); !os.IsNotExist(err) {
				t.Fatalf("expected stale managed symlink skill to be removed")
			}
			// User-owned symlinks must be preserved.
			if _, err := os.Lstat(userSymlinkPath); os.IsNotExist(err) {
				t.Fatalf("expected user-owned symlink skill to be preserved")
			}
			if slices.Contains(report2.PerTarget[tt.target].Removed, tt.ownedFile) {
				t.Fatalf("expected root file not to be reported removed, got Removed=%v", report2.PerTarget[tt.target].Removed)
			}
		})
	}
}

func TestRunGeminiSkillRebasesGeneratedSubtree(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irSkill := ir.NewSkill("gemini-skill", "Gemini", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "gemini-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetGemini},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.TotalWritten() == 0 {
		t.Fatal("expected files to be written")
	}

	expectedPath := filepath.Join(repoRoot, ".gemini", "skills", "gemini-skill", "SKILL.md")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected gemini skill at %s: %v", expectedPath, err)
	}
	nestedPath := filepath.Join(repoRoot, ".gemini", ".gemini", "skills", "gemini-skill", "SKILL.md")
	if _, err := os.Stat(nestedPath); !os.IsNotExist(err) {
		t.Fatalf("expected no nested gemini skill at %s, got err=%v", nestedPath, err)
	}
}

func TestRunSkillSymlinksPointToAgentMarketplaceCacheForEveryTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   hcc.Target
		linkPath func(repoRoot, homeDir, name string) string
	}{
		{
			name:   "claude",
			target: hcc.TargetClaude,
			linkPath: func(repoRoot, homeDir, name string) string {
				return filepath.Join(repoRoot, ".claude", "skills", name)
			},
		},
		{
			name:   "factory",
			target: hcc.TargetFactory,
			linkPath: func(repoRoot, homeDir, name string) string {
				return filepath.Join(repoRoot, ".factory", "skills", name)
			},
		},
		{
			name:   "cursor",
			target: hcc.TargetCursor,
			linkPath: func(repoRoot, homeDir, name string) string {
				return filepath.Join(repoRoot, ".cursor", "skills", name)
			},
		},
		{
			name:   "windsurf",
			target: hcc.TargetWindsurf,
			linkPath: func(repoRoot, homeDir, name string) string {
				return filepath.Join(repoRoot, ".windsurf", "skills", name)
			},
		},
		{
			name:   "gemini",
			target: hcc.TargetGemini,
			linkPath: func(repoRoot, homeDir, name string) string {
				return filepath.Join(repoRoot, ".gemini", "skills", name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			sourceSkillDir := filepath.Join(t.TempDir(), tt.name+"-skill")
			if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
				t.Fatalf("mkdir source skill: %v", err)
			}
			if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("${CLAUDE_PLUGIN_ROOT}"), 0o644); err != nil {
				t.Fatalf("write source skill: %v", err)
			}
			skillName := tt.name + "-skill"
			resolved := []resolver.ResolvedArtifact{
				{
					Entry:     catalog.Entry{Name: skillName, Path: sourceSkillDir, Resource: ir.NewSkill(skillName, tt.name+" skill", "${CLAUDE_PLUGIN_ROOT}", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{tt.target},
				},
			}

			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if len(report.Diags) != 0 {
				t.Fatalf("expected no diagnostics, got %v", report.Diags)
			}

			linkPath := tt.linkPath(repoRoot, homeDir, skillName)
			target, ok := existingSymlinkTarget(linkPath)
			if !ok {
				t.Fatalf("expected %s to be a symlink", linkPath)
			}
			cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
			if target != cacheDir && !strings.HasPrefix(target, cacheDir+string(os.PathSeparator)) {
				t.Fatalf("expected symlink target under %q, got %q", cacheDir, target)
			}
			if target == sourceSkillDir {
				t.Fatalf("expected symlink target to be cache-backed, got source path %q", target)
			}
		})
	}
}

func TestRunGeminiSkillSymlinksNativeCache(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheSkillDir := filepath.Join(t.TempDir(), "gemini-skill")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("${CLAUDE_PLUGIN_ROOT}"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}

	irSkill := ir.NewSkill("gemini-skill", "Gemini", "${CLAUDE_PLUGIN_ROOT}", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "gemini-skill", Path: cacheSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetGemini},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	geminiSkillPath := filepath.Join(repoRoot, ".gemini", "skills", "gemini-skill")
	target := assertSymlinkTargetUnderCache(t, geminiSkillPath, homeDir)
	body, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized Gemini skill: %v", err)
	}
	// Skill compilation now renders YAML frontmatter (name, description)
	// before the body content.
	if !strings.Contains(string(body), "${CLAUDE_PLUGIN_ROOT}") {
		t.Fatalf("expected Gemini materialization to contain skill body, got %q", body)
	}
	if !strings.HasPrefix(string(body), "---\n") {
		t.Fatalf("expected Gemini skill to start with frontmatter, got %q", string(body)[:min(len(string(body)), 80)])
	}
	if !strings.Contains(string(body), "name: gemini-skill") {
		t.Fatalf("expected Gemini skill frontmatter to contain name, got %q", body)
	}
}

func TestRunWindsurfMachineScopeDoesNotPruneHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	trashDir := filepath.Join(homeDir, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	if err := os.Chmod(trashDir, 0); err != nil {
		t.Fatalf("chmod trash: %v", err)
	}
	defer func() {
		_ = os.Chmod(trashDir, 0o755)
	}()
	homeFile := filepath.Join(homeDir, "keep.txt")
	if err := os.WriteFile(homeFile, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write home file: %v", err)
	}

	irSkill := ir.NewSkill("windsurf-global", "Windsurf global", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "windsurf-global", Resource: irSkill},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codeium", "windsurf", "skills")); err != nil {
		t.Fatalf("expected ~/.codeium/windsurf/skills to exist: %v", err)
	}
	if _, err := os.Stat(homeFile); err != nil {
		t.Fatalf("expected unrelated home file to remain: %v", err)
	}
	for _, tr := range report.TargetReports() {
		if tr.Target == hcc.TargetWindsurf && tr.OutDir == homeDir {
			t.Fatalf("expected windsurf scoped output dir, got home dir")
		}
	}
}

func TestRunWindsurfRepoScopeDoesNotPruneRepoRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	trashDir := filepath.Join(repoRoot, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	if err := os.Chmod(trashDir, 0); err != nil {
		t.Fatalf("chmod trash: %v", err)
	}
	defer func() {
		_ = os.Chmod(trashDir, 0o755)
	}()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	irSkill := ir.NewSkill("windsurf-repo", "Windsurf repo", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "windsurf-repo", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".windsurf")); err != nil {
		t.Fatalf("expected repo .windsurf to exist: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected repo config to remain: %v", err)
	}
	for _, tr := range report.TargetReports() {
		if tr.Target == hcc.TargetWindsurf && tr.OutDir == repoRoot {
			t.Fatalf("expected windsurf scoped output dir, got repo root")
		}
	}
}

func TestRunWindsurfMachineScopeSymlinksSkillToCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	cacheSkillDir := filepath.Join(t.TempDir(), "windsurf-global")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "windsurf-global", Path: cacheSkillDir, Resource: ir.NewSkill("windsurf-global", "Windsurf global", "Body", nil, nil, ir.SourceRef{})},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	linkPath := filepath.Join(homeDir, ".codeium", "windsurf", "skills", "windsurf-global")
	assertSymlinkTargetUnderCache(t, linkPath, homeDir)
	content, err := os.ReadFile(filepath.Join(linkPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read windsurf skill: %v", err)
	}
	if !strings.HasPrefix(string(content), "---\nname: windsurf-global\n") {
		t.Fatalf("expected windsurf skill frontmatter, got %q", string(content))
	}
}

func TestRunWindsurfPreservesUserOwnedSkillDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	cacheSkillDir := filepath.Join(t.TempDir(), "windsurf-global")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}
	materializedDir := filepath.Join(homeDir, ".codeium", "windsurf", "skills", "windsurf-global")
	if err := os.MkdirAll(materializedDir, 0o755); err != nil {
		t.Fatalf("mkdir materialized skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(materializedDir, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write materialized skill: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "windsurf-global", Path: cacheSkillDir, Resource: ir.NewSkill("windsurf-global", "Windsurf global", "Body", nil, nil, ir.SourceRef{})},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The pre-existing non-symlink directory is user-owned and must NOT be replaced.
	info, statErr := os.Lstat(materializedDir)
	if statErr != nil {
		t.Fatalf("expected user-owned skill directory to still exist: %v", statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected user-owned skill directory to remain a directory, got symlink")
	}
	// The original content should be preserved.
	content, readErr := os.ReadFile(filepath.Join(materializedDir, "SKILL.md"))
	if readErr != nil {
		t.Fatalf("read user skill SKILL.md: %v", readErr)
	}
	if string(content) != "old" {
		t.Fatalf("expected user skill content preserved, got %q", content)
	}
	// A collision diagnostic should be emitted.
	var collisionDiags []diag.Diagnostic
	for _, d := range report.Diags {
		if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, "windsurf-global") {
			collisionDiags = append(collisionDiags, d)
		}
	}
	if len(collisionDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic for windsurf-global")
	}
}

func TestRunPrunesRemovedUserScopedSkillLinksForFactoryAndWindsurf(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	irSkill := ir.NewSkill("user-skill", "User skill", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "user-skill", Resource: irSkill},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetFactory, hcc.TargetWindsurf},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	factoryLink := filepath.Join(homeDir, ".factory", "skills", "user-skill")
	if _, err := os.Lstat(factoryLink); err != nil {
		t.Fatalf("expected machine-scoped factory skill link to exist: %v", err)
	}
	windsurfLink := filepath.Join(homeDir, ".codeium", "windsurf", "skills", "user-skill")
	if _, err := os.Lstat(windsurfLink); err != nil {
		t.Fatalf("expected machine-scoped windsurf skill link to exist: %v", err)
	}

	userWindsurfSkillMD := filepath.Join(homeDir, ".codeium", "windsurf", "skills", "my-manual-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userWindsurfSkillMD), 0o755); err != nil {
		t.Fatalf("mkdir user windsurf skill: %v", err)
	}
	if err := os.WriteFile(userWindsurfSkillMD, []byte("# My manual windsurf skill\n"), 0o644); err != nil {
		t.Fatalf("write user windsurf skill: %v", err)
	}

	report2, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	if _, ok := report2.PerTarget[hcc.TargetFactory]; !ok {
		t.Fatal("expected factory target report when pruning removed machine-scoped skills")
	}
	if _, ok := report2.PerTarget[hcc.TargetWindsurf]; !ok {
		t.Fatal("expected windsurf target report when pruning removed machine-scoped skills")
	}

	if _, err := os.Lstat(factoryLink); !os.IsNotExist(err) {
		t.Fatalf("expected stale machine-scoped factory skill link to be removed, got err=%v", err)
	}
	if _, err := os.Lstat(windsurfLink); !os.IsNotExist(err) {
		t.Fatalf("expected stale machine-scoped windsurf skill link to be removed, got err=%v", err)
	}
	if _, err := os.Stat(userWindsurfSkillMD); os.IsNotExist(err) {
		t.Fatal("expected user-owned windsurf skill directory to be preserved")
	}

	if !slices.Contains(report2.PerTarget[hcc.TargetFactory].Removed, "skills/user-skill") {
		t.Fatalf("expected skills/user-skill in factory removed entries, got %v", report2.PerTarget[hcc.TargetFactory].Removed)
	}
	if !slices.Contains(report2.PerTarget[hcc.TargetWindsurf].Removed, "skills/user-skill") {
		t.Fatalf("expected skills/user-skill in windsurf removed entries, got %v", report2.PerTarget[hcc.TargetWindsurf].Removed)
	}
}

func TestRunClaudeRepoScopeSymlinksSkillToCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	cacheSkillDir := filepath.Join(t.TempDir(), "claude-repo")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "claude-repo", Path: cacheSkillDir, Resource: ir.NewSkill("claude-repo", "Claude repo", "Body", nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	linkPath := filepath.Join(repoRoot, ".claude", "skills", "claude-repo")
	assertSymlinkTargetUnderCache(t, linkPath, homeDir)
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("expected non-skill Claude artifacts to remain materialized: %v", err)
	}
}

func TestRunClaudePreservesUserOwnedSkillDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	cacheSkillDir := filepath.Join(t.TempDir(), "claude-repo")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}
	materializedDir := filepath.Join(repoRoot, ".claude", "skills", "claude-repo")
	if err := os.MkdirAll(materializedDir, 0o755); err != nil {
		t.Fatalf("mkdir materialized skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(materializedDir, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write materialized skill: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "claude-repo", Path: cacheSkillDir, Resource: ir.NewSkill("claude-repo", "Claude repo", "Body", nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// The pre-existing non-symlink directory is user-owned and must NOT be replaced.
	info, statErr := os.Lstat(materializedDir)
	if statErr != nil {
		t.Fatalf("expected user-owned skill directory to still exist: %v", statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected user-owned skill directory to remain a directory, got symlink")
	}
	content, readErr := os.ReadFile(filepath.Join(materializedDir, "SKILL.md"))
	if readErr != nil {
		t.Fatalf("read user skill SKILL.md: %v", readErr)
	}
	if string(content) != "old" {
		t.Fatalf("expected user skill content preserved, got %q", content)
	}
	// A collision diagnostic should be emitted.
	var collisionDiags []diag.Diagnostic
	for _, d := range report.Diags {
		if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, "claude-repo") {
			collisionDiags = append(collisionDiags, d)
		}
	}
	if len(collisionDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic for claude-repo")
	}
}

func TestRunFactoryAndCursorSymlinkNativeSkillsToCache(t *testing.T) {
	tests := []struct {
		name                string
		target              hcc.Target
		rootDir             string
		nonSkillArtifactRel string
	}{
		{name: "factory", target: hcc.TargetFactory, rootDir: ".factory", nonSkillArtifactRel: ""},
		{name: "cursor", target: hcc.TargetCursor, rootDir: ".cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			cacheSkillDir := filepath.Join(t.TempDir(), tt.name+"-repo")
			if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
				t.Fatalf("mkdir cache skill: %v", err)
			}
			if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
				t.Fatalf("write cache skill: %v", err)
			}

			resolved := []resolver.ResolvedArtifact{
				{
					Entry:     catalog.Entry{Name: tt.name + "-repo", Path: cacheSkillDir, Resource: ir.NewSkill(tt.name+"-repo", tt.name+" repo", "Body", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{tt.target},
				},
			}

			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if len(report.Diags) != 0 {
				t.Fatalf("expected no diagnostics, got %v", report.Diags)
			}

			linkPath := filepath.Join(repoRoot, tt.rootDir, "skills", tt.name+"-repo")
			assertSymlinkTargetUnderCache(t, linkPath, homeDir)
			if tt.nonSkillArtifactRel != "" {
				if _, err := os.Stat(filepath.Join(repoRoot, tt.rootDir, tt.nonSkillArtifactRel)); err != nil {
					t.Fatalf("expected non-skill artifacts to remain materialized: %v", err)
				}
			}
		})
	}
}

func TestRunFactoryAndCursorPreserveUserOwnedSkillDirectory(t *testing.T) {
	tests := []struct {
		name    string
		target  hcc.Target
		rootDir string
	}{
		{name: "factory", target: hcc.TargetFactory, rootDir: ".factory"},
		{name: "cursor", target: hcc.TargetCursor, rootDir: ".cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			cacheSkillDir := filepath.Join(t.TempDir(), tt.name+"-repo")
			if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
				t.Fatalf("mkdir cache skill: %v", err)
			}
			if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
				t.Fatalf("write cache skill: %v", err)
			}
			materializedDir := filepath.Join(repoRoot, tt.rootDir, "skills", tt.name+"-repo")
			if err := os.MkdirAll(materializedDir, 0o755); err != nil {
				t.Fatalf("mkdir materialized skill: %v", err)
			}
			if err := os.WriteFile(filepath.Join(materializedDir, "SKILL.md"), []byte("old"), 0o644); err != nil {
				t.Fatalf("write materialized skill: %v", err)
			}

			resolved := []resolver.ResolvedArtifact{
				{
					Entry:     catalog.Entry{Name: tt.name + "-repo", Path: cacheSkillDir, Resource: ir.NewSkill(tt.name+"-repo", tt.name+" repo", "Body", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{tt.target},
				},
			}

			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			// The pre-existing non-symlink directory is user-owned and must NOT be replaced.
			info, statErr := os.Lstat(materializedDir)
			if statErr != nil {
				t.Fatalf("expected user-owned skill directory to still exist: %v", statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("expected user-owned skill directory to remain a directory, got symlink")
			}
			content, readErr := os.ReadFile(filepath.Join(materializedDir, "SKILL.md"))
			if readErr != nil {
				t.Fatalf("read user skill SKILL.md: %v", readErr)
			}
			if string(content) != "old" {
				t.Fatalf("expected user skill content preserved, got %q", content)
			}
			// A collision diagnostic should be emitted.
			var collisionDiags []diag.Diagnostic
			for _, d := range report.Diags {
				if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, tt.name+"-repo") {
					collisionDiags = append(collisionDiags, d)
				}
			}
			if len(collisionDiags) == 0 {
				t.Fatalf("expected materialize.collision.user-owned diagnostic for %s-repo", tt.name)
			}
		})
	}
}

func TestRunFactorySubagentSymlinksCompiledDroidToCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("worker-generic", "Worker", "You are a worker", "claude-sonnet-4-5-20250929", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "worker-generic", Path: filepath.Join(t.TempDir(), "worker-generic.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	droidPath := filepath.Join(repoRoot, ".factory", "droids", "worker-generic.md")
	target := assertSymlinkTargetUnderCache(t, droidPath, homeDir)
	if filepath.Base(target) != "worker-generic.md" {
		t.Fatalf("expected droid symlink to target worker-generic.md, got %s", target)
	}
	body, err := os.ReadFile(droidPath)
	if err != nil {
		t.Fatalf("read droid symlink: %v", err)
	}
	if !strings.Contains(string(body), "You are a worker") {
		t.Fatalf("expected compiled droid body, got %q", body)
	}
	// Factory discovers droids from the filesystem, not plugin.json arrays.
	// No plugin.json should be generated.
	pluginPath := filepath.Join(repoRoot, ".factory", ".factory-plugin", "plugin.json")
	if _, err := os.Stat(pluginPath); err == nil {
		t.Fatalf("expected plugin.json to NOT exist; Factory discovers droids from filesystem")
	}
}

// TestRunFactorySubagentPreservesUserDroids covers a regression: syncing a
// managed droid must not delete pre-existing user-authored droids (e.g.
// worker.md) that share the .factory/droids directory. User droids are
// non-symlink regular files without the ownership marker and must be preserved.
func TestRunFactorySubagentPreservesUserDroids(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	droidsDir := filepath.Join(repoRoot, ".factory", "droids")
	if err := os.MkdirAll(droidsDir, 0o755); err != nil {
		t.Fatalf("mkdir droids: %v", err)
	}
	userDroids := []string{"worker.md", "planner.md", "explorer.md"}
	for _, name := range userDroids {
		content := "---\nname: " + strings.TrimSuffix(name, ".md") + "\ndescription: hand-written droid\n---\n\n# user droid\n"
		if err := os.WriteFile(filepath.Join(droidsDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write user droid %s: %v", name, err)
		}
	}

	subagent := ir.NewSubagent("reviewer", "Reviewer", "You are a reviewer", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "reviewer", Path: filepath.Join(t.TempDir(), "reviewer.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Managed droid symlink materialized.
	assertSymlinkTargetUnderCache(t, filepath.Join(droidsDir, "reviewer.md"), homeDir)

	// All user-authored droids must survive.
	for _, name := range userDroids {
		if _, err := os.Stat(filepath.Join(droidsDir, name)); err != nil {
			t.Fatalf("expected user droid %s to be preserved, got err=%v", name, err)
		}
	}

	// None of the user droids should be reported as removed.
	for _, name := range userDroids {
		if slices.Contains(report.PerTarget[hcc.TargetFactory].Removed, name) {
			t.Fatalf("user droid %s was pruned; Removed=%v", name, report.PerTarget[hcc.TargetFactory].Removed)
		}
	}
}

// TestRunFactorySubagentPreservesUserSymlinkDroids verifies that a symlink in
// the .factory/droids directory that was NOT placed by agent-marketplace (i.e.
// its target is not under the agent-marketplace cache) is preserved on sync.
func TestRunFactorySubagentPreservesUserSymlinkDroids(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	droidsDir := filepath.Join(repoRoot, ".factory", "droids")
	if err := os.MkdirAll(droidsDir, 0o755); err != nil {
		t.Fatalf("mkdir droids: %v", err)
	}

	// Create a user symlink pointing outside the agent-marketplace cache.
	externalDir := t.TempDir()
	externalTarget := filepath.Join(externalDir, "custom.md")
	if err := os.WriteFile(externalTarget, []byte("---\nname: custom\n---\n\n# custom\n"), 0o644); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	userLink := filepath.Join(droidsDir, "custom.md")
	if err := os.Symlink(externalTarget, userLink); err != nil {
		t.Fatalf("symlink user droid: %v", err)
	}

	subagent := ir.NewSubagent("reviewer", "Reviewer", "You are a reviewer", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "reviewer", Path: filepath.Join(t.TempDir(), "reviewer.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Managed droid symlink materialized.
	assertSymlinkTargetUnderCache(t, filepath.Join(droidsDir, "reviewer.md"), homeDir)

	// The user symlink must survive and still point to its external target.
	got, err := os.Readlink(userLink)
	if err != nil {
		t.Fatalf("expected user symlink to be preserved, got err=%v", err)
	}
	if got != externalTarget {
		t.Fatalf("expected user symlink target %q, got %q", externalTarget, got)
	}
	if slices.Contains(report.PerTarget[hcc.TargetFactory].Removed, "custom.md") {
		t.Fatalf("user symlink custom.md was pruned; Removed=%v", report.PerTarget[hcc.TargetFactory].Removed)
	}
}

func TestRunFactorySubagentRenormalizationEffortToReasoningEffort(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("effort-agent", "Agent", "Body", "claude-sonnet-4-5-20250929", []string{"Read"}, "high", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "effort-agent", Path: filepath.Join(t.TempDir(), "effort-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, d := range report.Diags {
		if d.IsError() {
			t.Fatalf("unexpected error diagnostic: %v", d)
		}
	}

	body, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "droids", "effort-agent.md"))
	if err != nil {
		t.Fatalf("read droid: %v", err)
	}
	if !strings.Contains(string(body), "reasoningEffort: high") {
		t.Errorf("expected reasoningEffort: high in frontmatter, got:\n%s", body)
	}
	if strings.Contains(string(body), "\neffort:") {
		t.Errorf("frontmatter should not contain effort: (renamed to reasoningEffort), got:\n%s", body)
	}
}

func TestRunFactorySubagentDisallowedToolsDroppedDiagnostic(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("restricted-agent", "Agent", "Body", "sonnet", []string{"Read"}, "", []string{"Bash"}, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "restricted-agent", Path: filepath.Join(t.TempDir(), "restricted-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !hasDiagCode(report.Diags, diag.CodeSubagentFactoryDisallowedToolsDropped) {
		t.Errorf("expected diagnostic %s, got diags: %v", diag.CodeSubagentFactoryDisallowedToolsDropped, report.Diags)
	}

	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "droids", "restricted-agent.md")); !os.IsNotExist(err) {
		t.Fatalf("security-restricted droid artifact exists or stat failed: %v", err)
	}
}

func TestRunFactorySubagentIsolationDroppedDiagnostic(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("iso-agent", "Agent", "Body", "", []string{"Read"}, "", nil, "worktree", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "iso-agent", Path: filepath.Join(t.TempDir(), "iso-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !hasDiagCode(report.Diags, diag.CodeSubagentFactoryIsolationDropped) {
		t.Errorf("expected diagnostic %s, got diags: %v", diag.CodeSubagentFactoryIsolationDropped, report.Diags)
	}

	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "droids", "iso-agent.md")); !os.IsNotExist(err) {
		t.Fatalf("security-restricted droid artifact exists or stat failed: %v", err)
	}
}

func TestRunFactorySubagentNoLossyFieldsNoDiagnostics(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("clean-agent", "Agent", "Body", "sonnet", []string{"Read"}, "high", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "clean-agent", Path: filepath.Join(t.TempDir(), "clean-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hasDiagCode(report.Diags, diag.CodeSubagentFactoryDisallowedToolsDropped) {
		t.Errorf("should not emit disallowed-tools-dropped when no disallowedTools")
	}
	if hasDiagCode(report.Diags, diag.CodeSubagentFactoryIsolationDropped) {
		t.Errorf("should not emit isolation-dropped when no isolation")
	}
}

func TestRunFactorySubagentModelMapping(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("model-agent", "Agent", "Body", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "model-agent", Path: filepath.Join(t.TempDir(), "model-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, d := range report.Diags {
		if d.IsError() {
			t.Fatalf("unexpected error diagnostic: %v", d)
		}
	}

	body, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "droids", "model-agent.md"))
	if err != nil {
		t.Fatalf("read droid: %v", err)
	}
	if !strings.Contains(string(body), "model: claude-sonnet-5") {
		t.Errorf("expected mapped model claude-sonnet-5, got:\n%s", body)
	}
}

func TestRunFactorySubagentWriteToolTranslated(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	subagent := ir.NewSubagent("write-agent", "Agent", "Body", "", []string{"Read", "Write"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "write-agent", Path: filepath.Join(t.TempDir(), "write-agent.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, d := range report.Diags {
		if d.IsError() {
			t.Fatalf("unexpected error diagnostic: %v", d)
		}
	}

	body, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "droids", "write-agent.md"))
	if err != nil {
		t.Fatalf("read droid: %v", err)
	}
	if !strings.Contains(string(body), "Create") {
		t.Errorf("expected Write translated to Create (file.write concept), got:\n%s", body)
	}
	if strings.Contains(string(body), "Write") || strings.Contains(string(body), "Edit") {
		t.Errorf("frontmatter should not contain Write/Edit (Write maps to Create only, no widening), got:\n%s", body)
	}
}

func TestRunMachineScopeUsesHomeDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	irSkill := ir.NewSkill("global-skill", "Global skill", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "global-skill", Resource: irSkill},
			Source:    config.UserPersonal, // machine scope
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tr := report.PerTarget[hcc.TargetClaude]
	// The Machine path is computed at init time from os.UserHomeDir(),
	// not from the test's temp HOME. We verify that the report has an
	// outDir (not empty) and that it ends with .claude.
	if tr.OutDir == "" {
		t.Fatal("expected non-empty OutDir")
	}
	if !strings.HasSuffix(tr.OutDir, ".claude") {
		t.Fatalf("expected machine outDir to end with .claude, got %s", tr.OutDir)
	}
}

func TestRunMaterializesSameTargetToRepoAndMachineScopes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	repoSkill := ir.NewSkill("repo-skill", "Repo skill", "Body", nil, nil, ir.SourceRef{})
	userSkill := ir.NewSkill("user-skill", "User skill", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "repo-skill", Resource: repoSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
		{
			Entry:     catalog.Entry{Name: "user-skill", Resource: userSkill},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if report.TotalWritten() == 0 {
		t.Fatal("expected files to be written")
	}

	repoPath := filepath.Join(repoRoot, ".factory", "skills", "repo-skill", "SKILL.md")
	userPath := filepath.Join(homeDir, ".factory", "skills", "user-skill", "SKILL.md")
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("expected repo-scoped factory skill at %s: %v", repoPath, err)
	}
	if _, err := os.Stat(userPath); err != nil {
		t.Fatalf("expected machine-scoped factory skill at %s: %v", userPath, err)
	}
	userContent, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read machine-scoped factory skill: %v", err)
	}
	if !strings.HasPrefix(string(userContent), "---\nname: user-skill\n") {
		t.Fatalf("expected factory skill frontmatter, got %q", string(userContent))
	}
}

func TestRunIsIdempotentWhenRepoRootIsHomeDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := homeDir // simulate `am sync` run from the home directory

	irSkill := ir.NewSkill("user-skill", "User skill", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "user-skill", Resource: irSkill},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first sync: unexpected error: %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first sync: expected files to be written")
	}

	// Second sync must be idempotent: the repo-scope pass must not prune the
	// machine-scope files that the first sync wrote, because both scope paths
	// resolve to the same directory (~/.factory) when repoRoot == homeDir.
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second sync: unexpected error: %v", err)
	}
	if report2.TotalRemoved() > 0 {
		t.Fatalf("second sync: expected no removals, got %d removed (install→remove cycle detected)", report2.TotalRemoved())
	}
	if report2.TotalWritten() > 0 {
		t.Fatalf("second sync: expected no writes (idempotent), got %d written", report2.TotalWritten())
	}
}

func TestRunFactoryReplacesLegacySkillsRootSymlink(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	cacheSkillDir := filepath.Join(t.TempDir(), "factory-repo")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir cache skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatalf("write cache skill: %v", err)
	}
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatalf("mkdir factory dir: %v", err)
	}
	if err := os.Symlink("../.agents/skills", filepath.Join(factoryDir, "skills")); err != nil {
		t.Fatalf("create legacy skills symlink: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "factory-repo", Path: cacheSkillDir, Resource: ir.NewSkill("factory-repo", "factory repo", "Body", nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	skillsDir := filepath.Join(factoryDir, "skills")
	info, err := os.Lstat(skillsDir)
	if err != nil {
		t.Fatalf("stat skills dir: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected skills root to be replaced with directory, got symlink")
	}
	assertSymlinkTargetUnderCache(t, filepath.Join(skillsDir, "factory-repo"), homeDir)
}

func assertSymlinkTargetUnderCache(t *testing.T, linkPath, homeDir string) string {
	t.Helper()
	target, ok := existingSymlinkTarget(linkPath)
	if !ok {
		t.Fatalf("expected %s to be a symlink", linkPath)
	}
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
	if target != cacheDir && !strings.HasPrefix(target, cacheDir+string(os.PathSeparator)) {
		t.Fatalf("expected symlink target under %q, got %q", cacheDir, target)
	}
	return target
}

func TestReportInSync(t *testing.T) {
	tests := []struct {
		name   string
		report *Report
		inSync bool
	}{
		{
			name:   "empty report is in sync",
			report: &Report{PerTarget: map[hcc.Target]TargetReport{}},
			inSync: true,
		},
		{
			name: "written files means not in sync",
			report: &Report{
				PerTarget: map[hcc.Target]TargetReport{
					hcc.TargetClaude: {Written: []string{"a.md"}},
				},
			},
			inSync: false,
		},
		{
			name: "removed files means not in sync",
			report: &Report{
				PerTarget: map[hcc.Target]TargetReport{
					hcc.TargetClaude: {Removed: []string{"b.md"}},
				},
			},
			inSync: false,
		},
		{
			name: "only skipped is in sync",
			report: &Report{
				PerTarget: map[hcc.Target]TargetReport{
					hcc.TargetClaude: {Skipped: []string{"a.md"}},
				},
			},
			inSync: true,
		},
		{
			name: "error diagnostic means not in sync",
			report: &Report{
				PerTarget: map[hcc.Target]TargetReport{},
				Diags: []diag.Diagnostic{
					{Code: "test", Severity: diag.SeverityError},
				},
			},
			inSync: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.InSync(); got != tt.inSync {
				t.Fatalf("expected InSync=%v, got %v", tt.inSync, got)
			}
		})
	}
}

func TestRenderSkillFrontmatterYAMLSafe(t *testing.T) {
	s := ir.NewSkill("name: with colon", "line1\nline2: with colon", "Body", nil, nil, ir.SourceRef{})
	rendered := renderSkillFrontmatter(s)
	if !strings.HasPrefix(rendered, "---\n") {
		t.Fatalf("expected opening frontmatter delimiter, got %q", rendered)
	}
	withoutOpen := strings.TrimPrefix(rendered, "---\n")
	yamlBody, _, ok := strings.Cut(withoutOpen, "\n---\n")
	if !ok {
		t.Fatalf("expected closing frontmatter delimiter, got %q", rendered)
	}
	var parsed struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(yamlBody), &parsed); err != nil {
		t.Fatalf("expected valid yaml, got: %v\n%s", err, rendered)
	}
	if parsed.Name != s.Name() {
		t.Fatalf("expected name %q, got %q", s.Name(), parsed.Name)
	}
	if parsed.Description != s.Description {
		t.Fatalf("expected description %q, got %q", s.Description, parsed.Description)
	}
}

func TestRunMaterializesSupportFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	// Create a source skill directory with SKILL.md + support files.
	sourceSkillDir := filepath.Join(t.TempDir(), "smith")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("---\nname: smith\ndescription: Smith skill\n---\n\nSkill body content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "helper.sh"), []byte("#!/bin/bash\necho helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(sourceSkillDir, "scripts")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "run.py"), []byte("print('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	irSkill := ir.NewSkill("smith", "Smith skill", "Skill body content", nil, nil, ir.SourceRef{File: filepath.Join(sourceSkillDir, "SKILL.md"), Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "smith", Path: sourceSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	// The skill symlink should exist.
	linkPath := filepath.Join(repoRoot, ".claude", "skills", "smith")
	target, ok := existingSymlinkTarget(linkPath)
	if !ok {
		t.Fatalf("expected %s to be a symlink", linkPath)
	}

	// The symlink target (cache dir) should contain SKILL.md, helper.sh,
	// and scripts/run.py.
	skillMd, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md from cache: %v", err)
	}
	if !strings.Contains(string(skillMd), "Skill body content") {
		t.Fatalf("expected SKILL.md to contain body, got %q", skillMd)
	}

	helperContent, err := os.ReadFile(filepath.Join(target, "helper.sh"))
	if err != nil {
		t.Fatalf("read helper.sh from cache: %v", err)
	}
	if !strings.Contains(string(helperContent), "echo helper") {
		t.Fatalf("expected helper.sh to contain script, got %q", helperContent)
	}

	scriptContent, err := os.ReadFile(filepath.Join(target, "scripts", "run.py"))
	if err != nil {
		t.Fatalf("read scripts/run.py from cache: %v", err)
	}
	if !strings.Contains(string(scriptContent), "print('hello')") {
		t.Fatalf("expected scripts/run.py to contain script, got %q", scriptContent)
	}
}

func TestRunMaterializesSupportFilesForFactory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	sourceSkillDir := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Code review\n---\n\nReview body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "config.json"), []byte(`{"level": "strict"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	irSkill := ir.NewSkill("review", "Code review", "Review body", nil, nil, ir.SourceRef{File: filepath.Join(sourceSkillDir, "SKILL.md"), Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "review", Path: sourceSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	linkPath := filepath.Join(repoRoot, ".factory", "skills", "review")
	target, ok := existingSymlinkTarget(linkPath)
	if !ok {
		t.Fatalf("expected %s to be a symlink", linkPath)
	}

	configContent, err := os.ReadFile(filepath.Join(target, "config.json"))
	if err != nil {
		t.Fatalf("read config.json from cache: %v", err)
	}
	if !strings.Contains(string(configContent), "strict") {
		t.Fatalf("expected config.json to contain content, got %q", configContent)
	}
}

func TestRunSkillBodyPreservedInMaterializedOutput(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	sourceSkillDir := filepath.Join(t.TempDir(), "code-review")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: code-review\ndescription: Code review skill\n---\n\nYou are a code reviewer.\nFocus on correctness and security."
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	irSkill := ir.NewSkill("code-review", "Code review skill", "You are a code reviewer.\nFocus on correctness and security.", nil, nil, ir.SourceRef{File: filepath.Join(sourceSkillDir, "SKILL.md"), Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "code-review", Path: sourceSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	// Read the materialized SKILL.md through the symlink.
	linkPath := filepath.Join(repoRoot, ".claude", "skills", "code-review")
	target, ok := existingSymlinkTarget(linkPath)
	if !ok {
		t.Fatalf("expected %s to be a symlink", linkPath)
	}

	written, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized SKILL.md: %v", err)
	}
	writtenStr := string(written)

	// The body must be present in the output, not just frontmatter.
	if !strings.Contains(writtenStr, "You are a code reviewer.") {
		t.Fatalf("expected SKILL.md to contain body text, got:\n%s", writtenStr)
	}
	if !strings.Contains(writtenStr, "Focus on correctness and security.") {
		t.Fatalf("expected SKILL.md to contain full body, got:\n%s", writtenStr)
	}
}

func TestRunMcpServerMergesIntoExistingFactoryConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryDir, "mcp.json"), []byte(`{
  "mcpServers": {
    "personal": {"command": "personal"},
    "stale": {"command": "stale", "_managed_by": "agent-marketplace"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mcp := ir.NewMcpServer("registry-server", "registry", []string{"serve"}, map[string]string{"TOKEN": "abc"}, nil, ir.SourceRef{File: "mcp/registry-server.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "registry-server", Path: "mcp/registry-server.json", Resource: mcp},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 1 || report.Diags[0].Code != "mcp.credentials.literal-secret" {
		t.Fatalf("expected literal credential diagnostic, got %v", report.Diags)
	}

	content, err := os.ReadFile(filepath.Join(factoryDir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"personal"`) {
		t.Fatalf("expected user MCP entry to be preserved, got %s", got)
	}
	if strings.Contains(got, `"stale"`) {
		t.Fatalf("expected stale managed MCP entry to be removed, got %s", got)
	}
	if !strings.Contains(got, `"registry-server"`) || !strings.Contains(got, `"_managed_by": "agent-marketplace"`) {
		t.Fatalf("expected managed MCP entry to be merged, got %s", got)
	}
	if target, ok := existingSymlinkTarget(filepath.Join(factoryDir, "mcp.json")); ok {
		t.Fatalf("expected mcp.json to be a real file, got symlink to %s", target)
	}
}

func TestRunSubagentMergesIntoExistingFactoryPluginManifest(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	pluginDir := filepath.Join(repoRoot, ".factory", ".factory-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing manifest: a user-authored droid entry plus a stale entry we
	// previously managed. The user entry must survive; the stale one must go.
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
  "droids": [
    {"name": "user-droid", "description": "hand-written"},
    {"name": "stale", "description": "old", "_managed_by": "agent-marketplace"}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	subagent := ir.NewSubagent("worker-generic", "Worker", "You are a worker", "claude-sonnet-4-5-20250929", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSubagent, Name: "worker-generic", Path: filepath.Join(t.TempDir(), "worker-generic.md"), Resource: subagent},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	// Factory discovers droids from the filesystem, not plugin.json arrays.
	// The existing plugin.json is preserved (user entries kept, stale managed
	// entries pruned), but no new managed entries are added.
	pluginPath := filepath.Join(pluginDir, "plugin.json")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"user-droid"`) {
		t.Fatalf("expected user droid entry to be preserved, got %s", got)
	}
	if strings.Contains(got, `"stale"`) {
		t.Fatalf("expected stale managed entry to be pruned, got %s", got)
	}
	// Factory discovers droids from the filesystem, not plugin.json arrays.
	// The compiled subagent must NOT be inserted as a new managed entry.
	if strings.Contains(got, `"worker-generic"`) {
		t.Fatalf("expected worker-generic to NOT be added to plugin.json, got %s", got)
	}
	if strings.Contains(got, `"_managed_by"`) {
		t.Fatalf("expected no _managed_by records in plugin.json, got %s", got)
	}
}

func TestStripManagedEntriesFromFile_ArrayContainer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, []byte(`{
  "skills": [
    {"name": "user-skill", "description": "keep me"},
    {"name": "managed-skill", "description": "drop me", "_managed_by": "agent-marketplace"}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cleaned, removed, err := stripManagedEntriesFromFile(path, false)
	if err != nil {
		t.Fatalf("stripManagedEntriesFromFile: %v", err)
	}
	if !slices.Contains(removed, "managed-skill") {
		t.Fatalf("expected managed-skill in removed, got %v", removed)
	}
	got := string(cleaned)
	if !strings.Contains(got, `"user-skill"`) {
		t.Fatalf("expected user-skill to be preserved, got %s", got)
	}
	if strings.Contains(got, `"managed-skill"`) {
		t.Fatalf("expected managed-skill to be stripped, got %s", got)
	}
	// The container key must remain (as an empty array) so the file stays valid.
	var parsed map[string]any
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("unmarshal cleaned: %v", err)
	}
	arr, ok := parsed["skills"].([]any)
	if !ok {
		t.Fatalf("expected skills to remain an array, got %T", parsed["skills"])
	}
	if len(arr) != 1 {
		t.Fatalf("expected exactly one (user) skill to remain, got %v", arr)
	}
}

func TestRunMcpServerReplacesExistingSymlinkWithMergedFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate a pre-existing symlink to a cached mcp.json (which can happen
	// if a previous sync incorrectly symlinked the merge artifact).
	cachedDir := filepath.Join(repoRoot, ".cache", "mcp")
	if err := os.MkdirAll(cachedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachedContent := `{"mcpServers":{"cached-only":{"command":"cached"}}}`
	if err := os.WriteFile(filepath.Join(cachedDir, "mcp.json"), []byte(cachedContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(cachedDir, "mcp.json"), filepath.Join(factoryDir, "mcp.json")); err != nil {
		t.Fatal(err)
	}

	mcp := ir.NewMcpServer("registry-server", "registry", []string{"serve"}, map[string]string{"TOKEN": "abc"}, nil, ir.SourceRef{File: "mcp/registry-server.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "registry-server", Path: "mcp/registry-server.json", Resource: mcp},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 1 || report.Diags[0].Code != "mcp.credentials.literal-secret" {
		t.Fatalf("expected literal credential diagnostic, got %v", report.Diags)
	}

	// The result must be a regular file, not a symlink.
	if target, ok := existingSymlinkTarget(filepath.Join(factoryDir, "mcp.json")); ok {
		t.Fatalf("expected mcp.json to be a real file, got symlink to %s", target)
	}

	content, err := os.ReadFile(filepath.Join(factoryDir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"registry-server"`) {
		t.Fatalf("expected managed MCP entry to be present, got %s", got)
	}
	if !strings.Contains(got, `"_managed_by": "agent-marketplace"`) {
		t.Fatalf("expected _managed_by tag on managed entry, got %s", got)
	}
	// User entry from the cached file should be preserved by the merge.
	if !strings.Contains(got, `"cached-only"`) {
		t.Fatalf("expected cached-only entry (from symlink target) to be preserved, got %s", got)
	}
}

func TestRunMcpServerWindsurfRepoScopeWritesToWindsurfDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	mcp := ir.NewMcpServer("repo-mcp", "repo-cmd", []string{"serve"}, map[string]string{"K": "v"}, nil, ir.SourceRef{File: "mcp.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "repo-mcp", Resource: mcp},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	// MCP config must be at .windsurf/mcp_config.json, NOT at repo root.
	windsurfMcpPath := filepath.Join(repoRoot, ".windsurf", "mcp_config.json")
	content, err := os.ReadFile(windsurfMcpPath)
	if err != nil {
		t.Fatalf("expected .windsurf/mcp_config.json to exist: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"repo-mcp"`) {
		t.Fatalf("expected repo-mcp in .windsurf/mcp_config.json, got %s", got)
	}
	if !strings.Contains(got, `"_managed_by": "agent-marketplace"`) {
		t.Fatalf("expected _managed_by marker, got %s", got)
	}

	// Must NOT exist at the repo root.
	stalePath := filepath.Join(repoRoot, "mcp_config.json")
	if _, err := os.Stat(stalePath); err == nil {
		t.Fatalf("mcp_config.json should NOT exist at repo root, only under .windsurf/")
	}
}

func TestRunMcpServerWindsurfMachineScopeWritesToMachineRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	mcp := ir.NewMcpServer("user-mcp", "user-cmd", []string{"run"}, map[string]string{"K": "v"}, nil, ir.SourceRef{File: "mcp.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "user-mcp", Resource: mcp},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(report.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", report.Diags)
	}

	// Machine-scope MCP config must be at ~/.codeium/windsurf/mcp_config.json.
	machineMcpPath := filepath.Join(homeDir, ".codeium", "windsurf", "mcp_config.json")
	content, err := os.ReadFile(machineMcpPath)
	if err != nil {
		t.Fatalf("expected ~/.codeium/windsurf/mcp_config.json to exist: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"user-mcp"`) {
		t.Fatalf("expected user-mcp in machine mcp_config.json, got %s", got)
	}
	if !strings.Contains(got, `"_managed_by": "agent-marketplace"`) {
		t.Fatalf("expected _managed_by marker, got %s", got)
	}
}

func TestRunDoesNotPruneUserOwnedFilesInComponentDirs(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Materialize a skill to Factory target to set up the harness root.
	irSkill := ir.NewSkill("keep-skill", "Keep", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "keep-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	factoryDir := filepath.Join(repoRoot, ".factory")

	// Create a user-owned regular file in the droids component directory.
	droidsDir := filepath.Join(factoryDir, "droids")
	userFile := filepath.Join(droidsDir, "my-custom-droid.md")
	if err := os.MkdirAll(droidsDir, 0o755); err != nil {
		t.Fatalf("mkdir droids: %v", err)
	}
	if err := os.WriteFile(userFile, []byte("# My custom droid\n"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	// Also create a stale managed symlink in the same directory to verify
	// managed symlinks are still pruned. It must point to the cache so
	// it's recognized as agent-marketplace-managed.
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
	staleLink := filepath.Join(droidsDir, "stale-managed-droid.md")
	if err := os.Symlink(filepath.Join(cacheDir, "compiled", "stale"), staleLink); err != nil {
		t.Fatalf("create stale managed symlink: %v", err)
	}
	// Also create a user-owned symlink (not cache-managed) to verify it is preserved.
	userSymlink := filepath.Join(droidsDir, "user-symlink-droid.md")
	if err := os.Symlink("/nonexistent", userSymlink); err != nil {
		t.Fatalf("create user symlink: %v", err)
	}

	// Run materialization again — user file should survive, stale symlink removed.
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// User-owned regular file must still exist.
	if _, err := os.Stat(userFile); os.IsNotExist(err) {
		t.Fatal("user-owned regular file should NOT have been removed")
	}

	// Stale managed symlink must have been removed.
	if _, err := os.Lstat(staleLink); !os.IsNotExist(err) {
		t.Fatal("stale managed symlink should have been removed")
	}

	// User-owned symlink must be preserved.
	if _, err := os.Lstat(userSymlink); os.IsNotExist(err) {
		t.Fatal("user-owned symlink should NOT have been removed")
	}

	// Verify the stale symlink appears in Removed, but the user file does not.
	removed := report2.PerTarget[hcc.TargetFactory].Removed
	if !slices.Contains(removed, filepath.Join("droids", "stale-managed-droid.md")) {
		t.Fatalf("expected droids/stale-managed-droid.md in Removed, got %v", removed)
	}
	if slices.Contains(removed, filepath.Join("droids", "my-custom-droid.md")) {
		t.Fatal("user-owned file should NOT appear in Removed")
	}
	if slices.Contains(removed, filepath.Join("droids", "user-symlink-droid.md")) {
		t.Fatal("user-owned symlink should NOT appear in Removed")
	}
}

func TestRunDoesNotPruneUserOwnedSkillDirectories(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Materialize a managed skill to Factory target to set up the harness root.
	irSkill := ir.NewSkill("managed-skill", "Managed", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "managed-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	factoryDir := filepath.Join(repoRoot, ".factory")
	skillsDir := filepath.Join(factoryDir, "skills")

	// Create a user-owned (non-symlink) skill directory in .factory/skills/.
	userSkillDir := filepath.Join(skillsDir, "my-manual-skill")
	if err := os.MkdirAll(userSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir user skill: %v", err)
	}
	userSkillMD := filepath.Join(userSkillDir, "SKILL.md")
	if err := os.WriteFile(userSkillMD, []byte("# My Manual Skill\n"), 0o644); err != nil {
		t.Fatalf("write user skill SKILL.md: %v", err)
	}

	// Also create a stale managed symlink skill to verify managed symlinks
	// are still pruned. It must point to the cache so it's recognized as
	// agent-marketplace-managed.
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
	staleLink := filepath.Join(skillsDir, "stale-skill")
	if err := os.Symlink(filepath.Join(cacheDir, "compiled", "stale"), staleLink); err != nil {
		t.Fatalf("create stale managed symlink: %v", err)
	}
	// Also create a user-owned symlink (not cache-managed) to verify it is preserved.
	userSymlink := filepath.Join(skillsDir, "user-skill-link")
	if err := os.Symlink("/nonexistent", userSymlink); err != nil {
		t.Fatalf("create user symlink: %v", err)
	}

	// Run materialization again — user skill dir should survive, stale symlink removed.
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// User-owned skill directory must still exist.
	if _, err := os.Stat(userSkillMD); os.IsNotExist(err) {
		t.Fatal("user-owned skill directory should NOT have been removed")
	}

	// Stale managed symlink must have been removed.
	if _, err := os.Lstat(staleLink); !os.IsNotExist(err) {
		t.Fatal("stale managed skill symlink should have been removed")
	}

	// User-owned symlink must be preserved.
	if _, err := os.Lstat(userSymlink); os.IsNotExist(err) {
		t.Fatal("user-owned skill symlink should NOT have been removed")
	}

	// Verify the stale symlink appears in Removed, but the user skill dir does not.
	removed := report2.PerTarget[hcc.TargetFactory].Removed
	if !slices.Contains(removed, "skills/stale-skill") {
		t.Fatalf("expected skills/stale-skill in Removed, got %v", removed)
	}
	if slices.Contains(removed, "skills/my-manual-skill") {
		t.Fatal("user-owned skill directory should NOT appear in Removed")
	}

	// Verify a diagnostic was emitted for the preserved user-owned skill.
	var preservedDiags []diag.Diagnostic
	for _, d := range report2.Diags {
		if d.Code == "materialize.skill.user-owned-preserved" {
			preservedDiags = append(preservedDiags, d)
		}
	}
	if len(preservedDiags) == 0 {
		t.Fatal("expected at least one materialize.skill.user-owned-preserved diagnostic")
	}
	found := false
	for _, d := range preservedDiags {
		if strings.Contains(d.Message, "my-manual-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected diagnostic to mention my-manual-skill, got %v", preservedDiags)
	}
}

// TestRunWindsurfRepoScopeSkillSyncIsIdempotent is a regression test for
// The second sync of a Windsurf repo-scoped skill must produce 0
// written and 0 removed files.
//
// Root cause: linkedComponents() extracted ".windsurf" as the component name
// from the skillLink installDirRel ".windsurf/skills", but the prune map
// expected "skills". The mismatch left noPrune["skills"]=false, which caused
// syncArtifactLinks to delete the skill symlink on every second sync.
func TestRunWindsurfRepoScopeSkillSyncIsIdempotent(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cacheSkillDir := filepath.Join(t.TempDir(), "windsurf-skill")
	if err := os.MkdirAll(cacheSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkillDir, "SKILL.md"), []byte("Body"), 0o644); err != nil {
		t.Fatal(err)
	}

	irSkill := ir.NewSkill("windsurf-skill", "Windsurf skill", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "windsurf-skill", Path: cacheSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected at least one file written")
	}

	skillLink := filepath.Join(repoRoot, ".windsurf", "skills", "windsurf-skill")
	if _, err := os.Lstat(skillLink); err != nil {
		t.Fatalf("first run: expected skill link to exist: %v", err)
	}

	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	if report2.TotalWritten() != 0 {
		t.Fatalf("second run: expected 0 written (idempotent), got %d: %v",
			report2.TotalWritten(), report2.PerTarget[hcc.TargetWindsurf].Written)
	}
	if report2.TotalRemoved() != 0 {
		t.Fatalf("second run: expected 0 removed (idempotent), got %d: %v",
			report2.TotalRemoved(), report2.PerTarget[hcc.TargetWindsurf].Removed)
	}

	if _, err := os.Lstat(skillLink); err != nil {
		t.Fatalf("second run: expected skill link to still exist: %v", err)
	}
}

// TestRunHookArtifactsAreNotPrunedOnSecondSync is a regression test for
// Hook merge artifacts must not be deleted by syncArtifactLinks on
// every sync. The hooks directory contains only merge (fragment) artifacts;
// the previous behaviour called syncArtifactLinks(hooks_dir, [], prune=true)
// which deleted all files before materializeMergeArtifacts re-wrote them.
// spec: hook-resources/repeat-sync-is-byte-identical
func TestRunHookArtifactsAreNotPrunedOnSecondSync(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	hook := ir.NewHook("pre-tool-use", "PreToolUse", "${DROID_PLUGIN_ROOT}/check.sh", "command", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "pre-tool-use", Resource: hook},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected hook artifacts to be written")
	}

	hooksFile := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	if _, err := os.Stat(hooksFile); err != nil {
		t.Fatalf("first run: expected hooks.json to exist at %s: %v", hooksFile, err)
	}
	content1, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("first run: read hooks.json: %v", err)
	}

	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	if report2.TotalRemoved() != 0 {
		t.Fatalf("second run: expected 0 removed (hooks must not be pruned), got %d: %v",
			report2.TotalRemoved(), report2.PerTarget[hcc.TargetFactory].Removed)
	}

	content2, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("second run: read hooks.json: %v", err)
	}
	if string(content1) != string(content2) {
		t.Fatalf("second run: hooks.json content changed between syncs:\nbefore: %s\nafter:  %s", content1, content2)
	}
}

// spec: materialization/machine-scope-hook-command-uses-the-absolute-machine-root
// spec: materialization/repository-scope-hook-command-stays-repository-relative
func TestFactoryHookCommandRootIsStableAcrossWorkingDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	machineHook := ir.NewHook("user-check", "PreToolUse", "${DROID_PLUGIN_ROOT}/hooks/check.sh", "command", nil, ir.SourceRef{})
	user := []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Name: "user-check", Resource: machineHook}, Source: config.UserPersonal,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	machineFile := filepath.Join(home, ".factory", "hooks", "hooks.json")
	var first []byte
	for _, root := range []string{home, repo} {
		if _, err := Run(context.Background(), root, user); err != nil {
			t.Fatalf("materialize machine hook from %s: %v", root, err)
		}
		content, err := os.ReadFile(machineFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), filepath.Join(home, ".factory", "hooks", "check.sh")) {
			t.Fatalf("machine hook must use absolute root, got %s", content)
		}
		if first != nil && !bytes.Equal(first, content) {
			t.Fatalf("machine hook bytes changed across roots:\n%s\n%s", first, content)
		}
		first = content
	}

	repoHook := ir.NewHook("repo-check", "PreToolUse", "${DROID_PLUGIN_ROOT}/hooks/repo-check.sh", "command", nil, ir.SourceRef{})
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Name: "repo-check", Resource: repoHook}, Source: config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}); err != nil {
		t.Fatalf("materialize repository hook: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repo, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), ".factory/hooks/repo-check.sh") || strings.Contains(string(content), repo+"/.factory") {
		t.Fatalf("repository hook must use relative root, got %s", content)
	}
}

// TestRunMultipleHooksSameEventBothSurvive verifies that two hooks for the
// same event (e.g. PostToolUse) are both present in the materialized
// hooks.json and survive idempotent re-syncs without duplication.
func TestRunMultipleHooksSameEventBothSurvive(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	hook1 := ir.NewHookWithMatcher("post-tool-use-1", "PostToolUse", "Edit|Create|MultiEdit",
		"${DROID_PLUGIN_ROOT}/remind.sh", "command", nil, ir.SourceRef{})
	hook2 := ir.NewHookWithMatcher("post-tool-use-2", "PostToolUse", "Read",
		"${DROID_PLUGIN_ROOT}/check.sh", "command", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "post-tool-use-1", Resource: hook1}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Name: "post-tool-use-2", Resource: hook2}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}

	// First sync: both hooks should be written.
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first run: %v", err)
	}

	hooksFile := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	content1, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(content1, &data); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	rules, ok := data["PostToolUse"].([]any)
	if !ok {
		t.Fatalf("PostToolUse not found or not an array: %v", data)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rule groups for PostToolUse, got %d: %v", len(rules), rules)
	}

	// Both commands should be present.
	content1Str := string(content1)
	if !strings.Contains(content1Str, "remind.sh") {
		t.Errorf("hooks.json missing remind.sh: %s", content1Str)
	}
	if !strings.Contains(content1Str, "check.sh") {
		t.Errorf("hooks.json missing check.sh: %s", content1Str)
	}

	// Second sync: should be idempotent (no duplication).
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("second run: %v", err)
	}
	content2, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("second read hooks.json: %v", err)
	}

	data2 := map[string]any{}
	if err := json.Unmarshal(content2, &data2); err != nil {
		t.Fatalf("parse second hooks.json: %v", err)
	}
	rules2, ok := data2["PostToolUse"].([]any)
	if !ok {
		t.Fatalf("second: PostToolUse not found: %v", data2)
	}
	if len(rules2) != 2 {
		t.Fatalf("second sync: expected 2 rule groups, got %d (duplication?): %s", len(rules2), content2)
	}

	if string(content1) != string(content2) {
		t.Fatalf("hooks.json changed between syncs:\nbefore: %s\nafter:  %s", content1, content2)
	}
}

// TestRunMultipleHooksSameEventSameMatcherBothSurvive verifies that two hooks
// for the same event AND same matcher (coming from different sources) both
// survive in the materialized hooks.json and are not deduplicated or lost
// on re-sync.
func TestRunMultipleHooksSameEventSameMatcherBothSurvive(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	hook1 := ir.NewHookWithMatcher("post-tool-use-remind", "PostToolUse", "Edit|Create|MultiEdit",
		"${DROID_PLUGIN_ROOT}/remind.sh", "command", nil, ir.SourceRef{})
	hook2 := ir.NewHookWithMatcher("post-tool-use-check", "PostToolUse", "Edit|Create|MultiEdit",
		"${DROID_PLUGIN_ROOT}/check.sh", "command", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "post-tool-use-remind", Resource: hook1}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Name: "post-tool-use-check", Resource: hook2}, Source: config.RepoPersonal, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}

	// First sync: both hooks should be written.
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first run: %v", err)
	}

	hooksFile := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	content1, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(content1, &data); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	rules, ok := data["PostToolUse"].([]any)
	if !ok {
		t.Fatalf("PostToolUse not found or not an array: %v", data)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rule groups for PostToolUse with same matcher, got %d: %s", len(rules), content1)
	}

	// Both commands should be present.
	content1Str := string(content1)
	if !strings.Contains(content1Str, "remind.sh") {
		t.Errorf("hooks.json missing remind.sh: %s", content1Str)
	}
	if !strings.Contains(content1Str, "check.sh") {
		t.Errorf("hooks.json missing check.sh: %s", content1Str)
	}

	// Second sync: should be idempotent (no duplication, no loss).
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("second run: %v", err)
	}
	content2, err := os.ReadFile(hooksFile)
	if err != nil {
		t.Fatalf("second read hooks.json: %v", err)
	}

	data2 := map[string]any{}
	if err := json.Unmarshal(content2, &data2); err != nil {
		t.Fatalf("parse second hooks.json: %v", err)
	}
	rules2, ok := data2["PostToolUse"].([]any)
	if !ok {
		t.Fatalf("second: PostToolUse not found: %v", data2)
	}
	if len(rules2) != 2 {
		t.Fatalf("second sync: expected 2 rule groups, got %d (duplication or loss?): %s", len(rules2), content2)
	}

	if string(content1) != string(content2) {
		t.Fatalf("hooks.json changed between syncs:\nbefore: %s\nafter:  %s", content1, content2)
	}
}

// TestRunExcludedHookEntryRemovedFromMergedFile verifies that when a hook is
// excluded (no longer in the resolved list), its managed entry is removed
// from the merged hooks.json — not just the support file symlink.
// spec: hook-resources/deselected-hook-is-removed-from-the-merged-file
func TestRunExcludedHookEntryRemovedFromMergedFile(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create source dirs with scripts.
	sourceDir1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir1, "remind.sh"), []byte("#!/bin/bash\necho remind\n"), 0o755); err != nil {
		t.Fatalf("write remind.sh: %v", err)
	}
	sourceDir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir2, "check.sh"), []byte("#!/bin/bash\necho check\n"), 0o755); err != nil {
		t.Fatalf("write check.sh: %v", err)
	}

	hook1 := ir.NewHookWithMatcher("post-tool-use-remind", "PostToolUse", "Edit|Create|MultiEdit",
		".factory/hooks/remind.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir1, "hooks.json")})
	hook2 := ir.NewHookWithMatcher("pre-tool-use-check", "PreToolUse", "Read",
		".factory/hooks/check.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir2, "hooks.json")})

	// First sync: both hooks active.
	resolvedBoth := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "post-tool-use-remind", Resource: hook1, Path: filepath.Join(sourceDir1, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
		{Entry: catalog.Entry{Name: "pre-tool-use-check", Resource: hook2, Path: filepath.Join(sourceDir2, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}
	if _, err := Run(context.Background(), repoRoot, resolvedBoth); err != nil {
		t.Fatalf("first run: %v", err)
	}

	hooksFile := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	content1, _ := os.ReadFile(hooksFile)

	// Verify both events are present.
	var data map[string]any
	if err := json.Unmarshal(content1, &data); err != nil {
		t.Fatalf("parse hooks.json after first sync: %v", err)
	}
	if _, ok := data["PostToolUse"]; !ok {
		t.Fatalf("PostToolUse missing after first sync: %s", content1)
	}
	if _, ok := data["PreToolUse"]; !ok {
		t.Fatalf("PreToolUse missing after first sync: %s", content1)
	}

	// Second sync: exclude hook1 (PostToolUse). Only hook2 remains.
	resolvedOnly2 := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "pre-tool-use-check", Resource: hook2, Path: filepath.Join(sourceDir2, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}
	if _, err := Run(context.Background(), repoRoot, resolvedOnly2); err != nil {
		t.Fatalf("second run: %v", err)
	}

	content2, _ := os.ReadFile(hooksFile)

	// PostToolUse should be gone (the only hook for that event was excluded).
	var data2 map[string]any
	if err := json.Unmarshal(content2, &data2); err != nil {
		t.Fatalf("parse hooks.json after second sync: %v", err)
	}
	if _, ok := data2["PostToolUse"]; ok {
		t.Errorf("PostToolUse should have been removed from hooks.json after excluding the only hook for that event: %s", content2)
	}
	// PreToolUse should still be present.
	if _, ok := data2["PreToolUse"]; !ok {
		t.Errorf("PreToolUse should still be present: %s", content2)
	}

	// The remind.sh symlink should have been pruned.
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "hooks", "remind.sh")); !os.IsNotExist(err) {
		t.Errorf("remind.sh symlink should have been pruned")
	}
	// The check.sh symlink should still exist.
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "hooks", "check.sh")); err != nil {
		t.Errorf("check.sh symlink should still exist: %v", err)
	}
}

// TestRunHookSupportFilesAreSymlinked verifies that support files (scripts)
// alongside hooks.json in the source directory are materialized as symlinks
// in the hooks output directory, and are pruned when the hook is removed.
// spec: hook-resources/support-scripts-are-installed-beside-the-hook-file
func TestRunHookSupportFilesAreSymlinked(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create a source directory with hooks.json and a support script.
	sourceDir := t.TempDir()
	scriptContent := []byte("#!/bin/bash\necho 'remind to fix isolated builds'\n")
	if err := os.WriteFile(filepath.Join(sourceDir, "remind.sh"), scriptContent, 0o755); err != nil {
		t.Fatalf("write source script: %v", err)
	}

	hook := ir.NewHookWithMatcher("post-tool-use-remind", "PostToolUse", "Edit|Create|MultiEdit",
		"${DROID_PLUGIN_ROOT}/hooks/remind.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "post-tool-use-remind", Resource: hook, Path: filepath.Join(sourceDir, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}

	// First sync: hooks.json and remind.sh should both be materialized.
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first run: %v", err)
	}

	hooksDir := filepath.Join(repoRoot, ".factory", "hooks")

	// hooks.json should be a real file (merge artifact).
	hooksJSON := filepath.Join(hooksDir, "hooks.json")
	info, err := os.Lstat(hooksJSON)
	if err != nil {
		t.Fatalf("hooks.json not materialized: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("hooks.json should be a real file, not a symlink")
	}

	// remind.sh should be a symlink (regular artifact).
	scriptLink := filepath.Join(hooksDir, "remind.sh")
	scriptInfo, err := os.Lstat(scriptLink)
	if err != nil {
		t.Fatalf("remind.sh not materialized: %v", err)
	}
	if scriptInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("remind.sh should be a symlink, got mode: %v", scriptInfo.Mode())
	}

	// The symlink should resolve to the correct content.
	content, err := os.ReadFile(scriptLink)
	if err != nil {
		t.Fatalf("read remind.sh through symlink: %v", err)
	}
	if !bytes.Contains(content, []byte("remind to fix isolated builds")) {
		t.Errorf("remind.sh content mismatch: %s", content)
	}

	// Second sync with no hooks: remind.sh symlink should be pruned,
	// but hooks.json should be cleaned of managed entries (becoming empty/minimal).
	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("second run (empty): %v", err)
	}

	if _, err := os.Lstat(scriptLink); !os.IsNotExist(err) {
		t.Errorf("remind.sh symlink should have been pruned, got err: %v", err)
	}
}

// spec: hook-resources/factory-commands-point-into-the-install-root
func TestRunFactoryHookCommandPointsIntoInstallRoot(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	sourceDir := t.TempDir()
	hook := ir.NewHook("pre-tool-use-check", "PreToolUse",
		"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "pre-tool-use-check", Resource: hook, Path: filepath.Join(sourceDir, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if hasErrorDiagnostics(report.Diags) {
		t.Fatalf("unexpected error diagnostics: %v", report.Diags)
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read materialized hooks.json: %v", err)
	}
	var document map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("materialized hooks.json is not valid JSON: %v\n%s", err, raw)
	}
	groups := document["PreToolUse"]
	if len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("materialized hooks.json does not contain exactly one PreToolUse handler: %s", raw)
	}
	if got := groups[0].Hooks[0].Command; got != ".factory/hooks/check.sh" {
		t.Fatalf("materialized command = %q, want the repository-relative install-root path %q", got, ".factory/hooks/check.sh")
	}
}

func TestRunDoesNotPruneUserOwnedSkillDirectoriesWindsurf(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Materialize a managed skill to Windsurf target.
	irSkill := ir.NewSkill("managed-skill", "Managed", "Body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "managed-skill", Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	windsurfDir := filepath.Join(repoRoot, ".windsurf")
	skillsDir := filepath.Join(windsurfDir, "skills")

	// Create a user-owned (non-symlink) skill directory in .windsurf/skills/.
	userSkillDir := filepath.Join(skillsDir, "my-windsurf-manual-skill")
	if err := os.MkdirAll(userSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir user skill: %v", err)
	}
	userSkillMD := filepath.Join(userSkillDir, "SKILL.md")
	if err := os.WriteFile(userSkillMD, []byte("# My Windsurf Manual Skill\n"), 0o644); err != nil {
		t.Fatalf("write user skill SKILL.md: %v", err)
	}

	// Run materialization again — user skill dir should survive.
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// User-owned skill directory must still exist.
	if _, err := os.Stat(userSkillMD); os.IsNotExist(err) {
		t.Fatal("user-owned skill directory in .windsurf/skills/ should NOT have been removed")
	}

	// Verify the user skill dir does not appear in Removed.
	removed := report2.PerTarget[hcc.TargetWindsurf].Removed
	if slices.Contains(removed, "skills/my-windsurf-manual-skill") {
		t.Fatal("user-owned skill directory should NOT appear in Removed")
	}

	// Verify a diagnostic was emitted for the preserved user-owned skill.
	var preservedDiags []diag.Diagnostic
	for _, d := range report2.Diags {
		if d.Code == "materialize.skill.user-owned-preserved" {
			preservedDiags = append(preservedDiags, d)
		}
	}
	if len(preservedDiags) == 0 {
		t.Fatal("expected at least one materialize.skill.user-owned-preserved diagnostic for Windsurf")
	}
}

func TestCopySupportFilesSkipsUnchangedReadOnlyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "helper.sh")
	dstPath := filepath.Join(dstDir, "helper.sh")
	content := []byte("#!/bin/sh\necho unchanged\n")
	if err := os.WriteFile(srcPath, content, 0o444); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dstPath, content, 0o444); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := copySupportFiles(srcDir, dstDir); err != nil {
		t.Fatalf("copySupportFiles should skip unchanged read-only file: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("destination content = %q, want %q", got, content)
	}
}

func TestCopySupportFilesCopiesChangedNestedFileAndMode(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcNestedDir := filepath.Join(srcDir, "scripts")
	dstNestedDir := filepath.Join(dstDir, "scripts")
	if err := os.MkdirAll(srcNestedDir, 0o755); err != nil {
		t.Fatalf("mkdir source scripts: %v", err)
	}
	if err := os.MkdirAll(dstNestedDir, 0o755); err != nil {
		t.Fatalf("mkdir destination scripts: %v", err)
	}

	srcPath := filepath.Join(srcNestedDir, "run.sh")
	dstPath := filepath.Join(dstNestedDir, "run.sh")
	if err := os.WriteFile(srcPath, []byte("#!/bin/sh\necho updated\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("#!/bin/sh\necho stale\n"), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := copySupportFiles(srcDir, dstDir); err != nil {
		t.Fatalf("copySupportFiles: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != "#!/bin/sh\necho updated\n" {
		t.Fatalf("destination content = %q, want updated content", got)
	}
	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("destination mode = %v, want %v", info.Mode().Perm(), os.FileMode(0o755))
	}
}

func TestIsManagedSymlink(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")

	tests := []struct {
		name  string
		setup func(dir string) string // returns the path to check
		want  bool
	}{
		{
			name: "nonexistent_path",
			setup: func(dir string) string {
				return filepath.Join(dir, "does-not-exist")
			},
			want: false,
		},
		{
			name: "regular_file",
			setup: func(dir string) string {
				p := filepath.Join(dir, "regular.txt")
				_ = os.WriteFile(p, []byte("data"), 0o644)
				return p
			},
			want: false,
		},
		{
			name: "directory",
			setup: func(dir string) string {
				p := filepath.Join(dir, "subdir")
				_ = os.MkdirAll(p, 0o755)
				return p
			},
			want: false,
		},
		{
			name: "symlink_to_cache_dir",
			setup: func(dir string) string {
				p := filepath.Join(dir, "managed-link")
				_ = os.Symlink(filepath.Join(cacheDir, "compiled", "skill"), p)
				return p
			},
			want: true,
		},
		{
			name: "symlink_to_cache_dir_exact",
			setup: func(dir string) string {
				p := filepath.Join(dir, "cache-root-link")
				_ = os.Symlink(cacheDir, p)
				return p
			},
			want: false, // exact cache dir, not under it (no trailing separator)
		},
		{
			name: "symlink_outside_cache",
			setup: func(dir string) string {
				p := filepath.Join(dir, "user-link")
				_ = os.Symlink("/some/other/path", p)
				return p
			},
			want: false,
		},
		{
			name: "symlink_to_relative_path",
			setup: func(dir string) string {
				p := filepath.Join(dir, "relative-link")
				_ = os.Symlink("../other", p)
				return p
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.setup(dir)
			got := isManagedSymlink(path)
			if got != tt.want {
				t.Errorf("isManagedSymlink(%q) = %v, want %v", path, got, tt.want)
			}
		})
	}
}

func TestEnsureSymlinkPathSkipsUserOwnedSymlink(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "my-link")
	target := filepath.Join(dir, "desired-target")

	// Create user-owned symlink.
	if err := os.Symlink("/some/user/path", linkPath); err != nil {
		t.Fatalf("create user symlink: %v", err)
	}

	result := ensureSymlinkPath(linkPath, target, false)
	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if !result.skippedUserOwned {
		t.Fatal("expected skippedUserOwned=true")
	}
	if result.alreadySynced {
		t.Fatal("expected alreadySynced=false")
	}
	// Original symlink should be untouched.
	got, _ := os.Readlink(linkPath)
	if got != "/some/user/path" {
		t.Fatalf("expected user symlink preserved, got target %q", got)
	}
}

func TestEnsureSymlinkPathSkipsUserOwnedDirectory(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "my-dir")
	target := filepath.Join(dir, "desired-target")

	// Create user-owned directory.
	if err := os.MkdirAll(linkPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	result := ensureSymlinkPath(linkPath, target, false)
	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if !result.skippedUserOwned {
		t.Fatal("expected skippedUserOwned=true")
	}
	// Directory should still exist.
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("expected directory to still exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory, got something else")
	}
}

func TestEnsureSymlinkPathReplacesManagedSymlink(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")

	dir := t.TempDir()
	linkPath := filepath.Join(dir, "managed-link")
	newTarget := filepath.Join(dir, "new-target")

	// Create managed symlink (target under cache dir).
	oldTarget := filepath.Join(cacheDir, "compiled", "old-skill")
	if err := os.MkdirAll(filepath.Dir(oldTarget), 0o755); err != nil {
		t.Fatalf("mkdir old target: %v", err)
	}
	if err := os.WriteFile(oldTarget, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old target: %v", err)
	}
	if err := os.Symlink(oldTarget, linkPath); err != nil {
		t.Fatalf("create managed symlink: %v", err)
	}

	result := ensureSymlinkPath(linkPath, newTarget, false)
	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if result.skippedUserOwned {
		t.Fatal("expected skippedUserOwned=false for managed symlink")
	}
	// Symlink should now point to the new target.
	got, _ := os.Readlink(linkPath)
	if got != newTarget {
		t.Fatalf("expected symlink to point to %q, got %q", newTarget, got)
	}
}

func TestStaleArtifactLinksPreservesUserSymlinks(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")

	outDir := t.TempDir()
	desired := map[string]string{"keep.txt": "/some/target"}

	// Create a managed stale symlink (under cache).
	managedStalePath := filepath.Join(outDir, "stale-managed.txt")
	if err := os.Symlink(filepath.Join(cacheDir, "compiled", "stale"), managedStalePath); err != nil {
		t.Fatalf("create managed stale symlink: %v", err)
	}

	// Create a user-owned stale symlink (not under cache).
	userStalePath := filepath.Join(outDir, "stale-user.txt")
	if err := os.Symlink("/nonexistent", userStalePath); err != nil {
		t.Fatalf("create user stale symlink: %v", err)
	}

	// Create a user regular file.
	userFilePath := filepath.Join(outDir, "user-file.txt")
	_ = os.WriteFile(userFilePath, []byte("data"), 0o644)

	stale, diags, err := staleArtifactLinks(outDir, desired)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the managed stale symlink should be reported.
	if !slices.Contains(stale, "stale-managed.txt") {
		t.Fatalf("expected stale-managed.txt in stale, got %v", stale)
	}
	if slices.Contains(stale, "stale-user.txt") {
		t.Fatal("user-owned symlink should NOT be reported as stale")
	}
	if slices.Contains(stale, "user-file.txt") {
		t.Fatal("user regular file should NOT be reported as stale")
	}

	// Check for collision diagnostic about the user symlink.
	var userDiags []diag.Diagnostic
	for _, d := range diags {
		if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, "stale-user.txt") {
			userDiags = append(userDiags, d)
		}
	}
	if len(userDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic for stale-user.txt")
	}
}

func TestSyncSkillLinksPreservesUserSymlinks(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")

	skillsDir := t.TempDir()
	links := []skillLink{
		{name: "managed-skill", target: filepath.Join(cacheDir, "compiled", "managed-skill")},
	}

	// Create a user-owned symlink skill (not under cache).
	userSymlinkPath := filepath.Join(skillsDir, "user-skill")
	if err := os.Symlink("/my/custom/skill", userSymlinkPath); err != nil {
		t.Fatalf("create user symlink skill: %v", err)
	}

	// Create a stale managed symlink skill (under cache, not in desired).
	staleManagedPath := filepath.Join(skillsDir, "stale-managed")
	if err := os.Symlink(filepath.Join(cacheDir, "compiled", "old-skill"), staleManagedPath); err != nil {
		t.Fatalf("create stale managed symlink: %v", err)
	}

	report, err := syncSkillLinks(skillsDir, links, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User symlink must be preserved.
	if _, lerr := os.Lstat(userSymlinkPath); os.IsNotExist(lerr) {
		t.Fatal("expected user-owned skill symlink to be preserved")
	}

	// Stale managed symlink must be removed.
	if _, lerr := os.Lstat(staleManagedPath); !os.IsNotExist(lerr) {
		t.Fatal("expected stale managed symlink to be removed")
	}

	// User symlink should NOT appear in Removed.
	if slices.Contains(report.Removed, "user-skill") {
		t.Fatal("user-owned skill symlink should NOT appear in Removed")
	}

	// Stale managed symlink should appear in Removed.
	if !slices.Contains(report.Removed, "stale-managed") {
		t.Fatalf("expected stale-managed in Removed, got %v", report.Removed)
	}

	// Collision diagnostic for user symlink.
	var userDiags []diag.Diagnostic
	for _, d := range report.Diags {
		if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, "user-skill") {
			userDiags = append(userDiags, d)
		}
	}
	if len(userDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic for user-skill")
	}
}

func TestCollisionRemoveHint(t *testing.T) {
	tests := []struct {
		name         string
		fileName     string
		meta         skillLink
		wantContains []string
	}{
		{
			name:     "skill uses name only",
			fileName: "my-skill",
			meta:     skillLink{kind: ir.KindSkill, resourceName: "my-skill"},
			wantContains: []string{
				`agent-marketplace remove "my-skill"`,
			},
		},
		{
			name:     "subagent includes --kind flag",
			fileName: "scrutiny-feature-reviewer.md",
			meta:     skillLink{kind: ir.KindSubagent, resourceName: "scrutiny-feature-reviewer"},
			wantContains: []string{
				`--kind subagent`,
				`"scrutiny-feature-reviewer"`,
			},
		},
		{
			name:     "command includes --kind flag",
			fileName: "build.md",
			meta:     skillLink{kind: ir.KindCommand, resourceName: "build"},
			wantContains: []string{
				`--kind command`,
				`"build"`,
			},
		},
		{
			name:     "fallback strips extension when resourceName empty",
			fileName: "my-agent.md",
			meta:     skillLink{kind: ir.KindSubagent},
			wantContains: []string{
				`--kind subagent`,
				`"my-agent"`,
			},
		},
		{
			name:     "machine-scope adds --scope user",
			fileName: "user-testing-flow-validator.md",
			meta:     skillLink{kind: ir.KindSubagent, resourceName: "user-testing-flow-validator", machineScope: true},
			wantContains: []string{
				`--kind subagent`,
				`--scope user`,
				`"user-testing-flow-validator"`,
			},
		},
		{
			name:     "skill machine-scope adds --scope user only",
			fileName: "my-skill",
			meta:     skillLink{kind: ir.KindSkill, resourceName: "my-skill", machineScope: true},
			wantContains: []string{
				`--scope user`,
				`"my-skill"`,
			},
		},
		{
			name:     "registry url and path included in hint",
			fileName: "scrutiny-feature-reviewer.md",
			meta: skillLink{
				kind:         ir.KindSubagent,
				resourceName: "scrutiny-feature-reviewer",
				machineScope: true,
				registryURL:  "git@gitlab.example.com:org/registry",
				registryPath: "plugins/personal",
			},
			wantContains: []string{
				`--kind subagent`,
				`--scope user`,
				`--from git@gitlab.example.com:org/registry`,
				`--path plugins/personal`,
				`"scrutiny-feature-reviewer"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath := "/some/abs/" + tt.fileName
			got := collisionRemoveHint(absPath, tt.fileName, tt.meta)
			// Absolute path must appear in the rename suggestion.
			if !strings.Contains(got, absPath) {
				t.Errorf("collisionRemoveHint: expected absolute path %q in message, got: %q", absPath, got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("collisionRemoveHint(%q) = %q; want substring %q", tt.fileName, got, want)
				}
			}
		})
	}
}

func TestSyncSkillLinksCollisionHintForSubagent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")

	skillsDir := t.TempDir()

	// Create a user-owned regular file with the same name as a managed subagent.
	userFilePath := filepath.Join(skillsDir, "scrutiny-feature-reviewer.md")
	if err := os.WriteFile(userFilePath, []byte("# my droid\n"), 0o644); err != nil {
		t.Fatalf("create user file: %v", err)
	}

	links := []skillLink{
		{
			name:         "scrutiny-feature-reviewer.md",
			target:       filepath.Join(cacheDir, "compiled", "scrutiny-feature-reviewer.md"),
			kind:         ir.KindSubagent,
			resourceName: "scrutiny-feature-reviewer",
		},
	}

	report, err := syncSkillLinks(skillsDir, links, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var collDiags []diag.Diagnostic
	for _, d := range report.Diags {
		if d.Code == "materialize.collision.user-owned" {
			collDiags = append(collDiags, d)
		}
	}
	if len(collDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic")
	}
	msg := collDiags[0].Message
	if !strings.Contains(msg, "--kind subagent") {
		t.Errorf("expected --kind subagent in message, got: %q", msg)
	}
	if strings.Contains(msg, "scrutiny-feature-reviewer.md") && !strings.Contains(msg, `"scrutiny-feature-reviewer"`) {
		t.Errorf("expected resource name without extension in remove cmd, got: %q", msg)
	}
	// The absolute path of the conflicting file must be in the rename suggestion.
	if !strings.Contains(msg, userFilePath) {
		t.Errorf("expected absolute path %q in collision message, got: %q", userFilePath, msg)
	}
}

func TestRunEmitsCollisionDiagnosticForUserOwnedNonSymlinkSkill(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	// Create a user-owned skill directory with the same name as a resolved skill.
	skillDir := filepath.Join(repoRoot, ".factory", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir user skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Review Skill\n"), 0o644); err != nil {
		t.Fatalf("write user skill: %v", err)
	}

	sourceSkillDir := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir source skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Marketplace review\n---\n\nReview body"), 0o644); err != nil {
		t.Fatalf("write source skill: %v", err)
	}

	irSkill := ir.NewSkill("review", "Marketplace review", "Review body", nil, nil, ir.SourceRef{File: filepath.Join(sourceSkillDir, "SKILL.md"), Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "review", Path: sourceSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// User-owned skill directory must still exist.
	userSKILLMd := filepath.Join(skillDir, "SKILL.md")
	content, readErr := os.ReadFile(userSKILLMd)
	if readErr != nil {
		t.Fatalf("expected user skill SKILL.md to still exist: %v", readErr)
	}
	if string(content) != "# My Review Skill\n" {
		t.Fatalf("expected user content preserved, got %q", content)
	}

	// Collision diagnostic should be emitted.
	var collisionDiags []diag.Diagnostic
	for _, d := range report.Diags {
		if d.Code == "materialize.collision.user-owned" && strings.Contains(d.Message, "review") {
			collisionDiags = append(collisionDiags, d)
		}
	}
	if len(collisionDiags) == 0 {
		t.Fatal("expected materialize.collision.user-owned diagnostic for review")
	}
}

func TestRunCommandResourceOneTarget(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("greet", "Greeting command", "echo hello", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "greet", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tr, ok := report.PerTarget[hcc.TargetFactory]
	if !ok {
		t.Fatal("expected Factory target report")
	}
	if len(tr.Written) == 0 {
		t.Fatalf("expected at least one written file, got %d", len(tr.Written))
	}

	cmdPath := filepath.Join(repoRoot, ".factory", "commands", "greet.md")
	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		t.Fatalf("expected command file at %s", cmdPath)
	}
	body, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatalf("read command file: %v", err)
	}
	if !strings.Contains(string(body), "echo hello") {
		t.Fatalf("expected command body in output, got %q", body)
	}
}

func TestRunWindsurfCommandMachineScopeUsesGlobalWorkflows(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("create-module", "Create a module", "do the thing", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "create-module", Resource: irCmd},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Machine-scoped (global) workflows must land in global_workflows/, the
	// only directory Windsurf scans for machine-wide slash commands.
	globalPath := filepath.Join(homeDir, ".codeium", "windsurf", "global_workflows", "create-module.md")
	if _, err := os.Stat(globalPath); err != nil {
		t.Fatalf("expected workflow at %s: %v", globalPath, err)
	}
	// The legacy workflows/ directory must NOT be used for machine scope.
	wrongPath := filepath.Join(homeDir, ".codeium", "windsurf", "workflows", "create-module.md")
	if _, err := os.Stat(wrongPath); err == nil {
		t.Fatalf("workflow should not be written to machine-scoped workflows/ dir: %s", wrongPath)
	}
}

func TestRunWindsurfCommandRepoScopeUsesWorkflows(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("build", "Build command", "make build", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "build", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Workspace-scoped workflows stay in .windsurf/workflows/.
	repoPath := filepath.Join(repoRoot, ".windsurf", "workflows", "build.md")
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("expected workflow at %s: %v", repoPath, err)
	}
}

func TestRunCommandResourceMultipleTargets(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("multi-cmd", "Multi-target command", "echo multi", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "multi-cmd", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory, hcc.TargetClaude},
		},
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, target := range []hcc.Target{hcc.TargetFactory, hcc.TargetClaude} {
		tr, ok := report.PerTarget[target]
		if !ok {
			t.Fatalf("expected %s target report", target)
		}
		if len(tr.Written) == 0 {
			t.Fatalf("expected at least one written file for %s", target)
		}
	}

	factoryPath := filepath.Join(repoRoot, ".factory", "commands", "multi-cmd.md")
	claudePath := filepath.Join(repoRoot, ".claude", "commands", "multi-cmd.md")
	if _, err := os.Stat(factoryPath); err != nil {
		t.Fatalf("expected factory command at %s: %v", factoryPath, err)
	}
	if _, err := os.Stat(claudePath); err != nil {
		t.Fatalf("expected claude command at %s: %v", claudePath, err)
	}
}

func TestRunCommandIdempotent(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("idem-cmd", "Idempotent command", "echo idem", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "idem-cmd", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}

	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected at least one written file")
	}

	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}
	if report2.TotalWritten() != 0 {
		t.Fatalf("second run: expected 0 written (idempotent), got %d", report2.TotalWritten())
	}
}

func TestRunEmptyResolvedStripsManagedMcpEntries(t *testing.T) {
	// Simulate what deinit does: Run with empty resolved list should strip
	// managed entries from merge-artifact JSON files (like mcp.json).
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	// First: install an MCP server so mcp.json exists with a managed entry.
	mcp := ir.NewMcpServer("my-server", "cmd", []string{"serve"}, map[string]string{"K": "v"}, nil, ir.SourceRef{File: "mcp.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "my-server", Resource: mcp},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	// Add a user-owned entry alongside the managed one.
	factoryDir := filepath.Join(repoRoot, ".factory")
	mcpPath := filepath.Join(factoryDir, "mcp.json")
	userPlusManaged := `{
  "mcpServers": {
    "my-server": {
      "_managed_by": "agent-marketplace",
      "args": [
        "serve"
      ],
      "command": "cmd",
      "env": {
        "K": "v"
      }
    },
    "personal": {
      "command": "my-personal"
    }
  }
}`
	if err := os.WriteFile(mcpPath, []byte(userPlusManaged), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	// Second: Run with empty resolved list (deinit scenario).
	report2, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// Managed entry should be gone; user entry preserved.
	content2, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read mcp.json after prune: %v", err)
	}
	got2 := string(content2)
	if strings.Contains(got2, `"my-server"`) {
		t.Fatalf("expected managed MCP entry to be removed, got:\n%s", got2)
	}
	if !strings.Contains(got2, `"personal"`) {
		t.Fatalf("expected user-owned MCP entry to be preserved, got:\n%s", got2)
	}
	if strings.Contains(got2, `"_managed_by"`) {
		t.Fatalf("expected no _managed_by markers, got:\n%s", got2)
	}

	// Verify report tracks the change (Written for modified file,
	// or Removed if the file was deleted entirely).
	if report2.TotalWritten() == 0 && report2.TotalRemoved() == 0 {
		t.Fatal("expected at least one written or removed item in report")
	}
}

func TestRunEmptyResolvedKeepsEmptyMcpFileIfAllManaged(t *testing.T) {
	// When mcp.json contains only managed entries (no user entries),
	// deinit should leave an empty mcpServers object in place.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	mcp := ir.NewMcpServer("only-managed", "cmd", []string{"serve"}, nil, nil, ir.SourceRef{File: "mcp.json", Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "only-managed", Resource: mcp},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	report1, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first run: expected no error, got %v", err)
	}
	if report1.TotalWritten() == 0 {
		t.Fatal("first run: expected files to be written")
	}

	// Run with empty resolved list (deinit).
	report2, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("second run: expected no error, got %v", err)
	}

	// mcp.json should be preserved with an empty mcpServers object.
	mcpPath := filepath.Join(repoRoot, ".factory", "mcp.json")
	content, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected mcp.json to remain: %v", err)
	}
	got := string(content)
	if strings.Contains(got, `"only-managed"`) {
		t.Fatalf("expected managed MCP entry removed, got:\n%s", got)
	}
	// The container key survives as an empty object. The parse-based assertions
	// below verify that the pruned mcpServers value is still an object and has
	// no members; the exact byte representation is not load-bearing.
	var parsedMcp map[string]any
	if err := json.Unmarshal(content, &parsedMcp); err != nil {
		t.Fatalf("unmarshal pruned mcp.json: %v", err)
	}
	servers, ok := parsedMcp["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers to remain an object, got %T in:\n%s", parsedMcp["mcpServers"], got)
	}
	if len(servers) != 0 {
		t.Fatalf("expected empty mcpServers object, got:\n%s", got)
	}

	// Verify report tracks the rewrite.
	if report2.TotalWritten() == 0 {
		t.Fatal("expected at least one written item in report")
	}
}

func TestRunEmptyResolvedWindsurfPruneStaysInWindsurfDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	windsurfDir := filepath.Join(repoRoot, ".windsurf")
	if err := os.MkdirAll(windsurfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(windsurfDir, "mcp_config.json")
	if err := os.WriteFile(mcpPath, []byte(`{
  "mcpServers": {
    "managed": {"command": "managed", "_managed_by": "agent-marketplace"},
    "personal": {"command": "personal"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectJSONPath := filepath.Join(repoRoot, "package.json")
	projectJSON := `{
  "dependencies": {
    "agent-marketplace": {"version": "1.0.0", "_managed_by": "agent-marketplace"}
  }
}`
	if err := os.WriteFile(projectJSONPath, []byte(projectJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	projectContent, err := os.ReadFile(projectJSONPath)
	if err != nil {
		t.Fatalf("expected project JSON outside .windsurf to be preserved: %v", err)
	}
	if string(projectContent) != projectJSON {
		t.Fatalf("expected project JSON outside .windsurf to be untouched, got:\n%s", string(projectContent))
	}

	mcpContent, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected mcp_config.json with personal entry to remain: %v", err)
	}
	got := string(mcpContent)
	if strings.Contains(got, `"managed"`) {
		t.Fatalf("expected managed Windsurf MCP entry removed, got:\n%s", got)
	}
	if !strings.Contains(got, `"personal"`) {
		t.Fatalf("expected personal Windsurf MCP entry preserved, got:\n%s", got)
	}
}

func TestPruneManagedMergeFilesPreservesNonJsonFiles(t *testing.T) {
	outDir := t.TempDir()
	// Write a non-JSON file that should be left alone.
	if err := os.MkdirAll(filepath.Join(outDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "skills", "my-skill.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write a JSON file with managed entries.
	mcpContent := `{
  "mcpServers": {
    "managed-one": {"command": "c1", "_managed_by": "agent-marketplace"},
    "user-one": {"command": "personal"}
  }
}`
	if err := os.WriteFile(filepath.Join(outDir, "mcp.json"), []byte(mcpContent), 0o644); err != nil {
		t.Fatal(err)
	}

	report, diags := pruneManagedMergeFiles(outDir, false)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
	if len(report.Written) != 1 || report.Written[0] != "mcp.json" {
		t.Fatalf("expected mcp.json in Written, got %v", report.Written)
	}

	// Managed entry stripped, user entry preserved.
	content, err := os.ReadFile(filepath.Join(outDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Contains(got, `"managed-one"`) {
		t.Fatalf("expected managed entry removed, got:\n%s", got)
	}
	if !strings.Contains(got, `"user-one"`) {
		t.Fatalf("expected user entry preserved, got:\n%s", got)
	}

	// Non-JSON file untouched.
	skillContent, err := os.ReadFile(filepath.Join(outDir, "skills", "my-skill.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(skillContent) != "# Skill" {
		t.Fatalf("expected non-JSON file untouched, got: %s", string(skillContent))
	}
}

func TestPruneManagedMergeFilesReportsWriteError(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "managed-one": {"command": "c1", "_managed_by": "agent-marketplace"},
    "user-one": {"command": "personal"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Skipf("cannot make test file read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o600)
	})

	_, diags := pruneManagedMergeFiles(outDir, false)
	if len(diags) == 0 {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("expected a prune diagnostic, got none; additionally could not read file: %v", readErr)
		}
		if strings.Contains(string(content), `"managed-one"`) {
			t.Fatal("expected a prune diagnostic for read-only managed JSON file, got none")
		}
		t.Skip("filesystem allowed rewriting read-only file")
	}
	// An uncoded write failure still reports at error severity, naming the
	// file, so a prune that did not happen still fails the run.
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	if diags[0].Code != "materialize.write.error" || diags[0].Severity != diag.SeverityError {
		t.Fatalf("diagnostic = %+v, want materialize.write.error at error severity", diags[0])
	}
	if diags[0].Resource != "mcp.json" {
		t.Fatalf("diagnostic resource = %q, want %q", diags[0].Resource, "mcp.json")
	}
}

func TestPruneManagedMergeFilesDryRun(t *testing.T) {
	outDir := t.TempDir()
	mcpContent := `{
  "mcpServers": {
    "managed-one": {"command": "c1", "_managed_by": "agent-marketplace"}
  }
}`
	if err := os.WriteFile(filepath.Join(outDir, "mcp.json"), []byte(mcpContent), 0o644); err != nil {
		t.Fatal(err)
	}

	report, diags := pruneManagedMergeFiles(outDir, true)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}

	// Dry-run should report the rewrite but not modify the file.
	if len(report.Written) != 1 || report.Written[0] != "mcp.json" || len(report.Removed) != 0 {
		t.Fatalf("expected mcp.json in Written for dry-run, got Written=%v Removed=%v", report.Written, report.Removed)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	// File should still contain the managed entry (dry-run).
	if !strings.Contains(string(content), `"managed-one"`) {
		t.Fatalf("dry-run should not modify the file, got:\n%s", string(content))
	}
}

func TestStripManagedEntriesFromFileSkipsFilesWithoutMarker(t *testing.T) {
	outDir := t.TempDir()
	noMarker := `{"mcpServers": {"personal": {"command": "c"}}}`
	path := filepath.Join(outDir, "mcp.json")
	if err := os.WriteFile(path, []byte(noMarker), 0o644); err != nil {
		t.Fatal(err)
	}

	cleaned, removed, err := stripManagedEntriesFromFile(path, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed entries for file without managed markers, got %v", removed)
	}
	if cleaned != nil {
		t.Fatalf("expected nil cleaned bytes for unmodified file, got %s", string(cleaned))
	}
}

func TestStripManagedEntriesFromFileInvalidJson(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "broken.json")
	if err := os.WriteFile(path, []byte(`{not valid json}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, removed, err := stripManagedEntriesFromFile(path, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed entries for invalid JSON, got %v", removed)
	}
}

func TestInjectManagedByFrontmatter(t *testing.T) {
	marker := managedByField + ": " + managedByValue
	t.Run("no frontmatter prepends block", func(t *testing.T) {
		got := string(injectManagedByFrontmatter([]byte("echo hello\n")))
		want := "---\n" + marker + "\n---\n\necho hello\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("existing frontmatter inserts after delimiter", func(t *testing.T) {
		in := "---\ndescription: greet\n---\nbody\n"
		got := string(injectManagedByFrontmatter([]byte(in)))
		want := "---\n" + marker + "\ndescription: greet\n---\nbody\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		once := injectManagedByFrontmatter([]byte("---\ndescription: greet\n---\nbody\n"))
		twice := injectManagedByFrontmatter(once)
		if !strings.HasPrefix(string(once), "---\n"+marker+"\n") {
			t.Fatalf("marker not injected: %q", once)
		}
		if string(once) != string(twice) {
			t.Fatalf("not idempotent: %q vs %q", once, twice)
		}
	})
}

func TestIsManagedCommandFile(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"managed", "---\n" + managedByField + ": " + managedByValue + "\ndescription: x\n---\nbody\n", true},
		{"user no marker", "---\ndescription: x\n---\nbody\n", false},
		{"no frontmatter", "echo hello\n", false},
		{"marker only in body", "echo " + managedByField + "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".md")
			if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := isManagedCommandFile(p); got != tc.want {
				t.Fatalf("isManagedCommandFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestRunFactoryCommandMaterializedAsFile verifies the Factory file-copy
// workaround: commands are written as real files (not symlinks) carrying the
// _managed_by marker, and the same run still symlinks a Claude command.
func TestRunFactoryCommandMaterializedAsFile(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("deploy", "Deploy command", "echo deploy", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "deploy", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory, hcc.TargetClaude},
		},
	}

	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("run: %v", err)
	}

	factoryPath := filepath.Join(repoRoot, ".factory", "commands", "deploy.md")
	info, err := os.Lstat(factoryPath)
	if err != nil {
		t.Fatalf("lstat factory command: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected factory command to be a regular file, got symlink")
	}
	body, err := os.ReadFile(factoryPath)
	if err != nil {
		t.Fatalf("read factory command: %v", err)
	}
	if !strings.Contains(string(body), managedByField+": "+managedByValue) {
		t.Fatalf("expected _managed_by marker in factory command, got %q", body)
	}
	// The authored frontmatter (description) must materialize alongside the marker.
	if !strings.Contains(string(body), "description: Deploy command") {
		t.Fatalf("expected description frontmatter in factory command, got %q", body)
	}
	if !strings.Contains(string(body), "echo deploy") {
		t.Fatalf("expected command body, got %q", body)
	}

	// Claude command in the same run must remain a symlink (Factory-only scope).
	claudePath := filepath.Join(repoRoot, ".claude", "commands", "deploy.md")
	cinfo, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("lstat claude command: %v", err)
	}
	if cinfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected claude command to remain a symlink")
	}
}

func TestRunFactoryCommandIdempotentAndPrunes(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	irCmd := ir.NewCommand("deploy", "Deploy command", "echo deploy", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "deploy", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first run: %v", err)
	}
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if slices.Contains(report2.PerTarget[hcc.TargetFactory].Written, "commands/deploy.md") {
		t.Fatalf("second run should not rewrite, written=%v", report2.PerTarget[hcc.TargetFactory].Written)
	}

	// Removing the command from config prunes the managed file.
	report3, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("prune run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "commands", "deploy.md")); !os.IsNotExist(err) {
		t.Fatalf("expected managed command file to be pruned")
	}
	if !slices.Contains(report3.PerTarget[hcc.TargetFactory].Removed, "commands/deploy.md") {
		t.Fatalf("expected commands/deploy.md in removed, got %v", report3.PerTarget[hcc.TargetFactory].Removed)
	}
}

func TestRunFactoryCommandPreservesUserOwnedFile(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cmdDir := filepath.Join(repoRoot, ".factory", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userContent := "echo user-owned\n"
	userPath := filepath.Join(cmdDir, "deploy.md")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	irCmd := ir.NewCommand("deploy", "Deploy command", "echo deploy", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "deploy", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read user file: %v", err)
	}
	if string(got) != userContent {
		t.Fatalf("user-owned file overwritten: %q", got)
	}
	if !hasDiagCode(report.Diags, "materialize.collision.user-owned") {
		t.Fatalf("expected user-owned collision diagnostic")
	}
}

func TestRunFactoryCommandMigratesManagedSymlink(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Simulate a pre-workaround managed symlink pointing into the cache.
	cmdDir := filepath.Join(repoRoot, ".factory", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(homeDir, ".cache", "agent-marketplace")
	cacheTarget := filepath.Join(cacheDir, "compiled", "factory", "abc", "command", "deploy.md")
	if err := os.MkdirAll(filepath.Dir(cacheTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheTarget, []byte("echo deploy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(cmdDir, "deploy.md")
	if err := os.Symlink(cacheTarget, linkPath); err != nil {
		t.Fatal(err)
	}

	irCmd := ir.NewCommand("deploy", "Deploy command", "echo deploy", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "deploy", Resource: irCmd},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("run: %v", err)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected symlink to be migrated to a regular file")
	}
	body, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), managedByField+": "+managedByValue) {
		t.Fatalf("expected _managed_by marker after migration, got %q", body)
	}
}

func TestRunFileModeRejectsManifestIntermediateSymlink(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repoRoot, ".claude", "skills", "escape")); err != nil {
		t.Fatal(err)
	}
	manifest := "version: 1\nentries:\n  - target: claude\n    path: skills/escape/victim\n    digest: sha256:deadbeef\n"
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "materialization-manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("portable manifest followed intermediate symlink and removed external file: %v", err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("expected manifest containment diagnostic, got %+v", report.Diags)
	}
}

func TestRunFileModePrunesStaleManagedSymlinks(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cacheTarget := filepath.Join(homeDir, ".cache", "agent-marketplace", "compiled", "stale")
	if err := os.MkdirAll(filepath.Dir(cacheTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheTarget, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"skills/stale-skill", "commands/stale.md", "agents/stale.md"} {
		path := filepath.Join(repoRoot, ".claude", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cacheTarget, path); err != nil {
			t.Fatal(err)
		}
	}
	userTarget := filepath.Join(t.TempDir(), "user-target")
	if err := os.WriteFile(userTarget, []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	userLink := filepath.Join(repoRoot, ".claude", "skills", "user-skill")
	if err := os.Symlink(userTarget, userLink); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}))
	if err != nil {
		t.Fatalf("file-mode deinit: %v", err)
	}
	for _, rel := range []string{"skills/stale-skill", "commands/stale.md", "agents/stale.md"} {
		if _, err := os.Lstat(filepath.Join(repoRoot, ".claude", filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("stale managed symlink %s was not pruned: %v", rel, err)
		}
	}
	if _, err := os.Lstat(userLink); err != nil {
		t.Fatalf("user-owned symlink was pruned: %v", err)
	}
	if !slices.Contains(report.PerTarget[hcc.TargetClaude].Removed, "skills/stale-skill") {
		t.Fatalf("expected stale skill in report: %+v", report.PerTarget[hcc.TargetClaude])
	}
}

func TestRunFileModeDeinitAfterSymlinkMode(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	sourceDir := filepath.Join(t.TempDir(), "switchable-skill")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("---\nname: switchable-skill\ndescription: switch\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "switchable-skill", Path: sourceDir, Resource: ir.NewSkill("switchable-skill", "switch", "body", nil, nil, ir.SourceRef{File: filepath.Join(sourceDir, "SKILL.md")})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}}
	installed := filepath.Join(repoRoot, ".claude", "skills", "switchable-skill")
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("symlink-mode run: %v", err)
	}
	if info, err := os.Lstat(installed); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink-mode output before mode switch, info=%v err=%v", info, err)
	}
	fileModes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	if _, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(fileModes)); err != nil {
		t.Fatalf("file-mode deinit: %v", err)
	}
	if _, err := os.Lstat(installed); !os.IsNotExist(err) {
		t.Fatalf("file-mode deinit did not prune stale symlink-mode output: %v", err)
	}
}

func TestRunCopiesSkillSupportFilesFromImmutableResourceSource(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	sourceDir := filepath.Join(t.TempDir(), "snapshot", "skills", "review")
	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(sourceDir, "SKILL.md")
	if err := os.WriteFile(sourceFile, []byte("---\nname: review\ndescription: Review\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "review.sh"), []byte("echo review\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{File: sourceFile, Line: 1})
	resource.SupportFiles = []string{"scripts/review.sh"}
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "review", Path: filepath.Join(homeDir, "mutable", "main", "skills", "review", "SKILL.md"), Resource: resource},
		Source:    config.UserPersonal,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("materialize exact-snapshot skill: %v", err)
	}
	if hasDiagCode(report.Diags, diag.CodeSkillSupportFilesDeferred) {
		t.Fatalf("materialized support files reported %s", diag.CodeSkillSupportFilesDeferred)
	}
	installed := filepath.Join(homeDir, ".config", "devin", "skills", "review", "scripts", "review.sh")
	if got, err := os.ReadFile(installed); err != nil || string(got) != "echo review\n" {
		t.Fatalf("installed immutable support file = %q, %v", got, err)
	}
}

func TestPrepareSkillLinkCachePrunesRemovedSupportFiles(t *testing.T) {
	sourceDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("source skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	support := filepath.Join(sourceDir, "scripts", "helper.sh")
	if err := os.WriteFile(support, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("initial cache preparation: %v", err)
	}
	if err := os.Remove(support); err != nil {
		t.Fatal(err)
	}
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("reconcile cache preparation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "scripts", "helper.sh")); !os.IsNotExist(err) {
		t.Fatalf("removed support file remains in cache: %v", err)
	}
	// --check builds this virtual digest instead of preparing the cache. It
	// must agree with the real cache after a support file is removed.
	virtualDigest, err := portableSkillVirtualDigest(link)
	if err != nil {
		t.Fatalf("dry-run virtual digest: %v", err)
	}
	realDigest, err := portablePathDigest(cacheDir)
	if err != nil || virtualDigest != realDigest {
		t.Fatalf("real and dry-run support trees diverged: real=%q virtual=%q err=%v", realDigest, virtualDigest, err)
	}
	compiled, err := os.ReadFile(filepath.Join(cacheDir, "SKILL.md"))
	if err != nil || string(compiled) != "compiled skill" {
		t.Fatalf("compiled artifact was not retained as authoritative: %q, %v", compiled, err)
	}
}

// TestPrepareSkillLinkCacheExcludesVCSInternals covers the root-skill cache
// shape: when the skill directory is a whole registry checkout, its VCS
// internals must never reach the materialized cache that harness symlinks
// (and file-mode copies) expose to every workspace.
func TestPrepareSkillLinkCacheExcludesVCSInternals(t *testing.T) {
	sourceDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("source skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "tools", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// VCS internals at the checkout root and nested inside a support directory.
	for _, gitRel := range []string{
		filepath.Join(".git", "HEAD"),
		filepath.Join(".git", "objects", "pack-0.pack"),
		filepath.Join("tools", ".git", "config"),
	} {
		if err := os.MkdirAll(filepath.Join(sourceDir, filepath.Dir(gitRel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, gitRel), []byte("git internal"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("cache preparation: %v", err)
	}
	assertNoVCSInternals := func(label string) {
		t.Helper()
		err := filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(cacheDir, path)
			if relErr != nil {
				return relErr
			}
			for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
				if part == ".git" {
					return fmt.Errorf("%s leaked VCS internals into cache at %s", label, path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	assertNoVCSInternals("fresh copy")
	for name, want := range map[string]string{"README.md": "readme", filepath.Join("tools", "run.sh"): "#!/bin/sh\n"} {
		data, err := os.ReadFile(filepath.Join(cacheDir, name))
		if err != nil || string(data) != want {
			t.Fatalf("expected support file %s with %q, got %q (err=%v)", name, want, string(data), err)
		}
	}
	// A stale .git planted by an earlier run must be pruned on reconcile, not
	// merely left behind while fresh copies stop adding it.
	if err := os.MkdirAll(filepath.Join(cacheDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, ".git", "HEAD"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("reconcile cache preparation: %v", err)
	}
	assertNoVCSInternals("reconcile")
	// --check builds a virtual digest that must agree with the real cache.
	virtualDigest, err := portableSkillVirtualDigest(link)
	if err != nil {
		t.Fatalf("dry-run virtual digest: %v", err)
	}
	realDigest, err := portablePathDigest(cacheDir)
	if err != nil || virtualDigest != realDigest {
		t.Fatalf("real and dry-run support trees diverged: real=%q virtual=%q err=%v", realDigest, virtualDigest, err)
	}
	compiled, err := os.ReadFile(filepath.Join(cacheDir, "SKILL.md"))
	if err != nil || string(compiled) != "compiled skill" {
		t.Fatalf("compiled artifact was not retained as authoritative: %q, %v", compiled, err)
	}
}

func TestSkillCacheAndVirtualDigestExcludeGitPointers(t *testing.T) {
	for _, tc := range []struct {
		name string
		add  func(string) error
	}{
		{name: "file", add: func(path string) error {
			return os.WriteFile(path, []byte("gitdir: ../worktrees/checkout"), 0o644)
		}},
		{name: "dangling symlink", add: func(path string) error {
			return os.Symlink("missing-gitdir", path)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceDir := t.TempDir()
			cacheDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(sourceDir, "tools"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"SKILL.md": "source skill", "tools/run.sh": "retained support"} {
				if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, parent := range []string{sourceDir, filepath.Join(sourceDir, "tools")} {
				if err := tc.add(filepath.Join(parent, ".git")); err != nil {
					t.Fatal(err)
				}
			}
			link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
			digest, err := portableSkillVirtualDigest(link)
			if err != nil {
				t.Fatalf("virtual digest: %v", err)
			}
			if err := prepareSkillLinkCache(link); err != nil {
				t.Fatalf("cache preparation: %v", err)
			}
			for _, rel := range []string{".git", "tools/.git"} {
				if _, err := os.Lstat(filepath.Join(cacheDir, rel)); !os.IsNotExist(err) {
					t.Fatalf("cache entry %s must be absent, got %v", rel, err)
				}
			}
			for name, want := range map[string]string{"SKILL.md": "compiled skill", "tools/run.sh": "retained support"} {
				got, err := os.ReadFile(filepath.Join(cacheDir, name))
				if err != nil || string(got) != want {
					t.Fatalf("cache file %s = %q, %v; want %q", name, got, err, want)
				}
			}
			actual, err := portablePathDigest(cacheDir)
			if err != nil || digest != actual {
				t.Fatalf("virtual digest %q differs from cache digest %q: %v", digest, actual, err)
			}
		})
	}
}

func TestRunFileModePrunesManifestWhenHarnessRootIsAbsent(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(repoRoot, ".agents", "materialization-manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("version: 1\nentries:\n  - target: claude\n    path: skills/gone\n    digest: sha256:deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest was not cleaned when harness root was absent: %v", err)
	}
}

func TestRunFileModePortableManifestPruneAndMigration(t *testing.T) {
	for _, target := range []struct {
		name        hcc.Target
		projectRoot string
		userRoot    string
	}{
		{hcc.TargetClaude, ".claude", ".claude"},
		{hcc.TargetFactory, ".factory", ".factory"},
		{hcc.TargetCursor, ".cursor", ".cursor"},
		{hcc.TargetWindsurf, ".windsurf", ".codeium/windsurf"},
		{hcc.TargetGemini, ".gemini", ".gemini"},
		{hcc.TargetDevin, ".devin", ".config/devin"},
		{hcc.TargetCodex, ".agents", ".agents"},
	} {
		for _, machine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine=%v", target.name, machine), func(t *testing.T) {
				repoRoot, homeDir := devinEnv(t)
				t.Setenv("CODEX_HOME", filepath.Join(homeDir, ".codex"))
				root, source := filepath.Join(repoRoot, target.projectRoot), config.RepoCommitted
				manifestPath := filepath.Join(repoRoot, ".agents", "materialization-manifest.yaml")
				if machine {
					root, source = filepath.Join(homeDir, target.userRoot), config.UserPersonal
					manifestPath = filepath.Join(homeDir, ".agent-marketplace", "materialization-manifest.yaml")
				}

				sourceDir := filepath.Join(t.TempDir(), "portable-skill")
				if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("---\nname: portable-skill\ndescription: portable\n---\n\nbody"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "helper.sh"), []byte("echo portable"), 0o755); err != nil {
					t.Fatal(err)
				}
				resource := ir.NewSkill("portable-skill", "portable", "body", nil, nil, ir.SourceRef{File: filepath.Join(sourceDir, "SKILL.md")})
				resolved := []resolver.ResolvedArtifact{{
					Entry:     catalog.Entry{Name: "portable-skill", Path: sourceDir, Resource: resource},
					Source:    source,
					Harnesses: []hcc.Target{target.name},
				}, {
					Entry:     catalog.Entry{Name: "compiled-only", Resource: ir.NewSkill("compiled-only", "compiled skill", "compiled body", nil, nil, ir.SourceRef{})},
					Source:    source,
					Harnesses: []hcc.Target{target.name},
				}}
				fileModes := config.MaterializationModes{Machine: config.MaterializationModeFile, Project: config.MaterializationModeFile}
				run := func(resources []resolver.ResolvedArtifact, options ...RunOption) *Report {
					t.Helper()
					report, err := Run(context.Background(), repoRoot, resources, options...)
					if err != nil {
						t.Fatalf("Run: %v", err)
					}
					requireNoErrorDiags(t, report.Diags)
					return report
				}
				run(resolved, WithMaterializationModes(fileModes))

				installed := filepath.Join(root, "skills", "portable-skill")
				info, err := os.Lstat(installed)
				if err != nil {
					t.Fatalf("stat portable skill: %v", err)
				}
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					t.Fatalf("file mode should install a regular skill tree, got mode %v", info.Mode())
				}
				if got := readFileString(t, filepath.Join(installed, "scripts", "helper.sh")); got != "echo portable" {
					t.Fatalf("portable support file = %q", got)
				}
				helperInfo, err := os.Lstat(filepath.Join(installed, "scripts", "helper.sh"))
				if err != nil || !helperInfo.Mode().IsRegular() || helperInfo.Mode().Perm() != 0o755 {
					t.Fatalf("portable support file mode: info=%v err=%v", helperInfo, err)
				}
				manifest, err := os.ReadFile(manifestPath)
				if err != nil {
					t.Fatalf("read project manifest: %v", err)
				}
				if strings.Contains(string(manifest), repoRoot) || !strings.Contains(string(manifest), "skills/portable-skill") || !strings.Contains(string(manifest), "digest: sha256:") {
					t.Fatalf("manifest must contain portable target-relative digest state, got:\n%s", manifest)
				}
				second := run(resolved, WithMaterializationModes(fileModes))
				if second.TotalWritten() != 0 || !second.InSync() {
					t.Fatalf("unchanged file mode output should be in sync: %+v", second)
				}
				if check := run(resolved, WithMaterializationModes(fileModes), WithDryRun()); !check.InSync() {
					t.Fatalf("unchanged file mode output has drift: %+v", check)
				}

				run(resolved)
				info, err = os.Lstat(installed)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink migration should replace manifest-owned tree, info=%v err=%v", info, err)
				}
				if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
					t.Fatalf("manifest should be removed after migration to symlink mode, err=%v", err)
				}

				run(resolved, WithMaterializationModes(fileModes))
				info, err = os.Lstat(installed)
				if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("symlink-to-file migration: info=%v err=%v", info, err)
				}
				if check := run(nil, WithMaterializationModes(fileModes), WithDryRun()); check.InSync() {
					t.Fatal("deselected installed resource must report prune drift")
				}
				if _, err := os.Stat(installed); err != nil {
					t.Fatalf("dry run changed installed resource: %v", err)
				}
				run(nil, WithMaterializationModes(fileModes))
				if _, err := os.Lstat(installed); !os.IsNotExist(err) {
					t.Fatalf("file mode prune should remove manifest-owned tree, err=%v", err)
				}
			})
		}
	}
}

func TestCodexPortableManifestAloneSchedulesReconciliation(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	state, err := loadPortableState(repoRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	state.add(hcc.TargetCodex, ".agents/skills/demo/SKILL.md", "sha256:demo")
	if err := state.save(false); err != nil {
		t.Fatal(err)
	}

	outputs := make(map[outputKey]bool)
	var diagnostics []diag.Diagnostic
	addPortableManifestOutputKeys(repoRoot, false, outputs, &diagnostics)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !outputs[outputKey{target: hcc.TargetCodex}] {
		t.Fatalf("Codex manifest did not schedule reconciliation: %#v", outputs)
	}
}

func TestSyncPortableArtifactFilesUsesDefaultFileModeForDigest(t *testing.T) {
	outDir := t.TempDir()
	cacheDir := t.TempDir()
	compiledArtifact := artifact.Artifact{Path: "command.md", Bytes: []byte("command body")}
	if err := os.WriteFile(filepath.Join(cacheDir, compiledArtifact.Path), compiledArtifact.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state := &portableState{entries: make(map[string]portableManifestEntry)}
	if _, _, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, nil, state, hcc.TargetClaude, "commands", map[string]bool{}); err != nil {
		t.Fatalf("first syncPortableArtifactFiles: %v", err)
	}
	report, _, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, nil, state, hcc.TargetClaude, "commands", map[string]bool{})
	if err != nil {
		t.Fatalf("second syncPortableArtifactFiles: %v", err)
	}
	if len(report.Written) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("second materialization should be idempotent, got written=%v skipped=%v", report.Written, report.Skipped)
	}
}

// TestSyncPortableArtifactFilesStaleManifestDigestNoFalseCollision verifies
// that when the on-disk output already equals the desired output, a stale
// recorded digest (e.g. from a merge, cherry-pick, or cross-CLI-version
// checkout) does not produce a false collision. The entry must be re-stamped
// and the file treated as in sync so later genuine updates are not suppressed.
func TestSyncPortableArtifactFilesStaleManifestDigestNoFalseCollision(t *testing.T) {
	outDir := t.TempDir()
	cacheDir := t.TempDir()
	compiledArtifact := artifact.Artifact{Path: "command.md", Bytes: []byte("command body")}
	if err := os.WriteFile(filepath.Join(cacheDir, compiledArtifact.Path), compiledArtifact.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	target := hcc.TargetClaude
	manifestPath := "commands/command.md"
	state := &portableState{entries: make(map[string]portableManifestEntry)}
	if _, _, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, nil, state, target, "commands", map[string]bool{}); err != nil {
		t.Fatalf("initial syncPortableArtifactFiles: %v", err)
	}

	// Simulate a stale recorded digest while the on-disk file still equals the
	// desired output (as happens after a cross-version checkout).
	state.add(target, manifestPath, "sha256:stale")

	report, diags, err := syncPortableArtifactFiles(outDir, []artifact.Artifact{compiledArtifact}, cacheDir, false, false, nil, state, target, "commands", map[string]bool{})
	if err != nil {
		t.Fatalf("re-sync with stale digest: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("stale digest with matching output must not warn, got diags=%+v", diags)
	}
	if len(report.Written) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("expected file treated as in sync, got written=%v skipped=%v", report.Written, report.Skipped)
	}
	entry, ok := state.entry(target, manifestPath)
	if !ok {
		t.Fatal("expected manifest entry to remain owned")
	}
	if want := portableArtifactDigest(compiledArtifact); entry.Digest != want {
		t.Fatalf("entry digest was not re-stamped: got %q want %q", entry.Digest, want)
	}
}

func TestRunFileModePreservesModifiedManifestOutputs(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetDevin} {
		t.Run(string(target), func(t *testing.T) {
			t.Run("update", func(t *testing.T) {
				repoRoot, resolved, modes, installed := setupPortableFileModeSkill(t, target)
				if _, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(modes)); err != nil {
					t.Fatalf("initial file-mode run: %v", err)
				}
				userContent := []byte("user-owned update\n")
				if err := os.WriteFile(filepath.Join(installed, "SKILL.md"), userContent, 0o644); err != nil {
					t.Fatal(err)
				}
				updated := append([]resolver.ResolvedArtifact(nil), resolved...)
				updated[0].Resource = ir.NewSkill("portable-skill", "portable", "updated body", nil, nil, ir.SourceRef{})
				report, err := Run(context.Background(), repoRoot, updated, WithMaterializationModes(modes))
				if err != nil {
					t.Fatalf("update run: %v", err)
				}
				got, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
				if err != nil || !bytes.Equal(got, userContent) {
					t.Fatalf("modified output was overwritten: %q, err=%v", got, err)
				}
				if !hasDiagCode(report.Diags, "materialize.collision.user-owned") {
					t.Fatalf("expected collision diagnostic, got %+v", report.Diags)
				}
			})

			t.Run("prune", func(t *testing.T) {
				repoRoot, resolved, modes, installed := setupPortableFileModeSkill(t, target)
				if _, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(modes)); err != nil {
					t.Fatalf("initial file-mode run: %v", err)
				}
				if err := os.WriteFile(filepath.Join(installed, "SKILL.md"), []byte("user-owned prune\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				report, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(modes))
				if err != nil {
					t.Fatalf("prune run: %v", err)
				}
				if _, err := os.Stat(installed); err != nil {
					t.Fatalf("modified stale output was removed: %v", err)
				}
				if !hasDiagCode(report.Diags, "materialize.collision.user-owned") {
					t.Fatalf("expected prune collision diagnostic, got %+v", report.Diags)
				}
			})

			t.Run("file-to-symlink migration", func(t *testing.T) {
				repoRoot, resolved, modes, installed := setupPortableFileModeSkill(t, target)
				if _, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(modes)); err != nil {
					t.Fatalf("initial file-mode run: %v", err)
				}
				if err := os.WriteFile(filepath.Join(installed, "SKILL.md"), []byte("user-owned migration\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				report, err := Run(context.Background(), repoRoot, resolved)
				if err != nil {
					t.Fatalf("symlink migration run: %v", err)
				}
				info, err := os.Lstat(installed)
				if err != nil || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("modified file-mode output was migrated: info=%v err=%v", info, err)
				}
				if !hasDiagCode(report.Diags, "materialize.collision.user-owned") {
					t.Fatalf("expected migration collision diagnostic, got %+v", report.Diags)
				}
			})
		})
	}
}

func setupPortableFileModeSkill(t *testing.T, target hcc.Target) (string, []resolver.ResolvedArtifact, config.MaterializationModes, string) {
	t.Helper()
	repoRoot, _ := devinEnv(t)
	sourceDir := filepath.Join(t.TempDir(), "portable-skill")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("---\nname: portable-skill\ndescription: portable\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	resource := ir.NewSkill("portable-skill", "portable", "body", nil, nil, ir.SourceRef{File: filepath.Join(sourceDir, "SKILL.md")})
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "portable-skill", Path: sourceDir, Resource: resource},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{target},
	}}
	modes := config.MaterializationModes{Machine: config.MaterializationModeSymlink, Project: config.MaterializationModeFile}
	return repoRoot, resolved, modes, filepath.Join(repoRoot, "."+string(target), "skills", "portable-skill")
}

func TestRunMaterializationModesAreScopeSpecific(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "user-file", Resource: ir.NewSkill("user-file", "user", "body", nil, nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetClaude}},
		{Entry: catalog.Entry{Name: "repo-link", Resource: ir.NewSkill("repo-link", "repo", "body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetClaude}},
	}
	modes := config.MaterializationModes{Machine: config.MaterializationModeFile, Project: config.MaterializationModeSymlink}
	if _, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	machinePath := filepath.Join(homeDir, ".claude", "skills", "user-file")
	projectPath := filepath.Join(repoRoot, ".claude", "skills", "repo-link")
	machineInfo, err := os.Lstat(machinePath)
	if err != nil || machineInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("machine output should use user file mode, info=%v err=%v", machineInfo, err)
	}
	projectInfo, err := os.Lstat(projectPath)
	if err != nil || projectInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("project output should retain project symlink override, info=%v err=%v", projectInfo, err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".agent-marketplace", "materialization-manifest.yaml")); err != nil {
		t.Fatalf("machine file mode should use local user manifest: %v", err)
	}
}

func TestCommandMarkerInjectionIgnoresBodyText(t *testing.T) {
	const marker = "_managed_by: agent-marketplace"
	for _, tc := range []struct{ name, input, want string }{
		{"body only", marker + "\n", "---\n" + marker + "\n---\n\n" + marker + "\n"},
		{"fenced body", "---\ndescription: sample\n---\n```yaml\n" + marker + "\n```\n", "---\n" + marker + "\ndescription: sample\n---\n```yaml\n" + marker + "\n```\n"},
		{"description text", "---\ndescription: 'example _managed_by: agent-marketplace'\n---\nBody\n", "---\n" + marker + "\ndescription: 'example _managed_by: agent-marketplace'\n---\nBody\n"},
		{"already managed", "---\n" + marker + "\ndescription: sample\n---\nBody\n", "---\n" + marker + "\ndescription: sample\n---\nBody\n"},
		{"leading whitespace", "\ufeff \n---\ndescription: sample\n---\n" + marker + "\n", "\ufeff \n---\n" + marker + "\ndescription: sample\n---\n" + marker + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := injectManagedByFrontmatter([]byte(tc.input))
			if string(got) != tc.want {
				t.Fatalf("marker was not injected into frontmatter: got %q want %q", got, tc.want)
			}
			if twice := injectManagedByFrontmatter(got); !bytes.Equal(got, twice) {
				t.Fatalf("injection is not idempotent: %q", twice)
			}
			path := filepath.Join(t.TempDir(), "command.md")
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}
			if !isManagedCommandFile(path) {
				t.Fatal("injection and cleanup disagree on ownership")
			}
		})
	}
}
