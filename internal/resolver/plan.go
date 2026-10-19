package resolver

import (
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// PlanPluginSelection runs the resolver's own selection over the given
// configuration and catalogs and projects the resolved KindPlugin artifacts
// into the plan every plugin-v1 manifest derives from. Equality with Resolve
// holds by construction: the plan is the projected output of the same
// selection stages.
func PlanPluginSelection(cfg *config.Config, catalogs map[string][]catalog.Entry, localCfg *config.LocalConfig, opts ...ResolveOption) (catalog.PluginSelectionPlan, []error) {
	resolved, errs := Resolve(cfg, catalogs, localCfg, opts...)
	plan := catalog.PluginSelectionPlan{}
	for _, artifact := range resolved {
		if artifact.Kind != ir.KindPlugin || artifact.Selection == nil || artifact.Plugin == nil {
			continue
		}
		owner, found := pluginOwnerEntry(cfg, artifact)
		if !found {
			continue
		}
		planned := catalog.PlannedPlugin{Source: owner, Name: artifact.Name, Entry: artifact.Entry}
		for _, edge := range artifact.Selection.Edges {
			planned.Edges = append(planned.Edges, catalog.PluginSelectionEdge{Type: edge.Kind, Collection: edge.Collection, Scope: edge.Source})
		}
		members := make([]string, 0, len(artifact.Selection.Members))
		for _, member := range artifact.Selection.Members {
			members = append(members, member.Key())
		}
		sort.Strings(members)
		planned.Members = members
		planned.Exclusions = append(planned.Exclusions, artifact.Selection.Exclusions...)
		plan.Plugins = append(plan.Plugins, planned)
	}
	sort.Slice(plan.Plugins, func(i, j int) bool {
		if plan.Plugins[i].Name != plan.Plugins[j].Name {
			return plan.Plugins[i].Name < plan.Plugins[j].Name
		}
		return plan.Plugins[i].Source.URL < plan.Plugins[j].Source.URL
	})
	return plan, errs
}

// pluginOwnerEntry finds the configured registry entry the resolved plugin
// artifact was bound to: same normalized URL, ref, and registry path, and,
// when several layers repeat the location, the same install scope.
func pluginOwnerEntry(cfg *config.Config, artifact ResolvedArtifact) (config.RegistryEntry, bool) {
	url := normalizedRegistryURL(artifact.RegistryURL)
	path := normalizedEntryPath(artifact.RegistryPath)
	ref := strings.TrimSpace(artifact.Ref)
	matches := func(entry config.RegistryEntry) bool {
		if normalizedRegistryURL(entry.URL) != url {
			return false
		}
		if ref != "" && strings.TrimSpace(entry.Ref) != ref {
			return false
		}
		return normalizedEntryPath(entry.Path) == path
	}
	var fallback config.RegistryEntry
	fallbackFound := false
	for _, entry := range cfg.Registries {
		if !matches(entry) {
			continue
		}
		if artifact.CatalogKey != "" && entry.CatalogKey != "" && entry.CatalogKey != artifact.CatalogKey {
			continue
		}
		if entry.Source == artifact.Source {
			return entry, true
		}
		if !fallbackFound {
			fallback, fallbackFound = entry, true
		}
	}
	return fallback, fallbackFound
}
