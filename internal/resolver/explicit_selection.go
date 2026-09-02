package resolver

import (
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type ExplicitSelection struct {
	Kind             ir.ResourceKind
	Name             string
	Source           config.Source
	RegistryURL      string
	RegistryPath     string
	Ref              string
	Required         bool
	AutoSelected     bool
	AutoRegistries   []config.RegistryEntry
	OwningRegistries []config.RegistryEntry
	TopLevel         bool
}

func ExplicitSelections(ec config.EffectiveConfig, name string) []ExplicitSelection {
	var selections []ExplicitSelection
	selections = appendConfigExplicitSelections(selections, ec.Organization.Config, config.Organization, name)
	selections = appendConfigExplicitSelections(selections, ec.User.Config, config.UserPersonal, name)
	selections = appendConfigExplicitSelections(selections, ec.Repo.Config, config.RepoCommitted, name)
	selections = appendLocalExplicitSelections(selections, ec.Local, name)
	return slices.DeleteFunc(selections, func(selection ExplicitSelection) bool {
		return !explicitSelectionEnabled(selection, ec.Merged, ec.Local)
	})
}

func appendConfigExplicitSelections(dst []ExplicitSelection, cfg *config.Config, source config.Source, name string) []ExplicitSelection {
	if cfg == nil {
		return dst
	}
	return appendLayerExplicitSelections(dst, cfg.SelectorForKind, cfg.Registries, cfg.Tags, source, name)
}

func appendLocalExplicitSelections(dst []ExplicitSelection, cfg *config.LocalConfig, name string) []ExplicitSelection {
	if cfg == nil {
		return dst
	}
	return appendLayerExplicitSelections(dst, cfg.SelectorForKind, cfg.Registries, nil, config.RepoPersonal, name)
}

func appendLayerExplicitSelections(dst []ExplicitSelection, selector func(ir.ResourceKind) config.KindSelector, entries []config.RegistryEntry, inheritedTags []string, source config.Source, name string) []ExplicitSelection {
	autoRegistries := autoSelectingRegistries(entries, inheritedTags)
	for _, kind := range config.AllKinds {
		if selectionContains(selector(kind).Include, name) {
			owners := owningRegistries(entries, kind)
			required := slices.ContainsFunc(owners, func(entry config.RegistryEntry) bool { return entry.Required })
			dst = append(dst, ExplicitSelection{Kind: kind, Name: name, Source: source, Required: required, AutoSelected: len(autoRegistries) > 0, AutoRegistries: autoRegistries, OwningRegistries: owners, TopLevel: true})
		}
		for _, entry := range entries {
			if selectionContains(entry.SelectorForKind(kind).Include, name) {
				autoSelected := entry.Default || len(entry.Tags) > 0 || (entry.Tags == nil && len(inheritedTags) > 0)
				dst = append(dst, ExplicitSelection{Kind: kind, Name: name, Source: source, RegistryURL: entry.URL, RegistryPath: entry.Path, Ref: entry.Ref, Required: entry.Required, AutoSelected: autoSelected})
			}
		}
	}
	return dst
}

func explicitSelectionEnabled(selection ExplicitSelection, cfg *config.Config, localCfg *config.LocalConfig) bool {
	if selection.Required {
		return true
	}
	if localSelectionExcluded(selection, localCfg) {
		return false
	}
	if selection.Source == config.RepoPersonal && selection.TopLevel {
		return true
	}
	if cfg == nil {
		return true
	}
	for _, entry := range cfg.Registries {
		if entry.Source != selection.Source || !selectionRegistryMatches(selection, entry) || !entryExplicitlySelects(entry, selection.Kind, selection.Name) {
			continue
		}
		artifact := ResolvedArtifact{Entry: catalog.Entry{Kind: selection.Kind, Name: selection.Name}, RegistryURL: entry.URL, RegistryPath: entry.Path, Ref: entry.Ref, Source: entry.Source, Required: entry.Required}
		if !artifactIsCrossLayerExcluded(artifact, cfg.Registries) {
			return true
		}
	}
	return false
}

func selectionRegistryMatches(selection ExplicitSelection, entry config.RegistryEntry) bool {
	if selection.TopLevel {
		return slices.ContainsFunc(selection.OwningRegistries, func(owner config.RegistryEntry) bool {
			return config.NormalizeGitURL(owner.URL) == config.NormalizeGitURL(entry.URL) && strings.TrimSpace(owner.Ref) == strings.TrimSpace(entry.Ref) && strings.TrimSpace(owner.Path) == strings.TrimSpace(entry.Path)
		})
	}
	return config.NormalizeGitURL(selection.RegistryURL) == config.NormalizeGitURL(entry.URL) && strings.TrimSpace(selection.Ref) == strings.TrimSpace(entry.Ref) && strings.TrimSpace(selection.RegistryPath) == strings.TrimSpace(entry.Path)
}

func entryExplicitlySelects(entry config.RegistryEntry, kind ir.ResourceKind, name string) bool {
	selected := resolveSelectedResources(entry, []catalog.Entry{{Kind: kind, Name: name}})
	return slices.Contains(selected, resourceSelection{kind: kind, name: strings.TrimSpace(name)})
}

func localSelectionExcluded(selection ExplicitSelection, localCfg *config.LocalConfig) bool {
	if localCfg == nil || selection.Source.IsMachineScope() || selection.Required {
		return false
	}
	selector := localCfg.SelectorForKind(selection.Kind)
	return selector.ExcludeAll || selectionContains(selector.Exclude, selection.Name)
}

func owningRegistries(entries []config.RegistryEntry, kind ir.ResourceKind) []config.RegistryEntry {
	return slices.Collect(func(yield func(config.RegistryEntry) bool) {
		for _, entry := range entries {
			if entry.SelectorForKind(kind).Include == nil && !yield(entry) {
				return
			}
		}
	})
}

func autoSelectingRegistries(entries []config.RegistryEntry, inheritedTags []string) []config.RegistryEntry {
	return slices.Collect(func(yield func(config.RegistryEntry) bool) {
		for _, entry := range entries {
			if (entry.Default || len(entry.Tags) > 0 || (entry.Tags == nil && len(inheritedTags) > 0)) && !yield(entry) {
				return
			}
		}
	})
}

func selectionContains(values []string, wanted string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) == strings.TrimSpace(wanted) })
}
