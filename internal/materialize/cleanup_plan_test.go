package materialize

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func planMCPSelection(names ...string) []resolver.ResolvedArtifact {
	selection := devinMcpSelection(names...)
	for i := range selection {
		selection[i].Kind = ir.KindMcpServer
	}
	return selection
}

func TestOwnedMCPRemovalPlanCommitsOnlyTarget(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	all := planMCPSelection("remove", "keep")
	report, err := Run(context.Background(), repo, all)
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed=%#v error=%v", report, err)
	}
	path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(raw, []byte("keep-binary"), []byte("keep-edited"), 1)
	edited = bytes.Replace(edited, []byte("{\"keep\""), []byte("{\"handwritten\":{\"command\":\"manual\"},\"keep\""), 1)
	if bytes.Equal(raw, edited) {
		t.Fatal("fixture did not edit sibling")
	}
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	result, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) {
		called++
		return dependencytxn.Result{Changed: []string{"config"}}, nil
	})
	if err != nil || called != 1 || len(result.Changed) != 1 {
		t.Fatalf("called=%d result=%#v error=%v", called, result, err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || string(raw) == "" || bytes.Contains(raw, []byte("remove-binary")) || !bytes.Contains(raw, []byte("keep-edited")) || !bytes.Contains(raw, []byte("handwritten")) {
		t.Fatalf("aggregate=%q error=%v", raw, err)
	}
}

func TestOwnedMCPRemovalPlanLastSelected(t *testing.T) {
	repo, _ := devinEnv(t)
	all := planMCPSelection("remove")
	report, err := Run(context.Background(), repo, all)
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed=%#v error=%v", report, err)
	}
	plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(devinProjectRoot(repo), "mcp_config.json"))
	if err != nil || bytes.Contains(raw, []byte("remove-binary")) {
		t.Fatalf("aggregate=%q error=%v", raw, err)
	}
}

func TestOwnedMCPRemovalPlanCodex(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	all := cleanupMCPSelection(hcc.TargetCodex, "remove", "keep")
	report, err := Run(context.Background(), repo, all)
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed=%#v error=%v", report, err)
	}
	plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".codex", codexMCPFile))
	if err != nil || bytes.Contains(raw, []byte("remove-binary")) || !bytes.Contains(raw, []byte("keep-binary")) {
		t.Fatalf("aggregate=%q error=%v", raw, err)
	}
}

func TestOwnedMCPRemovalPlanCodexTrustDiagnostic(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repo", true: "machine"}[machine], func(t *testing.T) {
			repo, home := devinEnv(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			var all []resolver.ResolvedArtifact
			if machine {
				all = cleanupMCPSelection(hcc.TargetCodex, "remove", "keep")
			} else {
				all = planMCPSelection("remove", "keep")
				for i := range all {
					all[i].Harnesses = []hcc.Target{hcc.TargetCodex}
				}
			}
			report, err := Run(context.Background(), repo, all)
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed=%#v error=%v", report, err)
			}
			plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, nil }); err != nil {
				t.Fatal(err)
			}
			diagnostics := plan.Diagnostics()
			if machine {
				if len(diagnostics) != 0 {
					t.Fatalf("machine diagnostics=%#v", diagnostics)
				}
			} else if len(diagnostics) != 1 || diagnostics[0].Code != diag.CodeMaterializeCodexTrustReview {
				t.Fatalf("repo diagnostics=%#v", diagnostics)
			}
		})
	}
	t.Run("publish failure", func(t *testing.T) {
		repo, _ := devinEnv(t)
		all := planMCPSelection("remove", "keep")
		for i := range all {
			all[i].Harnesses = []hcc.Target{hcc.TargetCodex}
		}
		if _, err := Run(context.Background(), repo, all); err != nil {
			t.Fatal(err)
		}
		plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
		if err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("publish")
		if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, sentinel }); !errors.Is(err, sentinel) {
			t.Fatalf("error=%v", err)
		}
		if diagnostics := plan.Diagnostics(); len(diagnostics) != 0 {
			t.Fatalf("failure diagnostics=%#v", diagnostics)
		}
	})
}

func TestOwnedMCPRemovalPlanPreflightAndPublishFailure(t *testing.T) {
	t.Run("same key remains", func(t *testing.T) {
		repo, _ := devinEnv(t)
		all := planMCPSelection("remove", "keep")
		if _, err := Run(context.Background(), repo, all); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("publish failure", func(t *testing.T) {
		repo, _ := devinEnv(t)
		all := planMCPSelection("remove", "keep")
		if _, err := Run(context.Background(), repo, all); err != nil {
			t.Fatal(err)
		}
		plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(devinProjectRoot(repo), "mcp_config.json"))
		if err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("publish failed")
		if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, sentinel }); !errors.Is(err, sentinel) {
			t.Fatalf("error=%v", err)
		}
		after, err := os.ReadFile(filepath.Join(devinProjectRoot(repo), "mcp_config.json"))
		if err != nil || string(before) != string(after) {
			t.Fatalf("aggregate changed: %v", err)
		}
	})
}

func TestOwnedMCPRemovalPlanRejectsRequiredAndUnsupportedTargets(t *testing.T) {
	repo, _ := devinEnv(t)
	target := planMCPSelection("remove")[0]
	target.Required = true
	if _, err := PrepareOwnedMCPRemoval(context.Background(), repo, target, nil); !errors.Is(err, ErrOwnedCleanupUnsupported) {
		t.Fatalf("required target error=%v", err)
	}
	target.Required = false
	target.Harnesses = []hcc.Target{hcc.TargetDevin, hcc.TargetFactory}
	if _, err := PrepareOwnedMCPRemoval(context.Background(), repo, target, nil); !errors.Is(err, ErrOwnedCleanupUnsupported) {
		t.Fatalf("mixed target error=%v", err)
	}
}

func TestOwnedMCPRemovalPlanMissingProofPreventsPublish(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	all := planMCPSelection("remove", "keep")
	for i := range all {
		all[i].Harnesses = []hcc.Target{hcc.TargetDevin, hcc.TargetCodex}
	}
	report, err := Run(context.Background(), repo, all)
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v error=%v", report, err)
	}
	plan, err := PrepareOwnedMCPRemoval(context.Background(), repo, all[0], all[1:])
	if err != nil {
		t.Fatal(err)
	}
	devinPath := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	codexPath := filepath.Join(repo, ".codex", codexMCPFile)
	devinBefore, err := os.ReadFile(devinPath)
	if err != nil {
		t.Fatal(err)
	}
	codexBefore, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repo, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	called := 0
	_, err = plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) {
		called++
		return dependencytxn.Result{}, nil
	})
	devinAfter, devinErr := os.ReadFile(devinPath)
	codexAfter, codexErr := os.ReadFile(codexPath)
	if err == nil || called != 0 || devinErr != nil || codexErr != nil || !bytes.Equal(devinBefore, devinAfter) || !bytes.Equal(codexBefore, codexAfter) {
		t.Fatalf("called=%d error=%v devinErr=%v codexErr=%v", called, err, devinErr, codexErr)
	}
}
