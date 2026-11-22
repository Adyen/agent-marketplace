package materialize

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

var lookupApplicationTarget = func(target hcc.Target) (harness.ApplicationTarget, bool, error) {
	registry, err := harness.NewInstallationAdapterRegistry()
	if err != nil {
		return harness.ApplicationTarget{}, false, err
	}
	appTarget, ok := registry.Lookup(string(target))
	return appTarget, ok, nil
}

func preflightPluginInstances(work []skillTarget) (map[string]bool, []diag.Diagnostic) {
	groups := make(map[string][]skillTarget)
	var identities []string
	for _, item := range work {
		if item.skill.PluginPackage == nil {
			continue
		}
		identity := pluginInstanceIdentity(item.skill, item.target)
		if _, ok := groups[identity]; !ok {
			identities = append(identities, identity)
		}
		groups[identity] = append(groups[identity], item)
	}
	blocked := make(map[string]bool)
	var diagnostics []diag.Diagnostic
	for _, identity := range identities {
		first := groups[identity][0]
		if err := requireAcceptedDelivery(first.target, resourceDeliveryKind(first)); err != nil {
			blocked[identity] = true
			diagnostics = append(diagnostics, diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Target:   string(first.target),
				Resource: "plugin/" + first.skill.PluginPackage.Name,
				Message:  err.Error(),
				Source:   diag.SourceRef{File: first.skill.PluginPackage.PackageRoot},
			})
			continue
		}
		if first.skill.PluginPackage.Delivery == "native" {
			continue
		}
		for _, item := range groups[identity] {
			if len(item.skill.PluginPackage.DecodeFailures) > 0 {
				blocked[identity] = true
				failingMember := item.skill.PluginPackage.DecodeFailures[0]
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(item.target),
					Resource: "plugin/" + item.skill.PluginPackage.Name + "/" + failingMember,
					Message:  fmt.Sprintf("plugin member %s failed to decode; the instance was retained", failingMember),
					Source:   diag.SourceRef{File: item.skill.PluginPackage.PackageRoot},
				})
				break
			}
			if item.skill.PluginPackage.GenerationError != "" {
				blocked[identity] = true
				break
			}
			resource, options, err := pluginMemberCompileInput(item)
			if err != nil {
				blocked[identity] = true
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(item.target),
					Resource: pluginMemberLabel(item.skill),
					Message:  fmt.Sprintf("compile plugin member for target %s: %v", item.target, err),
					Source:   diag.SourceRef{File: item.skill.Path},
				})
				break
			}
			if item.machineScope {
				options = append(options, hcc.WithScope(hcc.ScopeUser))
			}
			result, compileErr := hcc.Compile(resource, item.target, options...)
			if compileErr != nil {
				blocked[identity] = true
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(item.target),
					Resource: pluginMemberLabel(item.skill),
					Message:  fmt.Sprintf("compile plugin member for target %s: %v", item.target, compileErr),
					Source:   diag.SourceRef{File: item.skill.Path},
				})
				break
			}
			if result == nil {
				blocked[identity] = true
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(item.target),
					Resource: pluginMemberLabel(item.skill),
					Message:  "plugin member compiler returned no result",
					Source:   diag.SourceRef{File: item.skill.Path},
				})
				break
			}
			var failures []diag.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Severity == diag.SeverityError {
					if diagnostic.Target == "" {
						diagnostic.Target = string(item.target)
					}
					if diagnostic.Resource == "" {
						diagnostic.Resource = pluginMemberLabel(item.skill)
					}
					failures = append(failures, diagnostic)
				}
			}
			if len(failures) > 0 {
				blocked[identity] = true
				diagnostics = append(diagnostics, failures...)
				break
			}
		}
	}
	return blocked, diagnostics
}

func resourceDeliveryKind(item skillTarget) ir.ResourceKind {
	if item.skill.PluginPackage != nil {
		return ir.KindPlugin
	}
	return item.skill.Resource.Kind()
}

func requireAcceptedDelivery(target hcc.Target, kind ir.ResourceKind) error {
	if kind == ir.KindRule {
		return nil
	}
	appTarget, ok, err := lookupApplicationTarget(target)
	if err != nil {
		return fmt.Errorf("harness: cannot validate target %q resource kind %q: %w", target, kind, err)
	}
	if !ok {
		return fmt.Errorf("harness: target %q has no application declaration for resource kind %q", target, kind)
	}
	return appTarget.RequireAccepted(kind)
}

func pluginMemberCompileInput(item skillTarget) (ir.Resource, []hcc.Option, error) {
	resource := item.skill.Resource
	if resource == nil {
		return nil, nil, fmt.Errorf("plugin member has no resource")
	}
	if item.skill.InstalledName != "" {
		renamed, err := marketplaceResourceForInstallName(resource, item.skill.InstalledName)
		if err != nil {
			return nil, nil, err
		}
		resource = renamed
	}
	var options []hcc.Option
	if item.skill.PluginPackage != nil {
		pkg := item.skill.PluginPackage
		if resource.Kind() == ir.KindHook {
			options = append(options,
				hcc.WithPluginRoot(pkg.GenerationPath),
				hcc.WithNativeHookGenerationRoot(pkg.GenerationPath),
				hcc.WithNativeHookOutput(),
			)
		} else {
			resource = pluginpolicy.ExpandMemberRuntime(resource, pluginpolicy.Roots{Package: pkg.GenerationPath, Data: pkg.DataPath})
			options = append(options, hcc.WithPluginRoot(pkg.GenerationPath))
		}
	}
	return resource, options, nil
}

func pluginMemberLabel(resource resolver.ResolvedArtifact) string {
	if resource.InstalledName != "" {
		return resource.InstalledName
	}
	return resource.Name
}

func attachPluginInstanceOwner(entries []ownedJSONEntry, pkg *resolver.PluginPackage) {
	if pkg == nil || pkg.InstanceKey == "" {
		return
	}
	for i := range entries {
		entries[i].pluginInstanceKey = pkg.InstanceKey
	}
}
