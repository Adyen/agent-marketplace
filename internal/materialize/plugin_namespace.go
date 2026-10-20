package materialize

import (
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var agentSkillInstalledName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func pluginGenerationScope(resource resolver.ResolvedArtifact, repoRoot string) string {
	if resource.Source.IsMachineScope() {
		return "machine"
	}
	canonical, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		canonical = repoRoot
	}
	return "repo:" + ownership.RootKey(canonical)
}

func pluginInstanceIdentity(resource resolver.ResolvedArtifact, target hcc.Target) string {
	if resource.PluginPackage == nil {
		return ""
	}
	return strings.Join([]string{
		resource.PluginPackage.Name,
		resource.PluginPackage.PackageRoot,
		resource.PluginPackage.Scope,
		string(target),
	}, "\x00")
}

func pluginNamespaceRefusals(resolved []resolver.ResolvedArtifact) (map[string]bool, []diag.Diagnostic) {
	type occupant struct {
		resource resolver.ResolvedArtifact
		target   hcc.Target
		name     string
	}
	groups := map[string][]occupant{}
	blocked := map[string]bool{}
	var diagnostics []diag.Diagnostic
	reported := map[string]bool{}
	for _, resource := range resolver.MaterializationWinners(resolved) {
		if resource.PluginPackage == nil || resource.Resource == nil || resource.Resource.Kind() == ir.KindPlugin {
			continue
		}
		targets := resolver.HarnessTargets(resource.Harnesses)
		name := resource.InstalledName
		if name == "" {
			name = resource.Name
		}
		for _, target := range targets {
			identity := pluginInstanceIdentity(resource, target)
			if !validPluginInstalledName(target, resource.Resource.Kind(), name) {
				blocked[identity] = true
				if !reported[identity] {
					diagnostics = append(diagnostics, diag.Diagnostic{
						Code:     diag.CodePluginNamespaceInvalid,
						Severity: diag.SeverityError,
						Target:   string(target),
						Resource: "plugin/" + resource.PluginPackage.Name,
						Message:  "plugin member installed name is not valid for this target",
						Source:   diag.SourceRef{File: resource.Path},
					})
					reported[identity] = true
				}
				continue
			}
			key := strings.Join([]string{string(target), boolToken(resource.Source.IsMachineScope()), name}, "\x00")
			groups[key] = append(groups[key], occupant{resource: resource, target: target, name: name})
		}
	}
	for _, resource := range resolver.MaterializationWinners(resolved) {
		if resource.PluginPackage != nil || resource.Resource == nil {
			continue
		}
		targets := resolver.HarnessTargets(resource.Harnesses)
		name := resource.InstalledName
		if name == "" {
			name = resource.Name
		}
		for _, target := range targets {
			key := strings.Join([]string{string(target), boolToken(resource.Source.IsMachineScope()), name}, "\x00")
			groups[key] = append(groups[key], occupant{resource: resource, target: target, name: name})
		}
	}
	for _, occupants := range groups {
		if len(occupants) < 2 {
			continue
		}
		for _, occupant := range occupants {
			pkg := occupant.resource.PluginPackage
			if pkg == nil {
				continue
			}
			identity := pluginInstanceIdentity(occupant.resource, occupant.target)
			blocked[identity] = true
			if reported[identity] {
				continue
			}
			diagnostics = append(diagnostics, diag.Diagnostic{
				Code:     diag.CodePluginNamespaceCollision,
				Severity: diag.SeverityError,
				Target:   string(occupant.target),
				Resource: "plugin/" + pkg.Name,
				Message:  "plugin member installed name collides with another selected resource in this target and scope",
				Source:   diag.SourceRef{File: occupant.resource.Path},
			})
			reported[identity] = true
		}
	}
	return blocked, diagnostics
}

func validPluginInstalledName(target hcc.Target, kind ir.ResourceKind, name string) bool {
	if usesSkillDir(target, kind) {
		return len(name) <= 64 && agentSkillInstalledName.MatchString(name)
	}
	return ir.ValidateName(name) == nil
}

func boolToken(value bool) string {
	if value {
		return "machine"
	}
	return "repository"
}
