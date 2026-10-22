package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func geminiSelection(resource ir.Resource, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: resource.Name(), Kind: resource.Kind(), Resource: resource},
		Source:    source,
		Harnesses: []hcc.Target{hcc.TargetGemini},
	}
}

// spec: materialization/deselected-gemini-subagents-are-removed
func TestGeminiSubagentIsRemovedWhenNoLongerSelected(t *testing.T) {
	const handwritten = "---\nname: handwritten\ndescription: written by hand\n---\n\nKeep me.\n"
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, machine := range []bool{false, true} {
			for _, skillRemains := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/machine=%t/skill-remains=%t", mode, machine, skillRemains), func(t *testing.T) {
					home, repo := isolateSelectionEnvironment(t)
					root, source := filepath.Join(repo, ".gemini"), config.RepoCommitted
					if machine {
						root, source = filepath.Join(home, ".gemini"), config.UserPersonal
					}
					agent := geminiSelection(ir.NewSubagent("helper", "Helps", "Instructions.\n", "", nil, "", nil, "", nil, ir.SourceRef{}), source)
					skill := geminiSelection(ir.NewSkill("keep", "Kept", "Body\n", nil, nil, ir.SourceRef{}), source)
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}

					handwrittenPath := filepath.Join(root, "agents", "handwritten.md")
					if err := os.MkdirAll(filepath.Dir(handwrittenPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(handwrittenPath, []byte(handwritten), 0o644); err != nil {
						t.Fatal(err)
					}

					installedAgent := filepath.Join(root, "agents", "helper.md")
					installedSkill := filepath.Join(root, "skills", "keep")
					remaining := []resolver.ResolvedArtifact(nil)
					if skillRemains {
						remaining = []resolver.ResolvedArtifact{skill}
					}

					report, err := Run(context.Background(), repo, append([]resolver.ResolvedArtifact{skill}, agent), opts...)
					if err != nil {
						t.Fatal(err)
					}
					requireNoErrorDiags(t, report.Diags)
					for _, installed := range []string{installedAgent, installedSkill} {
						if _, err := os.Lstat(installed); err != nil {
							t.Fatalf("setup: %s was not materialized: %v", installed, err)
						}
					}

					report, err = Run(context.Background(), repo, remaining, opts...)
					if err != nil {
						t.Fatal(err)
					}
					requireNoErrorDiags(t, report.Diags)
					if _, err := os.Lstat(installedAgent); !os.IsNotExist(err) {
						t.Errorf("deselected subagent survived the sync (stat = %v)", err)
					}
					if got := readFileString(t, handwrittenPath); got != handwritten {
						t.Errorf("handwritten subagent changed:\n%s", got)
					}
					_, err = os.Lstat(installedSkill)
					if skillRemains && err != nil {
						t.Errorf("selected skill was pruned with the subagent: %v", err)
					}
					if !skillRemains && !os.IsNotExist(err) {
						t.Errorf("deselected skill survived the sync (stat = %v)", err)
					}

					check, err := Run(context.Background(), repo, remaining, append(opts, WithDryRun())...)
					if err != nil || !check.InSync() {
						t.Errorf("check after the removal reports drift: %+v %v", check, err)
					}
				})
			}
		}
	}
}
