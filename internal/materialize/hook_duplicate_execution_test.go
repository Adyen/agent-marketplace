package materialize

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func duplicateExecutionProvider(t *testing.T, name string, machine bool, command string, targets ...hcc.Target) resolver.ResolvedArtifact {
	t.Helper()
	provider := additiveSafetyProvider(t, hcc.TargetFactory, machine, command, "")
	hook, ok := provider.Resource.(*ir.Hook)
	if !ok || hook.SourceDefinitionDigest() == "" {
		t.Fatalf("Factory source hook has no physical definition digest: %#v", provider.Resource)
	}
	provider.Name = name
	provider.Harnesses = append([]hcc.Target(nil), targets...)
	return provider
}

func runDuplicateExecution(t *testing.T, repo string, resolved ...resolver.ResolvedArtifact) *Report {
	t.Helper()
	report, err := Run(context.Background(), repo, resolved)
	if err != nil || report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("materialize selected hooks: report=%+v err=%v", report, err)
	}
	return report
}

func duplicateExecutionWarnings(report *Report) []diag.Diagnostic {
	var warnings []diag.Diagnostic
	if report == nil {
		return warnings
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeHookDuplicateExecutionRisk {
			warnings = append(warnings, diagnostic)
		}
	}
	return warnings
}

func assertDuplicateExecutionWarnings(t *testing.T, report *Report, want []diag.Diagnostic) {
	t.Helper()
	if got := duplicateExecutionWarnings(report); !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate-execution warnings = %#v, want %#v; diagnostics=%#v", got, want, report.Diags)
	}
}

func assertDuplicateExecutionNativeCommands(t *testing.T, repo, home string, target hcc.Target, machine bool, want ...string) {
	t.Helper()
	layout := nativeHookJSONCaseFor(t, target, machine)
	root := layout.root(repo, home, os.Getenv("CODEX_HOME"))
	path := filepath.Join(root, filepath.FromSlash(layout.rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s Native hooks: %v", target, err)
	}
	got := nativeHookJSONCommands(t, data, layout.rel, layout.container, layout.event, layout.shape)
	gotCounts := make(map[string]int, len(got))
	for _, command := range got {
		gotCounts[command]++
	}
	wantCounts := make(map[string]int, len(want))
	for _, command := range want {
		wantCounts[command]++
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("%s Native commands = %v, want %v", target, got, want)
	}
}

// spec: hook-resources/claude-code-and-cursor-selected-together
func TestDuplicateExecutionWarningClaudeCodeAndCursorSelectedTogether(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	const command = "echo claude-cursor-warning"
	provider := duplicateExecutionProvider(t, "claude-cursor", false, command, hcc.TargetClaude, hcc.TargetCursor)
	want := []diag.Diagnostic{{
		Code:     diag.CodeHookDuplicateExecutionRisk,
		Severity: diag.SeverityWarning,
		Target:   "cursor",
		Message:  "Selected harness targets cursor and claude may execute the same hooks in repository scope; cursor also loads claude hook files",
	}}
	report := runDuplicateExecution(t, repo, provider)
	assertDuplicateExecutionWarnings(t, report, want)
	assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetClaude, false, command)
	assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetCursor, false, command)

	t.Run("stale Native output is not selected work", func(t *testing.T) {
		claudeLayout := nativeHookJSONCaseFor(t, hcc.TargetClaude, false)
		claudePath := filepath.Join(claudeLayout.root(repo, home, os.Getenv("CODEX_HOME")), filepath.FromSlash(claudeLayout.rel))
		if _, err := os.Stat(claudePath); err != nil {
			t.Fatalf("initial Claude Native output missing: %v", err)
		}
		cursorOnly := provider
		cursorOnly.Harnesses = []hcc.Target{hcc.TargetCursor}
		report := runDuplicateExecution(t, repo, cursorOnly)
		assertDuplicateExecutionWarnings(t, report, nil)
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetCursor, false, command)
	})

	t.Run("multiple hooks produce one repeatable warning", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		first := duplicateExecutionProvider(t, "first-hook", false, "echo first-hook", hcc.TargetClaude, hcc.TargetCursor)
		second := duplicateExecutionProvider(t, "second-hook", false, "echo second-hook", hcc.TargetClaude, hcc.TargetCursor)
		selected := []resolver.ResolvedArtifact{first, second}
		firstRun := runDuplicateExecution(t, repo, selected...)
		assertDuplicateExecutionWarnings(t, firstRun, want)
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetClaude, false, "echo first-hook", "echo second-hook")
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetCursor, false, "echo first-hook", "echo second-hook")
		secondRun := runDuplicateExecution(t, repo, selected...)
		assertDuplicateExecutionWarnings(t, secondRun, want)
	})
}

// spec: hook-resources/claude-code-and-devin-cli-selected-together
func TestDuplicateExecutionWarningClaudeCodeAndDevinMachineSelectedTogether(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	const command = "echo claude-devin-warning"
	provider := duplicateExecutionProvider(t, "claude-devin", true, command, hcc.TargetClaude, hcc.TargetDevin)
	report := runDuplicateExecution(t, repo, provider)
	want := []diag.Diagnostic{{
		Code:     diag.CodeHookDuplicateExecutionRisk,
		Severity: diag.SeverityWarning,
		Target:   "devin",
		Message:  "Selected harness targets devin and claude may execute the same hooks in machine scope; devin also loads claude hook files",
	}}
	assertDuplicateExecutionWarnings(t, report, want)
	assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetClaude, true, command)
	assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetDevin, true, command)
}

// spec: hook-resources/a-single-target-produces-no-duplicate-warning
func TestSingleTargetProducesNoDuplicateWarning(t *testing.T) {
	t.Run("Cursor output remains present", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		const command = "echo cursor-only"
		provider := duplicateExecutionProvider(t, "cursor-only", false, command, hcc.TargetCursor)
		report := runDuplicateExecution(t, repo, provider)
		assertDuplicateExecutionWarnings(t, report, nil)
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetCursor, false, command)
	})

	t.Run("different scopes are independent", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		claudeRepo := duplicateExecutionProvider(t, "claude-repo", false, "echo claude-repo", hcc.TargetClaude)
		cursorMachine := duplicateExecutionProvider(t, "cursor-machine", true, "echo cursor-machine", hcc.TargetCursor)
		report := runDuplicateExecution(t, repo, claudeRepo, cursorMachine)
		assertDuplicateExecutionWarnings(t, report, nil)
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetClaude, false, "echo claude-repo")
		assertDuplicateExecutionNativeCommands(t, repo, home, hcc.TargetCursor, true, "echo cursor-machine")
	})
}
