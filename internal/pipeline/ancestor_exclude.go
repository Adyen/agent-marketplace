package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// AncestorMaterializedExcludeWarning is emitted when a subdirectory's local
// config excludes a skill that is still materialized in an ancestor root's
// harness output. Agent harnesses discover skills by walking up the directory
// tree, so the ancestor copy remains visible and the subdir exclude has no
// effect. The user must exclude at the providing root instead.
type AncestorMaterializedExcludeWarning struct {
	Kind          ir.ResourceKind
	Name          string
	ProvidingRoot string     // deepest ancestor dir whose harness output still contains the skill
	ConfigToEdit  string     // <ProvidingRoot>/.agents/config.local.yaml
	Harness       hcc.Target // a target where the shadowing materialization was found
}

func (w *AncestorMaterializedExcludeWarning) Error() string {
	return fmt.Sprintf(
		"exclude of %s %q has no effect from this directory: it is still materialized at the ancestor root %s, which agent harnesses discover by walking up the directory tree",
		skill.KindLabel(w.Kind), w.Name, w.ProvidingRoot)
}

// Remediation satisfies resolver.Remediable so the CLI prints an actionable hint.
func (w *AncestorMaterializedExcludeWarning) Remediation() string {
	return fmt.Sprintf(
		"add the exclude to %s and run 'agent-marketplace sync' from %s; note this removes %q for the entire %s checkout, not just this subdirectory",
		w.ConfigToEdit, w.ProvidingRoot, w.Name, w.ProvidingRoot)
}

// DedupKey lets the sync command collapse duplicate warnings.
func (w *AncestorMaterializedExcludeWarning) DedupKey() string {
	return fmt.Sprintf("ancestor-exclude:%v:%s:%s", w.Kind, w.Name, w.ProvidingRoot)
}

// detectAncestorMaterializedExcludes reports excludes authored in a
// subdirectory's local layer that cannot take effect because the excluded
// skill is still materialized in an ancestor root above matRoot.
//
// Scoped to skills (directory-based, reliably detectable on disk); non-skill
// kinds (mcp/hook merged into shared JSON files) are deferred.
func detectAncestorMaterializedExcludes(
	matRoot, repoRoot string,
	targets []hcc.Target,
	localExcludes []resourceName,
	resolved []resolver.ResolvedArtifact,
) []error {
	if matRoot == "" || repoRoot == "" {
		return nil
	}
	if filepath.Clean(matRoot) == filepath.Clean(repoRoot) {
		return nil
	}

	resolvedSet := make(map[resourceName]struct{}, len(resolved))
	for _, r := range resolved {
		resolvedSet[resourceName{kind: r.Kind, name: strings.TrimSpace(r.Name)}] = struct{}{}
	}

	ancestors := ancestorRoots(matRoot, repoRoot)
	if len(ancestors) == 0 {
		return nil
	}

	var warns []error
	for _, ex := range localExcludes {
		if ex.kind != ir.KindSkill {
			continue
		}
		if _, present := resolvedSet[ex]; present {
			continue
		}
		// Walk ancestors deepest-first; report the closest ancestor that
		// shadows the exclude.
		for _, ancestor := range ancestors {
			if target, ok := skillMaterializedAt(ancestor, ex.name, targets); ok {
				warns = append(warns, &AncestorMaterializedExcludeWarning{
					Kind:          ex.kind,
					Name:          ex.name,
					ProvidingRoot: ancestor,
					ConfigToEdit:  filepath.Join(ancestor, ".agents", "config.local.yaml"),
					Harness:       target,
				})
				break
			}
		}
	}
	return warns
}

// AncestorMaterializedExcludeWarnings reports excludes in the local layer of a
// subdirectory that cannot take effect because the excluded skill is still
// materialized in an ancestor root. Exposed for callers (e.g. sync --check)
// that drive resolution directly instead of via Sync.
func AncestorMaterializedExcludeWarnings(matRoot, repoRoot string, res ResolveResult) []error {
	targets := res.TargetHarnesses
	return detectAncestorMaterializedExcludes(
		matRoot,
		repoRoot,
		targets,
		skillExcludesFromLocal(res.LocalCfg),
		res.Resolved,
	)
}

// ancestorRoots returns the directories from the parent of matRoot up to and
// including repoRoot, ordered deepest-first. Returns nil if repoRoot is not an
// ancestor of matRoot.
func ancestorRoots(matRoot, repoRoot string) []string {
	matRoot = filepath.Clean(matRoot)
	repoRoot = filepath.Clean(repoRoot)
	var out []string
	for dir := filepath.Dir(matRoot); ; dir = filepath.Dir(dir) {
		out = append(out, dir)
		if dir == repoRoot {
			return out
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without hitting repoRoot.
			return nil
		}
	}
}

// skillMaterializedAt reports whether a skill directory named name exists in
// any of the given targets' harness output under root.
func skillMaterializedAt(root, name string, targets []hcc.Target) (hcc.Target, bool) {
	for _, target := range targets {
		dir := skillDir(target, root, name)
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return target, true
		}
	}
	return "", false
}

// skillDir returns the on-disk directory where a skill named name would be
// materialized for target under root (repo scope). Returns "" if the target is
// unknown.
func skillDir(target hcc.Target, root, name string) string {
	roots, ok := harness.InstallRoots[target]
	if !ok {
		if target != hcc.TargetCodex {
			return ""
		}
		destinations, err := harness.ResolveDestinations(target, false, root)
		if err != nil {
			return ""
		}
		for _, destination := range destinations {
			if destination.Info.SkillPathPattern == "" {
				continue
			}
			skillRel := filepath.Dir(fmt.Sprintf(destination.Info.SkillPathPattern, name))
			return filepath.Join(destination.Root, skillRel)
		}
		return ""
	}
	installRoot := roots.For(false, root)
	// The native-layout descriptor is the topology authority for targets that
	// declare one — Claude deliberately leaves TargetInfo.SkillPathPattern
	// unset, so the descriptor is where its skill shape lives. The literal
	// fallback only serves targets with no registered layout.
	pattern := "skills/%s/SKILL.md"
	if declared, ok := targetmeta.NativeResourcePattern(target, ir.KindSkill); ok {
		pattern = declared
	} else if info, ok := harness.TargetInfoFor(target); ok && info.SkillPathPattern != "" {
		pattern = info.SkillPathPattern
	}
	skillRel := filepath.Dir(fmt.Sprintf(pattern, name))
	return filepath.Join(installRoot, skillRel)
}

// resourceName is a (kind, name) pair used for exclude/resolved comparisons.
type resourceName struct {
	kind ir.ResourceKind
	name string
}

// skillExcludesFromLocal collects skill-kind excludes authored in the local
// (subdir) config layer, both top-level and per-registry-entry.
func skillExcludesFromLocal(localCfg *config.LocalConfig) []resourceName {
	if localCfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []resourceName
	add := func(names []string) {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, resourceName{kind: ir.KindSkill, name: name})
		}
	}
	add(localCfg.SelectorForKind(ir.KindSkill).Exclude)
	for _, entry := range localCfg.Registries {
		add(entry.Skills.Exclude)
	}
	return out
}
