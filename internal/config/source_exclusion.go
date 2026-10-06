package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"gopkg.in/yaml.v3"
)

// SourceExclusionEdit selects how EditSourceExclusion changes the exclusions
// one source has in a config file.
type SourceExclusionEdit uint8

const (
	// SourceExcludeAll excludes every resource of the source: exclude_all in
	// a legacy file, the "*" exclusion in a v4 file.
	SourceExcludeAll SourceExclusionEdit = iota
	// SourceClearExcludeAll removes that source-wide exclusion.
	SourceClearExcludeAll
	// SourceClearResourceExclusions removes the exclusions of individual
	// resources and kinds of the source, keeping a source-wide exclusion.
	SourceClearResourceExclusions
)

func (e SourceExclusionEdit) String() string {
	switch e {
	case SourceExcludeAll:
		return "exclude_all"
	case SourceClearExcludeAll:
		return "clear_exclude_all"
	case SourceClearResourceExclusions:
		return "clear_resource_exclusions"
	}
	return "unknown"
}

func v4EditLayer(path string) layerKind {
	if isLocalConfigPath(path) {
		return localLayer
	}
	if isGlobalConfigPath(path) {
		return userLayer
	}
	return repoLayer
}

// EditSourceExclusion changes the exclusions a Git or directory source has in
// configPath. declaringPath is the real root path; it resolves v4 includes,
// relative directories, and the destination layer when configPath is a staged
// copy. A v4 root is edited through its resource groups and a legacy root
// through its registry entries. Includes and other selections are kept, so
// clearing a source-wide exclusion restores the previous selection. A
// missing file is created as a v4 document only to add an exclusion.
func EditSourceExclusion(configPath, declaringPath string, entry RegistryEntry, edit SourceExclusionEdit) (changed bool, err error) {
	if edit > SourceClearResourceExclusions {
		return false, fmt.Errorf("unsupported source exclusion edit")
	}
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
			if edit != SourceExcludeAll {
				return nil
			}
			data = initialSourceEditData(declaringPath)
		}
		if PeekAPIVersion(data) == APIVersionV4 {
			var editErr error
			changed, editErr = editV4SourceExclusion(configPath, declaringPath, data, entry, edit)
			return editErr
		}
		var editErr error
		changed, editErr = editLegacySourceExclusion(configPath, declaringPath, entry, edit)
		return editErr
	})
	slog.Debug("source exclusion edit", "path", declaringPath, "edit", edit.String(), "changed", changed, "error", err)
	return changed, err
}

func initialSourceEditData(declaringPath string) []byte {
	body := "apiVersion: " + APIVersionV4 + "\n"
	if isGlobalConfigPath(declaringPath) {
		body += "harnesses: [" + strings.Join(DefaultHarnesses(), ", ") + "]\n"
	}
	return []byte(body)
}

func sourceEditMode(configPath string) (os.FileMode, error) {
	info, err := os.Stat(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return defaultConfigMode, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat config %q: %w", configPath, err)
	}
	return info.Mode().Perm(), nil
}

