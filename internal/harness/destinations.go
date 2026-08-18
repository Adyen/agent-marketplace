package harness

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Destination is one independently rooted native target layout.
type Destination struct {
	ID     string
	Root   string
	Prefix string
	Info   TargetInfo
}

// RouteArtifact strips the destination's native prefix after validating that
// the artifact remains within the destination root.
func (d Destination) RouteArtifact(path string) (string, error) {
	path = filepath.ToSlash(path)
	if err := ValidateRelPath(path); err != nil {
		return "", err
	}
	if d.Prefix == "" {
		return path, nil
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(d.Prefix), "/") + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("artifact %q is outside destination prefix %q", path, d.Prefix)
	}
	rel := strings.TrimPrefix(path, prefix)
	if err := ValidateRelPath(rel); err != nil || rel == "" {
		return "", fmt.Errorf("artifact %q has invalid destination path", path)
	}
	return rel, nil
}

// ResolveDestinations returns the native output roots for a target and scope.
// Existing targets retain one default destination; Codex resolves its split
// .agents and .codex roots without requiring public target registration.
func ResolveDestinations(target hcc.Target, machineScope bool, repoRoot string) ([]Destination, error) {
	slog.Debug("resolve harness destinations start", "target", target, "machine_scope", machineScope)
	if target != hcc.Target(targetmeta.TargetCodex) {
		info, ok := TargetInfoMap[target]
		if !ok {
			return nil, fmt.Errorf("unknown harness target %q", target)
		}
		root, err := canonicalRoot(info.Roots.For(machineScope, repoRoot))
		if err != nil {
			return nil, err
		}
		return []Destination{{ID: "default", Root: root, Info: info}}, nil
	}

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
	destinations := []Destination{
		{ID: agentsID, Root: agentsRoot, Prefix: ".agents", Info: codexAgentsTargetInfo()},
		{ID: codexID, Root: codexRoot, Prefix: ".codex", Info: codexConfigTargetInfo()},
	}
	slog.Debug("resolve harness destinations complete", "target", target, "machine_scope", machineScope, "count", len(destinations))
	return destinations, nil
}

func canonicalRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty harness destination root")
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve harness destination %q: %w", path, err)
	}
	return filepath.Clean(root), nil
}

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
