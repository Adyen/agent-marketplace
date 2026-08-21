package harness

import (
	"fmt"
	"os"
	"path/filepath"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var resolvedLayoutProviders = map[hcc.Target]resolvedLayoutProvider{
	hcc.Target(targetmeta.TargetCodex): resolveCodexDestinations,
}

func resolveCodexDestinations(machineScope bool, repoRoot string) ([]Destination, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	agentsRoot := filepath.Join(repoRoot, ".agents")
	codexRoot := filepath.Join(repoRoot, ".codex")
	agentsID, codexID := "repo-agents", "repo-codex"
	if machineScope {
		agentsRoot = filepath.Join(home, ".agents")
		codexRoot = os.Getenv("CODEX_HOME")
		if codexRoot == "" {
			codexRoot = filepath.Join(home, ".codex")
		} else if !filepath.IsAbs(codexRoot) {
			return nil, fmt.Errorf("CODEX_HOME must be absolute")
		}
		agentsID, codexID = "machine-agents", "machine-codex"
	}
	agentsRoot, err = canonicalRoot(agentsRoot)
	if err != nil {
		return nil, err
	}
	codexRoot, err = canonicalRoot(codexRoot)
	if err != nil {
		return nil, err
	}
	return []Destination{
		{ID: agentsID, Root: agentsRoot, Prefix: ".agents", Info: codexAgentsTargetInfo()},
		{ID: codexID, Root: codexRoot, Prefix: ".codex", Info: codexConfigTargetInfo()},
	}, nil
}

// These values describe Agent Marketplace planning and ownership policy. The
// Codex compiler remains authoritative for the native paths it emits.
func codexAgentsTargetInfo() TargetInfo {
	return TargetInfo{
		Components:       []string{"skills"},
		PlanStrategy:     PlanSubtree,
		SkillPathPattern: "skills/%s/SKILL.md",
		SkillDirKinds:    map[ir.ResourceKind]bool{ir.KindCommand: true},
	}
}

func codexConfigTargetInfo() TargetInfo {
	return TargetInfo{
		Components:   []string{"agents"},
		PlanStrategy: PlanSubtree,
		ResourceDirs: map[ir.ResourceKind]string{ir.KindSubagent: "agents"},
		AggregateFiles: AggregateFileSet{
			Project: []string{"hooks.json", "config.toml"},
			User:    []string{"hooks.json", "config.toml"},
		},
		OwnershipReconciledAggregates: true,
	}
}
