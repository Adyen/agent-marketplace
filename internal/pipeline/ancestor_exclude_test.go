package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func skillExclude(names ...string) []resourceName {
	out := make([]resourceName, 0, len(names))
	for _, n := range names {
		out = append(out, resourceName{kind: ir.KindSkill, name: n})
	}
	return out
}

func resolvedSkill(name string) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name}}
}

// materializeFactorySkill creates a skill directory under root's factory output.
func materializeFactorySkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, ".factory", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestDetectAncestorExcludes_NoWarningWhenMatRootIsRepoRoot(t *testing.T) {
	repoRoot := t.TempDir()
	materializeFactorySkill(t, repoRoot, "alpha")

	warns := detectAncestorMaterializedExcludes(
		repoRoot, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("alpha"),
		nil,
	)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings when matRoot == repoRoot, got %d: %v", len(warns), warns)
	}
}

func TestDetectAncestorExcludes_WarnsWhenAncestorMaterializesExcludedSkill(t *testing.T) {
	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	materializeFactorySkill(t, repoRoot, "beta")

	warns := detectAncestorMaterializedExcludes(
		sub, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("beta"),
		nil,
	)
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warns), warns)
	}
	var w *AncestorMaterializedExcludeWarning
	if !errors.As(warns[0], &w) {
		t.Fatalf("expected AncestorMaterializedExcludeWarning, got %T", warns[0])
	}
	if w.ProvidingRoot != filepath.Clean(repoRoot) {
		t.Errorf("ProvidingRoot = %q, want %q", w.ProvidingRoot, filepath.Clean(repoRoot))
	}
	if want := filepath.Join(repoRoot, ".agents", "config.local.yaml"); w.ConfigToEdit != want {
		t.Errorf("ConfigToEdit = %q, want %q", w.ConfigToEdit, want)
	}
	var r resolver.Remediable
	if !errors.As(warns[0], &r) {
		t.Fatal("warning should satisfy resolver.Remediable")
	}
}

func TestDetectAncestorExcludes_WarnsWhenAncestorMaterializesCodexSkill(t *testing.T) {
	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	codexSkillDir := filepath.Join(repoRoot, ".agents", "skills", "codex-skill")
	if err := os.MkdirAll(codexSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	warns := detectAncestorMaterializedExcludes(
		sub, repoRoot,
		[]hcc.Target{hcc.TargetCodex},
		skillExclude("codex-skill"),
		nil,
	)
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warns), warns)
	}
	var w *AncestorMaterializedExcludeWarning
	if !errors.As(warns[0], &w) {
		t.Fatalf("expected AncestorMaterializedExcludeWarning, got %T", warns[0])
	}
	if w.Harness != hcc.TargetCodex {
		t.Errorf("Harness = %q, want %q", w.Harness, hcc.TargetCodex)
	}
}

func TestSkillDir_CodexDestinationResolutionErrorFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink loops are not portable on windows")
	}
	repoRoot := t.TempDir()
	agentsRoot := filepath.Join(repoRoot, ".agents")
	if err := os.Symlink(agentsRoot, agentsRoot); err != nil {
		t.Fatal(err)
	}

	if got := skillDir(hcc.TargetCodex, repoRoot, "codex-skill"); got != "" {
		t.Fatalf("skillDir() = %q, want empty path after destination resolution error", got)
	}
}

// TestSkillDir_UsesDescriptorPattern pins that the skill directory probe reads
// the target's native-layout descriptor rather than a second literal copy:
// Claude deliberately leaves TargetInfo.SkillPathPattern unset, so this path
// only resolves because the descriptor declares skills/%s/SKILL.md.
func TestSkillDir_UsesDescriptorPattern(t *testing.T) {
	repoRoot := t.TempDir()
	if got, want := skillDir(hcc.TargetClaude, repoRoot, "demo"), filepath.Join(repoRoot, ".claude", "skills", "demo"); got != want {
		t.Fatalf("skillDir(claude) = %q, want %q", got, want)
	}
	if got, want := skillDir(hcc.TargetDevin, repoRoot, "demo"), filepath.Join(repoRoot, ".devin", "skills", "demo"); got != want {
		t.Fatalf("skillDir(devin) = %q, want %q", got, want)
	}
	// A target with no registered layout still resolves through the literal
	// TargetInfo pattern.
	if got, want := skillDir(hcc.TargetFactory, repoRoot, "demo"), filepath.Join(repoRoot, ".factory", "skills", "demo"); got != want {
		t.Fatalf("skillDir(factory) = %q, want %q", got, want)
	}
}

func TestDetectAncestorExcludes_NoWarningWhenSkillNotMaterializedInAncestor(t *testing.T) {
	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// No skill materialized anywhere: exclude is effective, no warning.
	warns := detectAncestorMaterializedExcludes(
		sub, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("gamma"),
		nil,
	)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %d: %v", len(warns), warns)
	}
}

func TestDetectAncestorExcludes_NoWarningWhenSkillStillResolved(t *testing.T) {
	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	materializeFactorySkill(t, repoRoot, "delta")

	// "delta" is excluded by name but still present in the resolved set
	// (e.g. re-selected at the subdir layer); not an ineffective exclude.
	warns := detectAncestorMaterializedExcludes(
		sub, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("delta"),
		[]resolver.ResolvedArtifact{resolvedSkill("delta")},
	)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings when skill still resolved, got %d: %v", len(warns), warns)
	}
}

func TestDetectAncestorExcludes_ReportsDeepestAncestor(t *testing.T) {
	repoRoot := t.TempDir()
	mid := filepath.Join(repoRoot, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	// Skill materialized at both the repo root and the intermediate dir.
	materializeFactorySkill(t, repoRoot, "epsilon")
	materializeFactorySkill(t, mid, "epsilon")

	warns := detectAncestorMaterializedExcludes(
		leaf, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("epsilon"),
		nil,
	)
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warns), warns)
	}
	var w *AncestorMaterializedExcludeWarning
	if !errors.As(warns[0], &w) {
		t.Fatalf("unexpected type %T", warns[0])
	}
	if w.ProvidingRoot != filepath.Clean(mid) {
		t.Errorf("ProvidingRoot = %q, want deepest ancestor %q", w.ProvidingRoot, filepath.Clean(mid))
	}
}

func TestDetectAncestorExcludes_OnlyScansConfiguredHarness(t *testing.T) {
	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Skill exists only under the Claude output, but config targets Factory.
	claudeDir := filepath.Join(repoRoot, ".claude", "skills", "zeta")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	warns := detectAncestorMaterializedExcludes(
		sub, repoRoot,
		[]hcc.Target{hcc.TargetFactory},
		skillExclude("zeta"),
		nil,
	)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings when shadow is in an unconfigured harness, got %d: %v", len(warns), warns)
	}
}
