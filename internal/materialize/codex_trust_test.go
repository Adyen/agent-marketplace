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

// spec: hook-resources/codex-repository-hook-change-reports-trust-review
func TestCodexTrustReviewDiagnosticsFollowRepositoryMutation(t *testing.T) {
	tests := []struct {
		name       string
		outputPath string
		resource   func(string) ir.Resource
	}{
		{
			name:       "hooks",
			outputPath: filepath.Join(".codex", "hooks.json"),
			resource: func(command string) ir.Resource {
				return ir.NewHook("managed", "PreToolUse", command, "command", nil, ir.SourceRef{})
			},
		},
		{
			name:       "MCP",
			outputPath: filepath.Join(".codex", "config.toml"),
			resource: func(command string) ir.Resource {
				return ir.NewMcpServer("managed", command, nil, nil, nil, ir.SourceRef{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			gitignore := filepath.Join(repo, ".gitignore")
			trustPolicy := filepath.Join(repo, ".codex", "trust-policy.toml")
			if err := os.MkdirAll(filepath.Dir(trustPolicy), 0o755); err != nil {
				t.Fatal(err)
			}
			for path, body := range map[string]string{gitignore: "user-policy\n", trustPolicy: "trusted = false\n"} {
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			selection := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "managed", Resource: tt.resource("first")},
				Source:    config.RepoCommitted,
				Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
			}}
			report, err := Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			assertCodexTrustDiagnostic(t, report, diag.CodeMaterializeCodexTrustReview, "may require trust review")

			report, err = Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			if hasDiagCode(report.Diags, diag.CodeMaterializeCodexTrustReview) || hasDiagCode(report.Diags, diag.CodeMaterializeCodexTrustReviewPending) {
				t.Fatalf("no-op sync reported trust review: %#v", report.Diags)
			}

			output := filepath.Join(repo, tt.outputPath)
			before, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			selection[0].Resource = tt.resource("second")
			report, err = Run(context.Background(), repo, selection, WithDryRun())
			if err != nil {
				t.Fatal(err)
			}
			assertCodexTrustDiagnostic(t, report, diag.CodeMaterializeCodexTrustReviewPending, "did not modify")
			after, err := os.ReadFile(output)
			if err != nil || string(after) != string(before) {
				t.Fatalf("--check changed %s: err=%v\nbefore=%s\nafter=%s", output, err, before, after)
			}

			selection[0].Source = config.UserPersonal
			report, err = Run(context.Background(), repo, selection, WithMachineScopeOnly())
			if err != nil {
				t.Fatal(err)
			}
			if hasDiagCode(report.Diags, diag.CodeMaterializeCodexTrustReview) || hasDiagCode(report.Diags, diag.CodeMaterializeCodexTrustReviewPending) {
				t.Fatalf("machine sync reported repository trust review: %#v", report.Diags)
			}

			for path, want := range map[string]string{gitignore: "user-policy\n", trustPolicy: "trusted = false\n"} {
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != want {
					t.Errorf("policy file %s changed: got %q, err=%v", path, got, readErr)
				}
			}
		})
	}
}

func assertCodexTrustDiagnostic(t *testing.T, report *Report, code, messagePart string) {
	t.Helper()
	d, ok := findDiag(report.Diags, code)
	if !ok {
		t.Fatalf("missing %s diagnostic: %#v", code, report.Diags)
	}
	if !strings.Contains(d.Message, messagePart) || d.Hint == "" {
		t.Fatalf("trust diagnostic is not actionable: %+v", d)
	}
}
