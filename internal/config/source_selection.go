package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"gopkg.in/yaml.v3"
)

// EditSourceSelection selects every resource of a Git or directory source in
// configPath, or removes every selection configPath makes of it. declaringPath
// is the real root path; it resolves v4 includes, relative directories, and
// the destination layer when configPath is a staged copy.
//
// Selecting writes all: true to the source's v4 resource group, or
// default: true to its legacy registry entries, and drops that file's
// exclusions and now redundant includes and tags of the source so no resource
// stays deselected. Deselecting removes those selections and keeps the
// file's exclusions. The source stays declared either way, so it remains
// browsable when no other config declares it. A source that is required in
// configPath is refused, as is a selection an included file, or in a legacy
// file its top-level tags, would still override. A missing file is created,
// as a v4 document, only to select.
func EditSourceSelection(configPath, declaringPath string, entry RegistryEntry, selectAll bool) (changed bool, err error) {
	if strings.TrimSpace(entry.URL) == "" {
		return false, fmt.Errorf("registry url is required")
	}
	err = withConfigPathLock(configPath, func() error {
		data, readErr := readConfigFile(configPath)
		missing := errors.Is(readErr, os.ErrNotExist)
		if readErr != nil && !missing {
			return readErr
		}
		if missing {
			if !selectAll {
				return nil
			}
			data = initialSourceEditData(declaringPath)
		}
		if PeekAPIVersion(data) == APIVersionV4 {
			var editErr error
			changed, editErr = editV4SourceSelection(configPath, declaringPath, data, entry, selectAll)
			return editErr
		}
		var editErr error
		changed, editErr = editLegacySourceSelection(configPath, declaringPath, entry, selectAll)
		return editErr
	})
	slog.Debug("source selection edit", "path", declaringPath, "select_all", selectAll, "changed", changed, "error", err)
	return changed, err
}

func editLegacySourceSelection(configPath, declaringPath string, entry RegistryEntry, selectAll bool) (bool, error) {
	url := strings.TrimSpace(entry.URL)
	if selectAll {
		if err := validateRegistryWrite(declaringPath, RegistryEntry{URL: url}); err != nil {
			return false, err
		}
	}
	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return false, err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return false, fmt.Errorf("update config %q: %w", configPath, err)
	}
	_, registries := findTopLevelSequence(root, "registries")
	if registries != nil && registries.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("update config %q: registries must be a list", configPath)
	}
	target, err := legacySourceEditNode(registries, entry, declaringPath)
	if err != nil {
		return false, err
	}
	// Registry entries of user and repository configs inherit the top-level
	// tags and kind includes they do not set themselves; local configs apply
	// those selectors globally instead.
	var inherited legacyInheritedSelection
	if !isLocalConfigPath(declaringPath) {
		inherited = legacyTopLevelSelection(root)
		if selectAll && inherited.tags {
			return false, fmt.Errorf("update config %q: top-level tags select resources in every registry entry, so default: true cannot select all of %s; move the tags into registry entries first", declaringPath, url)
		}
	}
	changed := false
	matched := false
	if registries != nil {
		for _, entryNode := range registries.Content {
			if entryNode != target {
				continue
			}
			matched = true
			if boolMappingValue(entryNode, "required") {
				return false, fmt.Errorf("update config %q: registry %s is required there", declaringPath, url)
			}
			if selectAll {
				changed = selectLegacyEntryAll(entryNode, inherited) || changed
			} else {
				changed = clearLegacyEntrySelections(entryNode, inherited) || changed
			}
		}
	}
	if selectAll && !matched {
		if registries, err = ensureTopLevelSequence(root, "registries"); err != nil {
			return false, fmt.Errorf("update config %q: %w", configPath, err)
		}
		entryNode := legacySourceEntryNode(entry)
		selectLegacyEntryAll(entryNode, inherited)
		registries.Content = append(registries.Content, entryNode)
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, writeConfigDocument(configPath, mode, doc)
}

// legacyInheritedSelection records the top-level selectors of a legacy user
// or repository config that its registry entries inherit.
type legacyInheritedSelection struct {
	tags     bool
	includes []string // kind keys with a top-level include
}

