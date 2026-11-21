package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: application-target-model/unaccepted-kind-is-refused-before-output
func TestRunRejectsUnacceptedKindBeforeOutput(t *testing.T) {
	originalLookup := lookupApplicationTarget
	lookupApplicationTarget = func(target hcc.Target) (harness.ApplicationTarget, bool, error) {
		if target == hcc.TargetClaude {
			return harness.NewApplicationTarget(string(target), []ir.ResourceKind{ir.KindSkill}), true, nil
		}
		return originalLookup(target)
	}
	t.Cleanup(func() { lookupApplicationTarget = originalLookup })

	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSkill, Name: "accepted-skill", Resource: ir.NewSkill("accepted-skill", "accepted", "skill body", nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}
	hooks, _ := sourceBackedHookFixtures(t, "claude", config.RepoCommitted, hcc.TargetClaude, "refused-hook",
		[]byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo refused"}]}]}`), nil)
	resolved = append(resolved, hooks...)
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	refusal := false
	for _, diagnostic := range report.Diags {
		message := strings.ToLower(diagnostic.Message)
		if diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetClaude) &&
			strings.Contains(message, "hook") && strings.Contains(message, "claude") {
			refusal = true
			break
		}
	}
	if !refusal {
		t.Fatalf("diagnostics do not refuse hook delivery to Claude: %#v", report.Diags)
	}

	skillPath := filepath.Join(repoRoot, ".claude", "skills", "accepted-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("accepted skill was not delivered: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", "skills", "refused-hook", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("refused hook was delivered as a skill: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("refused hook aggregate exists: err=%v", err)
	}
}
