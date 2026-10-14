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
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: harness-model-catalogs/model-warnings-keep-materialized-resources-converged
func TestModelPolicyMaterializationKeepsResourcesAndConverges(t *testing.T) {
	for _, tc := range []struct {
		target                                hcc.Target
		dir, suffix, model, effort, wantModel string
	}{
		{hcc.TargetClaude, ".claude/agents", ".md", "sonnet", "high", "model: sonnet"},
		{hcc.TargetFactory, ".factory/droids", ".md", "gpt-5.6-terra", "high", "model: gpt-5.6-terra"},
		{hcc.TargetCursor, ".cursor/agents", ".md", "claude-opus-5", "high", "model: claude-opus-5[effort=high]"},
		{hcc.TargetGemini, ".gemini/agents", ".md", "gemini-3-preview", "", "model: gemini-3-preview"},
		{hcc.TargetDevin, ".devin/agents", ".md", "sonnet", "", "model: sonnet"},
		{hcc.TargetCodex, ".codex/agents", ".toml", "gpt-5.6-terra", "high", "model = 'gpt-5.6-terra'"},
	} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
			t.Run(string(tc.target)+"/"+string(mode), func(t *testing.T) {
				repo, home := devinEnv(t)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				resolved := make([]resolver.ResolvedArtifact, 0, 2)
				for _, spec := range []struct{ name, model, effort string }{{"fallback", "unresolvable-model", ""}, {"known", tc.model, tc.effort}} {
					agent := ir.NewSubagent(spec.name, "Review", "Retain instructions.\n", spec.model, nil, spec.effort, nil, "", nil, ir.SourceRef{})
					resolved = append(resolved, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: spec.name, Kind: ir.KindSubagent, Resource: agent}, Source: config.RepoCommitted, Harnesses: []hcc.Target{tc.target}})
				}
				opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				report, err := Run(context.Background(), repo, resolved, opts...)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				if len(report.Diags) == 0 {
					t.Fatal("missing model omission warning")
				}
				file := filepath.Join(repo, tc.dir, "fallback"+tc.suffix)
				body := readFileString(t, file)
				if !strings.Contains(body, "Retain instructions.") || strings.Contains(body, "unresolvable-model") || strings.Contains(body, "\nmodel:") || strings.Contains(body, "\nmodel =") {
					t.Fatalf("fallback artifact changed semantics: %s", body)
				}
				known := readFileString(t, filepath.Join(repo, tc.dir, "known"+tc.suffix))
				if !strings.Contains(known, tc.wantModel) {
					t.Fatalf("known selection not materialized: %s", known)
				}
				before, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Run(context.Background(), repo, resolved, opts...); err != nil {
					t.Fatal(err)
				}
				after, err := os.Stat(file)
				if err != nil || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("unchanged model output was rewritten: %v", err)
				}
				check, err := Run(context.Background(), repo, resolved, append(opts, WithDryRun())...)
				if err != nil || !check.InSync() {
					t.Fatalf("model warnings caused perpetual drift: %+v %v", check, err)
				}
			})
		}
	}
}
