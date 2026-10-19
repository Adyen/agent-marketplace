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

func requireCodexOverlapRefusal(t *testing.T, repo string, roots ...string) {
	t.Helper()
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		codexSubagent("reviewer", "Repository definition.", config.RepoCommitted),
		codexSubagent("reviewer", "Machine definition.", config.UserPersonal),
	})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap)
	if !ok {
		t.Fatalf("missing %s diagnostic: %+v", diag.CodeMaterializeDestinationOverlap, report.Diags)
	}
	for _, root := range roots {
		if !strings.Contains(d.Message, root) {
			t.Fatalf("overlap diagnostic must name %q: %+v", root, d)
		}
		entries, err := os.ReadDir(root)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("refused preflight mutated %q: %v", root, entries)
		}
	}
}

// spec: materialization/codex-partitions-that-differ-only-in-unicode-normalization-are-refused
func TestRunRefusesNormalizationAliasedCodexHomeBeforeMutation(t *testing.T) {
	for _, existing := range []bool{true, false} {
		name := "first install"
		if existing {
			name = "existing root"
		}
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			repo := filepath.Join(parent, "caf\u00e9")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := os.Mkdir(filepath.Join(repo, ".codex"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(parent, "cafe\u0301", ".codex")
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", alias)
			requireCodexOverlapRefusal(t, repo, filepath.Join(repo, ".codex"), alias)
		})
	}
}

func TestRunRefusesSameDirectoryCodexHomeSpellingBeforeMutation(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join("/System/Volumes/Data", repo, ".codex")
	if resolved, err := filepath.EvalSymlinks(alias); err != nil || resolved != alias {
		t.Skipf("no firmlinked spelling of the repository on this host: %q, %v", resolved, err)
	}
	if aliasInfo, err := os.Stat(alias); err != nil {
		t.Skipf("no firmlinked spelling of the repository on this host: %v", err)
	} else if repoInfo, err := os.Stat(filepath.Join(repo, ".codex")); err != nil || !os.SameFile(aliasInfo, repoInfo) {
		t.Skipf("firmlinked spelling names another directory: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", alias)
	requireCodexOverlapRefusal(t, repo, filepath.Join(repo, ".codex"), alias)
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