func legacyTopLevelSelection(root *yaml.Node) legacyInheritedSelection {
	var out legacyInheritedSelection
	if tags := findMappingValue(root, "tags"); tags != nil && tags.Kind == yaml.SequenceNode && len(tags.Content) > 0 {
		out.tags = true
	}
	for _, kind := range AllKinds {
		kindKey := skill.ConfigKeyForKind(kind)
		selector := findMappingValue(root, kindKey)
		include := selector
		if selector != nil && selector.Kind == yaml.MappingNode {
			include = findMappingValue(selector, "include")
		}
		if include != nil && include.Kind == yaml.SequenceNode && len(include.Content) > 0 {
			out.includes = append(out.includes, kindKey)
		}
	}
	return out
}

// selectLegacyEntryAll makes a legacy registry entry node select every
// resource of its source. default: true selects everything only while the
// entry has no include or tags, own or inherited, and its exclusions would
// still deselect, so an inherited include is overridden with an empty one.
func selectLegacyEntryAll(entryNode *yaml.Node, inherited legacyInheritedSelection) bool {
	changed := false
	if !boolMappingValue(entryNode, "default") {
		setMappingValue(entryNode, "default", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		changed = true
	}
	if removeMappingKeyByName(entryNode, "tags") {
		changed = true
	}
	if removeMappingKeyByName(entryNode, "exclude_all") {
		changed = true
	}
	for _, kind := range AllKinds {
		kindKey := skill.ConfigKeyForKind(kind)
		if removeLegacySelectorKeys(entryNode, kindKey, "exclude", "exclude_all") {
			changed = true
		}
		if clearLegacyInclude(entryNode, kindKey, slices.Contains(inherited.includes, kindKey)) {
			changed = true
		}
	}
	return changed
}

// clearLegacyEntrySelections removes the selections of a legacy registry
// entry node and keeps its exclusions. Inherited includes and tags are
// overridden with empty ones so the entry selects nothing.
func clearLegacyEntrySelections(entryNode *yaml.Node, inherited legacyInheritedSelection) bool {
	changed := removeMappingKeyByName(entryNode, "default")
	if inherited.tags {
		if tags := findMappingValue(entryNode, "tags"); tags == nil || tags.Kind != yaml.SequenceNode || len(tags.Content) > 0 {
			setMappingValue(entryNode, "tags", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle})
			changed = true
		}
	} else if removeMappingKeyByName(entryNode, "tags") {
		changed = true
	}
	for _, kind := range AllKinds {
		kindKey := skill.ConfigKeyForKind(kind)
		if clearLegacyInclude(entryNode, kindKey, slices.Contains(inherited.includes, kindKey)) {
			changed = true
		}
	}
	return changed
}

// clearLegacyInclude removes the include of the kind selector kindKey of a
// legacy registry entry node or, when the entry would inherit a top-level
// include instead, makes it an explicit empty include.
func clearLegacyInclude(entryNode *yaml.Node, kindKey string, overrideInherited bool) bool {
	if !overrideInherited {
		return removeLegacySelectorKeys(entryNode, kindKey, "include")
	}
	empty := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	selector := findMappingValue(entryNode, kindKey)
	if selector == nil || selector.Kind != yaml.MappingNode {
		setMappingValue(entryNode, kindKey, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"}, empty,
		}})
		return true
	}
	if include := findMappingValue(selector, "include"); include != nil && include.Kind == yaml.SequenceNode && len(include.Content) == 0 {
		return false
	}
	setMappingValue(selector, "include", empty)
	return true
}

// removeLegacySelectorKeys removes keys from the kind selector kindKey of a
// legacy registry entry node, dropping a selector the removal leaves empty. A
// list-form selector is an include list.
func removeLegacySelectorKeys(entryNode *yaml.Node, kindKey string, keys ...string) bool {
	selector := findMappingValue(entryNode, kindKey)
	if selector == nil {
		return false
	}
	if selector.Kind == yaml.SequenceNode {
		for _, key := range keys {
			if key == "include" {
				return removeMappingKeyByName(entryNode, kindKey)
			}
		}
		return false
	}
	if selector.Kind != yaml.MappingNode {
		return false
	}
	changed := false
	for _, key := range keys {
		if removeMappingKeyByName(selector, key) {
			changed = true
		}
	}
	if changed && len(selector.Content) == 0 {
		removeMappingKeyByName(entryNode, kindKey)
	}
	return changed
}

