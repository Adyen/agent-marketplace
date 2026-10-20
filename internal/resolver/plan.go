package resolver

import (
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// PlanPluginSelection runs the resolver's own selection over the given
// configuration and catalogs and returns the plan every plugin-v1 manifest
// derives from. The plan carries one record per (plugin, owning format
// source location, authoring layer), captured at the selection stages
// before occurrence deduplication, name collisions, and member displacement
// drop delivered artifacts: a selection authored in a layer is locked in
// that layer even when delivery resolves it away. Records merge only within
// one layer; the plan never deduplicates across layers.
func PlanPluginSelection(cfg *config.Config, catalogs map[string][]catalog.Entry, localCfg *config.LocalConfig, opts ...ResolveOption) (catalog.PluginSelectionPlan, []error) {
	_, errs, plan := ResolveWithPluginSelectionPlan(cfg, catalogs, localCfg, opts...)
	return plan, errs
}

// ResolveWithPluginSelectionPlan resolves materializable artifacts and the
// per-layer plugin selections in one resolver pass.
func ResolveWithPluginSelectionPlan(cfg *config.Config, catalogs map[string][]catalog.Entry, localCfg *config.LocalConfig, opts ...ResolveOption) ([]ResolvedArtifact, []error, catalog.PluginSelectionPlan) {
	sink := newPluginPlanSink()
	resolved, errs := Resolve(cfg, catalogs, localCfg, append(opts, withPluginPlanSink(sink))...)
	return resolved, errs, sink.plan()
}

// pluginPlanKey identifies one per-layer plan record: the plugin name, the
// layer that authored the selection, and the owning format source location.
type pluginPlanKey struct {
	name     string
	layer    config.Source
	location string
}

// pluginPlanRecord accumulates one layer's selection of one plugin at one
// source location: that layer's edges, the union of its authored member
// exclusions, and the plugin's full member set the exclusions fold over.
type pluginPlanRecord struct {
	source     config.RegistryEntry
	entry      catalog.Entry
	edges      []PluginEdge
	exclusions []string
	members    []ir.PluginMember
	deferred   bool
}

// pluginPlanSink collects per-layer plugin selection records during
// resolution, ahead of the stages that drop delivered artifacts.
type pluginPlanSink struct {
	records map[pluginPlanKey]*pluginPlanRecord
}

func newPluginPlanSink() *pluginPlanSink {
	return &pluginPlanSink{records: map[pluginPlanKey]*pluginPlanRecord{}}
}

// pluginSourceLocationKey renders a registry entry's canonical source
// location so same-location entries merge into one plan record.
func pluginSourceLocationKey(entry config.RegistryEntry) string {
	path := entry.Path
	if path == "" {
		path = "."
	}
	if entry.IsLocal() {
		return "local\x00" + entry.URL + "\x00" + strings.TrimSpace(entry.Ref) + "\x00" + path
	}
	canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
	if err != nil {
		canonical = normalizedRegistryURL(entry.URL)
	}
	return "git\x00" + canonical + "\x00" + strings.TrimSpace(entry.Ref) + "\x00" + path
}

func pluginEdgeLayer(edge PluginEdge) config.Source {
	if edge.Source == config.SourceUnset {
		return config.RepoCommitted
	}
	return edge.Source
}

// record folds one resolved plugin selection into the per-layer records:
// every edge of the selection joins the record of its authoring layer, and
// authored exclusions union deterministically.
func (s *pluginPlanSink) record(source config.RegistryEntry, name string, plugin *catalog.PluginEntry, artifactEntry catalog.Entry, sel *PluginSelection) {
	if s == nil || plugin == nil || sel == nil {
		return
	}
	location := pluginSourceLocationKey(source)
	for _, edge := range sel.Edges {
		key := pluginPlanKey{name: name, layer: pluginEdgeLayer(edge), location: location}
		rec := s.records[key]
		if rec == nil {
			rec = &pluginPlanRecord{
				source:   source,
				entry:    artifactEntry,
				members:  plugin.Members,
				deferred: plugin.Source.Kind == "git",
			}
			s.records[key] = rec
		}
		if !slices.Contains(rec.edges, edge) {
			rec.edges = append(rec.edges, edge)
		}
		for _, exclusion := range sel.Exclusions {
			if !slices.Contains(rec.exclusions, exclusion) {
				rec.exclusions = append(rec.exclusions, exclusion)
			}
		}
	}
}

// drop removes the layer records a displaced artifact carried: a layer
// whose only admission a required member displaced has no selection.
func (s *pluginPlanSink) drop(artifact ResolvedArtifact) {
	if s == nil || artifact.Selection == nil {
		return
	}
	location := pluginSourceLocationKey(config.RegistryEntry{
		URL:    artifact.RegistryURL,
		Ref:    artifact.Ref,
		Path:   artifact.RegistryPath,
		V4Kind: artifact.V4Kind,
	})
	for _, edge := range artifact.Selection.Edges {
		delete(s.records, pluginPlanKey{name: artifact.Name, layer: pluginEdgeLayer(edge), location: location})
	}
}

// plan projects the collected records into the selection plan: per-layer
// edges, member keys after that layer's exclusions (deferred for external
// entries, whose members decode at lock time), and sorted authored
// exclusions.
func (s *pluginPlanSink) plan() catalog.PluginSelectionPlan {
	plan := catalog.PluginSelectionPlan{}
	keys := make([]pluginPlanKey, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		if keys[i].location != keys[j].location {
			return keys[i].location < keys[j].location
		}
		return keys[i].layer < keys[j].layer
	})
	for _, key := range keys {
		rec := s.records[key]
		planned := catalog.PlannedPlugin{Source: rec.source, Name: key.name, Layer: key.layer, Entry: rec.entry}
		for _, edge := range rec.edges {
			planned.Edges = append(planned.Edges, catalog.PluginSelectionEdge{Type: edge.Kind, Collection: edge.Collection, Scope: edge.Source})
		}
		members := rec.members
		if !rec.deferred {
			for _, exclusion := range rec.exclusions {
				kept := make([]ir.PluginMember, 0, len(members))
				for _, member := range members {
					if !catalog.PluginMemberExclusionMatch(exclusion, member) {
						kept = append(kept, member)
					}
				}
				members = kept
			}
		}
		memberKeys := make([]string, 0, len(members))
		for _, member := range members {
			memberKeys = append(memberKeys, member.Key())
		}
		sort.Strings(memberKeys)
		planned.Members = memberKeys
		planned.Exclusions = append(planned.Exclusions, rec.exclusions...)
		sort.Strings(planned.Exclusions)
		plan.Plugins = append(plan.Plugins, planned)
	}
	return plan
}
