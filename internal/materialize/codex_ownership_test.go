package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestCodexHooksReconcileWithDurableOwnership(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	hooksPath := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\n  // handwritten first\n  \"hooks\": {\n    \"PreToolUse\": [{\"matcher\":\"Read\",\"hooks\":[{\"type\":\"command\",\"command\":\"./handwritten\"}]}]\n  },\n  \"other\": true\n}\n"
	if err := os.WriteFile(hooksPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: hook}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}

	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("report = %#v", report)
	}
	after, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	for _, want := range []string{"// handwritten first", `"other": true`, "./handwritten", "./managed"} {
		if !strings.Contains(text, want) {
			t.Errorf("hooks lost %q:\n%s", want, text)
		}
	}

	if _, err := Run(context.Background(), repo, nil); err != nil {
		t.Fatal(err)
	}
	pruned, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pruned), "./managed") || !strings.Contains(string(pruned), "./handwritten") {
		t.Fatalf("ownership prune wrong:\n%s", pruned)
	}
}

func hasErrorDiagnostics(diagnostics []diag.Diagnostic) bool { return diag.HasErrors(diagnostics) }