func editV4SourceSelection(configPath, declaringPath string, data []byte, entry RegistryEntry, selectAll bool) (bool, error) {
	layer := v4EditLayer(declaringPath)
	loaded, err := loadV4TreeWithOverlays(declaringPath, true, map[string]bool{}, nil, data, "", layer)
	if err != nil {
		return false, err
	}
	if err := loaded.validateLayerFields(layer); err != nil {
		return false, err
	}
	kind, err := v4EditSourceKind(entry, declaringPath)
	if err != nil {
		return false, err
	}
	identity, err := v4EditSourceIdentity(entry, declaringPath, layer, kind)
	if err != nil {
		return false, err
	}
	sourceName, err := v4EditMatchSource(loaded, identity, declaringPath)
	if err != nil {
		return false, err
	}
	if sourceName == "" && !selectAll {
		return false, nil
	}
	if group, ok := loaded.Groups[sourceName]; ok && group.HasRequired && group.Required != nil && *group.Required {
		return false, v4err("v4.edit-required", "resources."+sourceName+".required", 0, "source %q is required in this file", sourceName)
	}
	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return false, err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return false, err
	}
	changed := false
	if selectAll {
		if sourceName == "" {
			if layer == userLayer {
				if err := ValidateGlobalRegistryWrite(entry); err != nil {
					return false, err
				}
			}
			occupied := make(map[string]v4MigrationSource, len(loaded.Sources))
			for name := range loaded.Sources {
				occupied[name] = v4MigrationSource{}
			}
			sourceName = v4MigrationRegistrySourceName(occupied, entry.URL)
			if err := addV4EditSource(root, sourceName, entry, identity, kind); err != nil {
				return false, err
			}
			changed = true
		}
		canonical, _ := canonicalConfigPath(declaringPath)
		for _, decision := range loaded.Groups[sourceName].Decisions {
			if decision.Action == V4SelectionExclude && decision.Provenance.File != canonical {
				return false, v4err("v4.edit-exclusion", "resources."+sourceName+".exclude", 0, "the %q exclusion is inherited from %s; remove it there", decision.Exclusion, decision.Provenance.File)
			}
		}
		resources, err := ensureV4EditMapping(root, "resources")
		if err != nil {
			return false, err
		}
		group, groupChanged, err := ensureV4EditGroup(resources, sourceName)
		if err != nil {
			return false, err
		}
		changed = changed || groupChanged
		if all := findMappingValue(group, "all"); all == nil || all.Kind != yaml.ScalarNode || all.Value != "true" {
			setMappingValue(group, "all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			changed = true
		}
		for _, key := range []string{"include", "tags", "exclude"} {
			if removeMappingKeyByName(group, key) {
				changed = true
			}
		}
	} else {
		resources := findMappingValue(root, "resources")
		if group := findMappingValue(resources, sourceName); group != nil {
			if group.Kind == yaml.SequenceNode {
				changed = removeMappingKeyByName(resources, sourceName)
			} else {
				for _, key := range []string{"all", "tags", "include"} {
					if removeMappingKeyByName(group, key) {
						changed = true
					}
				}
				if len(group.Content) == 0 {
					removeMappingKeyByName(resources, sourceName)
				}
			}
			if resources != nil && len(resources.Content) == 0 {
				removeMappingKeyByName(root, "resources")
			}
		}
	}
	after := loaded
	var candidate []byte
	if changed {
		if candidate, after, err = encodeV4EditCandidate(configPath, declaringPath, root, layer); err != nil {
			return false, err
		}
	}
	if !selectAll {
		if group, used := after.Groups[sourceName]; used && v4GroupSelects(group) {
			return false, v4err("v4.edit-selection", "resources."+sourceName, 0, "an included config file still selects source %q; change it there", sourceName)
		}
	}
	if !changed {
		return false, nil
	}
	mode, err := sourceEditMode(configPath)
	if err != nil {
		return false, err
	}
	if err := writeConfigDocumentData(configPath, mode, candidate); err != nil {
		return false, err
	}
	return true, nil
}

func v4GroupSelects(group V4ResourceGroup) bool {
	return len(group.Include) > 0 || (group.HasAll && group.All != nil && *group.All) || (group.HasTags && len(group.Tags) > 0)
}