func editLegacySourceExclusion(configPath, declaringPath string, entry RegistryEntry, edit SourceExclusionEdit) (bool, error) {
	url := strings.TrimSpace(entry.URL)
	if edit == SourceExcludeAll {
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
	changed := false
	matched := false
	if registries != nil {
		kept := registries.Content[:0]
		for _, entryNode := range registries.Content {
			if entryNode != target {
				kept = append(kept, entryNode)
				continue
			}
			matched = true
			entryChanged := false
			switch edit {
			case SourceExcludeAll:
				if !boolMappingValue(entryNode, "exclude_all") {
					setMappingValue(entryNode, "exclude_all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
					entryChanged = true
				}
			case SourceClearExcludeAll:
				entryChanged = removeMappingKeyByName(entryNode, "exclude_all")
			case SourceClearResourceExclusions:
				entryChanged = clearEntryResourceExclusions(entryNode)
			}
			changed = changed || entryChanged
			// An entry that only named the source to carry the removed
			// exclusion would otherwise linger as an empty shadow.
			if entryChanged && edit != SourceExcludeAll && registryEntryHasOnlyIdentity(entryNode) {
				continue
			}
			kept = append(kept, entryNode)
		}
		registries.Content = kept
		if changed && len(kept) == 0 {
			removeMappingKeyByName(root, "registries")
		}
	}
	if edit == SourceExcludeAll && !matched {
		if registries, err = ensureTopLevelSequence(root, "registries"); err != nil {
			return false, fmt.Errorf("update config %q: %w", configPath, err)
		}
		entryNode := legacySourceEntryNode(entry)
		setMappingValue(entryNode, "exclude_all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		registries.Content = append(registries.Content, entryNode)
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, writeConfigDocument(configPath, mode, doc)
}

func legacySourceEditNode(registries *yaml.Node, entry RegistryEntry, configPath string) (*yaml.Node, error) {
	if registries == nil {
		return nil, nil
	}
	want := strings.TrimSpace(entry.SourceHarness)
	var physical, exact []*yaml.Node
	for _, item := range registries.Content {
		if item.Kind != yaml.MappingNode || !legacyEntryNodePhysicalMatches(item, entry) {
			continue
		}
		harness := legacyEntryNodeHarness(item)
		if !SourceLayoutCompatible(harness, want) {
			continue
		}
		physical = append(physical, item)
		if harness == want {
			exact = append(exact, item)
		}
	}
	matches := physical
	if len(exact) > 0 {
		matches = exact
	}
	if len(matches) > 1 {
		return nil, v4err("config.edit.ambiguous-source", configPath, 0, "registry identity matches multiple entries")
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, nil
}

// SourceLayoutCompatible reports whether two source_harness values can name
// the same source layout of one registry location. An empty value is an
// autodetected or unknown layout and matches any other; two explicit,
// different layouts are separate sources.
func SourceLayoutCompatible(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a == "" || b == "" || a == b
}

func registryEntryHasOnlyIdentity(entryNode *yaml.Node) bool {
	for i := 0; i+1 < len(entryNode.Content); i += 2 {
		switch entryNode.Content[i].Value {
		case "url", "ref", "path", "source_harness":
		default:
			return false
		}
	}
	return true
}

// clearEntryResourceExclusions removes each kind's exclude list and
// exclude_all flag from a legacy registry entry node, dropping a kind
// selector the removal leaves empty.
func clearEntryResourceExclusions(entryNode *yaml.Node) bool {
	changed := false
	for _, kind := range AllKinds {
		kindKey := skill.ConfigKeyForKind(kind)
		selector := findMappingValue(entryNode, kindKey)
		if selector == nil || selector.Kind != yaml.MappingNode {
			continue
		}
		if removeMappingKeyByName(selector, "exclude") {
			changed = true
		}
		if removeMappingKeyByName(selector, "exclude_all") {
			changed = true
		}
		if len(selector.Content) == 0 {
			removeMappingKeyByName(entryNode, kindKey)
		}
	}
	return changed
}

func editV4SourceExclusion(configPath, declaringPath string, data []byte, entry RegistryEntry, edit SourceExclusionEdit) (bool, error) {
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
	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return false, err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return false, err
	}
	changed := false
	switch edit {
	case SourceExcludeAll:
		if sourceName == "" {
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
		resources, err := ensureV4EditMapping(root, "resources")
		if err != nil {
			return false, err
		}
		group, groupChanged, err := ensureV4EditGroup(resources, sourceName)
		if err != nil {
			return false, err
		}
		changed = changed || groupChanged
		exclude := findMappingValue(group, "exclude")
		if exclude == nil {
			exclude = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setMappingValue(group, "exclude", exclude)
		} else if exclude.Kind != yaml.SequenceNode {
			return false, v4err("v4.type", "resources."+sourceName+".exclude", exclude.Line, "must be a list")
		}
		if !v4EditSequenceContains(exclude, "*") {
			exclude.Content = append(exclude.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "*"})
			changed = true
		}
	case SourceClearExcludeAll, SourceClearResourceExclusions:
		if sourceName == "" {
			return false, nil
		}
		if edit == SourceClearExcludeAll {
			canonical, _ := canonicalConfigPath(declaringPath)
			for _, decision := range loaded.Groups[sourceName].Decisions {
				if decision.Action == V4SelectionExclude && decision.Exclusion == "*" && decision.Provenance.File != canonical {
					return false, v4err("v4.edit-exclusion", "resources."+sourceName+".exclude", 0, "the \"*\" exclusion is inherited from %s; remove it there", decision.Provenance.File)
				}
			}
		}
		keep := func(item *yaml.Node) bool {
			if item.Kind != yaml.ScalarNode {
				return true
			}
			if edit == SourceClearExcludeAll {
				return item.Value != "*"
			}
			return item.Value == "*"
		}
		changed = v4EditFilterRootExclusions(root, sourceName, keep)
	}
	if !changed {
		return false, nil
	}
	candidate, candidateV4, err := encodeV4EditCandidate(configPath, declaringPath, root, layer)
	if err != nil {
		return false, err
	}
	if edit != SourceExcludeAll {
		if _, used := candidateV4.Groups[sourceName]; !used && removeMappingKeyByName(findMappingValue(root, "sources"), sourceName) {
			// The source now selects and excludes nothing anywhere in the
			// tree, so its declaration only carried the removed exclusion.
			if sources := findMappingValue(root, "sources"); sources != nil && len(sources.Content) == 0 {
				removeMappingKeyByName(root, "sources")
			}
			if candidate, _, err = encodeV4EditCandidate(configPath, declaringPath, root, layer); err != nil {
				return false, err
			}
		}
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

// encodeV4EditCandidate encodes root and validates it as the v4 tree rooted at
// declaringPath.
func encodeV4EditCandidate(configPath, declaringPath string, root *yaml.Node, layer layerKind) ([]byte, *V4Config, error) {
	candidate, err := encodeConfigDocument(configPath, &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
	if err != nil {
		return nil, nil, err
	}
	candidateV4, err := loadV4TreeWithOverlays(declaringPath, true, map[string]bool{}, nil, candidate, "", layer)
	if err != nil {
		return nil, nil, err
	}
	if err := candidateV4.validateLayerFields(layer); err != nil {
		return nil, nil, err
	}
	return candidate, candidateV4, nil
}

// v4EditFilterRootExclusions keeps only the exclusions of the root document's
// group for source that keep accepts, then drops the exclude list, group, and
// resources mapping the removal leaves empty.
func v4EditFilterRootExclusions(root *yaml.Node, source string, keep func(*yaml.Node) bool) bool {
	resources := findMappingValue(root, "resources")
	group := findMappingValue(resources, source)
	exclude := findMappingValue(group, "exclude")
	if exclude == nil || exclude.Kind != yaml.SequenceNode {
		return false
	}
	kept := exclude.Content[:0]
	for _, item := range exclude.Content {
		if keep(item) {
			kept = append(kept, item)
		}
	}
	if len(kept) == len(exclude.Content) {
		return false
	}
	exclude.Content = kept
	if len(exclude.Content) == 0 {
		removeMappingKeyByName(group, "exclude")
		if len(group.Content) == 0 {
			removeMappingKeyByName(resources, source)
		}
		if len(resources.Content) == 0 {
			removeMappingKeyByName(root, "resources")
		}
	}
	return true
}

// v4EditSourceKind returns the v4 source kind an editor write targets and
// rejects entries the selection editors cannot represent.
func v4EditSourceKind(entry RegistryEntry, declaringPath string) (V4SourceKind, error) {
	kind := entry.V4Kind
	if kind == "" {
		if entry.IsLocal() {
			kind = V4SourceDirectory
		} else {
			kind = V4SourceGit
		}
	}
	if kind != V4SourceDirectory && kind != V4SourceGit {
		return "", v4err("v4.edit-source", declaringPath, 0, "selection editor supports only git and directory sources")
	}
	if err := ValidateRegistryPath(entry.Path); err != nil {
		return "", v4err("v4.edit-source", "registry.path", 0, "%v", err)
	}
	if kind == V4SourceGit && strings.TrimSpace(entry.Ref) == "" {
		return "", v4err("v4.edit-source", "registry.ref", 0, "git sources require an explicit ref")
	}
	return kind, nil
}

// v4EditMatchSource returns the name of the loaded source bound to identity,
// or "" when none is. Several matches are refused because an edit to one of
// them would leave the others' selections in force.
func v4EditMatchSource(loaded *V4Config, identity v4EditIdentity, declaringPath string) (string, error) {
	names := make([]string, 0, len(loaded.Sources))
	for sourceName := range loaded.Sources {
		names = append(names, sourceName)
	}
	sort.Strings(names)
	var physical, exact []string
	for _, sourceName := range names {
		if v4EditSourcePhysicalMatches(loaded.Sources[sourceName], identity) &&
			SourceLayoutCompatible(loaded.Sources[sourceName].SourceHarness, identity.sourceHarness) {
			physical = append(physical, sourceName)
			if loaded.Sources[sourceName].SourceHarness == identity.sourceHarness {
				exact = append(exact, sourceName)
			}
		}
	}
	matches := physical
	if len(exact) > 0 {
		matches = exact
	}
	if len(matches) > 1 {
		return "", v4err("v4.edit-ambiguous-source", declaringPath, 0, "registry identity matches multiple sources: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", nil
}
