package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: materialization/codex-partitions-that-differ-only-in-letter-case-are-refused
func TestRunRefusesCaseAliasedCodexPartitionsBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection []resolver.ResolvedArtifact
	}{
		{name: "conflicting", selection: []resolver.ResolvedArtifact{
			codexSubagent("reviewer", "Repository definition.", config.RepoCommitted),
			codexSubagent("reviewer", "Machine definition.", config.UserPersonal),
		}},
		{name: "disjoint", selection: []resolver.ResolvedArtifact{
			codexSubagent("repo-reviewer", "Review repository changes.", config.RepoCommitted),
			codexSubagent("machine-reviewer", "Review machine changes.", config.UserPersonal),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if err := os.Mkdir(filepath.Join(repo, ".codex"), 0o755); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(repo, ".CODEX")
			t.Setenv("CODEX_HOME", alias)

			report, err := Run(context.Background(), repo, tc.selection)
			if err != nil {
				t.Fatal(err)
			}
			d, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap)
			if !ok {
				t.Fatalf("missing %s diagnostic: %+v", diag.CodeMaterializeDestinationOverlap, report.Diags)
			}
			if !strings.Contains(d.Message, filepath.Join(repo, ".codex")) || !strings.Contains(d.Message, alias) || !strings.Contains(d.Hint, "letter case") {
				t.Fatalf("overlap diagnostic must name both spellings and the remedy: %+v", d)
			}
			for _, root := range []string{filepath.Join(repo, ".codex"), alias} {
				entries, err := os.ReadDir(root)
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatalf("refused preflight mutated %q: %v", root, entries)
				}
			}
		})
	}
}

func TestRunRefusesNormalizationAliasedCodexHomeBeforeMutation(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "caf\u00e9")
	if err := os.MkdirAll(filepath.Join(repo, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "cafe\u0301", ".codex")
	if _, err := os.Stat(alias); err != nil {
		t.Skipf("filesystem distinguishes Unicode normalization forms: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", alias)

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		codexSubagent("reviewer", "Repository definition.", config.RepoCommitted),
		codexSubagent("reviewer", "Machine definition.", config.UserPersonal),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing %s diagnostic: %+v", diag.CodeMaterializeDestinationOverlap, report.Diags)
	}
	if entries, err := os.ReadDir(filepath.Join(repo, ".codex")); err != nil || len(entries) != 0 {
		t.Fatalf("refused preflight mutated the aliased root: %v, %v", entries, err)
	}
}

// spec: materialization/disjoint-codex-partitions-that-differ-only-in-letter-case-remain-usable
func TestRunAllowsDisjointCaseAliasedCodexPartitions(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(home, ".AGENTS"))

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		codexSkill(t, "demo", config.UserPersonal),
		codexSubagent("reviewer", "Review carefully.", config.UserPersonal),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
	for _, path := range []string{
		filepath.Join(home, ".agents", "skills", "demo"),
		filepath.Join(home, ".AGENTS", "agents", "reviewer.toml"),
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("disjoint case-aliased output %q missing: %v", path, err)
		}
	}
}
