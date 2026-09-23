package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

type V4SelectionEdit uint8

const (
	V4SelectionEnable V4SelectionEdit = iota
	V4SelectionDisable
	V4SelectionClearExclude
)

func EditV4ResourceSelection(configPath, declaringPath string, entry RegistryEntry, kind ir.ResourceKind, name string, action V4SelectionEdit) (handled bool, err error) {
	err = withConfigPathLock(configPath, func() error {
		data, readErr := readConfigFile(configPath)
		if errors.Is(readErr, os.ErrNotExist) {
			return nil
		}
		if readErr != nil {
			handled = true
			return readErr
		}
		if PeekAPIVersion(data) != APIVersionV4 {
			return nil
		}
		handled = true
		return editV4ResourceSelection(configPath, declaringPath, data, entry, kind, name, action)
	})
	return handled, err
}

func editV4ResourceSelection(configPath, declaringPath string, data []byte, entry RegistryEntry, kind ir.ResourceKind, name string, action V4SelectionEdit) error {
	layer := v4EditLayer(declaringPath)
	loaded, err := loadV4TreeWithOverlays(declaringPath, true, map[string]bool{}, nil, data, "", layer)
	if err != nil {
		return err
	}
	if err := loaded.validateLayerFields(layer); err != nil {
		return err
	}
	if action > V4SelectionClearExclude {
		return v4err("v4.edit-action", declaringPath, 0, "unsupported selection edit")
	}
	if strings.TrimSpace(name) == "" {
		return v4err("v4.locator", "name", 0, "resource name is required")
	}
	v4Kind := entry.V4Kind
	if v4Kind == "" {
		if entry.IsLocal() {
			v4Kind = V4SourceDirectory
		} else {
			v4Kind = V4SourceGit
		}
	}
	if v4Kind != V4SourceDirectory && v4Kind != V4SourceGit {
		return v4err("v4.edit-source", declaringPath, 0, "selection editor supports only git and directory sources")
	}
	if err := ValidateRegistryPath(entry.Path); err != nil {
		return v4err("v4.edit-source", "registry.path", 0, "%v", err)
	}
	if v4Kind == V4SourceGit {
		if strings.TrimSpace(entry.Ref) == "" {
			return v4err("v4.edit-source", "registry.ref", 0, "git sources require an explicit ref")
		}
		if _, _, _, err := parseV4Locator(v4ResourceKindName(kind)+"/"+name, "source"); err != nil {
			return v4err("v4.locator", "resources", 0, "%v", err)
		}
	}
	identity, err := v4EditSourceIdentity(entry, declaringPath, layer, v4Kind)
	if err != nil {
		return err
	}
	matches := make([]string, 0, 1)
	names := make([]string, 0, len(loaded.Sources))
	for sourceName := range loaded.Sources {
		names = append(names, sourceName)
	}
	sort.Strings(names)
	for _, sourceName := range names {
		if v4EditSourceMatches(loaded.Sources[sourceName], identity) {
			matches = append(matches, sourceName)
		}
	}
	if len(matches) > 1 {
		return v4err("v4.edit-ambiguous-source", declaringPath, 0, "registry identity matches multiple sources: %s", strings.Join(matches, ", "))
	}

	root, err := ensureDocumentMappingFromData(data, configPath)
	if err != nil {
		return err
	}
	changed := false
	sourceName := ""
	if len(matches) == 1 {
		sourceName = matches[0]
	} else if action != V4SelectionClearExclude {
		if action == V4SelectionEnable && layer == userLayer {
			if err := ValidateGlobalRegistryWrite(entry); err != nil {
				return err
			}
		}
		occupied := make(map[string]v4MigrationSource, len(loaded.Sources))
		for name := range loaded.Sources {
			occupied[name] = v4MigrationSource{}
		}
		sourceName = v4MigrationRegistrySourceName(occupied, entry.URL)
		if err := addV4EditSource(root, sourceName, entry, identity, v4Kind); err != nil {
			return err
		}
		changed = true
	}
	if sourceName == "" {
		return nil
	}
	locator := v4ResourceKindName(kind) + "/" + name
	if _, _, _, err := parseV4Locator(locator, sourceName); err != nil {
		return v4err("v4.locator", "resources."+sourceName, 0, "%v", err)
	}
	resources, err := ensureV4EditMapping(root, "resources")
	if err != nil {
		return err
	}
	group, groupChanged, err := ensureV4EditGroup(resources, sourceName)
	if err != nil {
		return err
	}
	changed = changed || groupChanged
	include, exclude, err := v4EditGroupSequences(group)
	if err != nil {
		return err
	}
	rootExclude := v4EditSequenceContains(exclude, locator)
	switch action {
	case V4SelectionEnable:
		if v4EditSequenceHasWildcard(exclude, kind, name) {
			return v4err("v4.edit-exclusion", "resources."+sourceName+".exclude", 0, "wildcard exclusion suppresses %q", locator)
		}
		if v4EditRemoveLocator(exclude, locator) {
			changed = true
		}
		if !v4EditSequenceContains(include, locator) {
			include.Content = append(include.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: locator})
			changed = true
		}
	case V4SelectionDisable:
		if v4EditRemoveLocator(include, locator) {
			changed = true
		}
		if !rootExclude {
			exclude.Content = append(exclude.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: locator})
			changed = true
		}
	case V4SelectionClearExclude:
		if !rootExclude {
			if v4EditLoadedSelectionSuppressed(loaded.Groups[sourceName], locator, declaringPath, false) {
				return v4err("v4.edit-exclusion", "resources."+sourceName+".exclude", 0, "an inherited or wildcard exclusion still suppresses %q", locator)
			}
			return nil
		}
		if v4EditLoadedSelectionSuppressed(loaded.Groups[sourceName], locator, declaringPath, true) {
			return v4err("v4.edit-exclusion", "resources."+sourceName+".exclude", 0, "an inherited or wildcard exclusion still suppresses %q", locator)
		}
		if v4EditRemoveLocator(exclude, locator) {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	candidate, err := encodeConfigDocument(configPath, &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
	if err != nil {
		return err
	}
	candidateV4, err := loadV4TreeWithOverlays(declaringPath, true, map[string]bool{}, nil, candidate, "", layer)
	if err != nil {
		return err
	}
	if err := candidateV4.validateLayerFields(layer); err != nil {
		return err
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat config %q: %w", configPath, err)
	}
	if err := writeConfigDocumentData(configPath, info.Mode().Perm(), candidate); err != nil {
		return err
	}
	return nil
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

type v4EditIdentity struct {
	kind V4SourceKind
	url  string
	ref  string
	path string
}

func v4EditSourceIdentity(entry RegistryEntry, declaringPath string, layer layerKind, kind V4SourceKind) (v4EditIdentity, error) {
	if kind == V4SourceDirectory {
		base := v4DirectoryBase(declaringPath, true, layer)
		localPath, err := entry.ResolveLocalPath(base)
		if err != nil {
			return v4EditIdentity{}, v4err("v4.directory-path", "registry.url", 0, "%v", err)
		}
		bundle := entry.BundlePath(localPath)
		raw := strings.TrimSpace(entry.URL)
		if !filepath.IsAbs(raw) && !strings.HasPrefix(raw, "~") && !strings.HasPrefix(strings.ToLower(raw), "file://") {
			relative := filepath.Join(raw, strings.TrimSpace(entry.Path))
			if _, err := resolveV4DirectorySource(base, filepath.ToSlash(relative)); err != nil {
				return v4EditIdentity{}, v4err("v4.directory-path", "registry.url", 0, "%v", err)
			}
		}
		absolute, err := filepath.Abs(bundle)
		if err != nil {
			return v4EditIdentity{}, v4err("v4.directory-path", "registry.url", 0, "%v", err)
		}
		return v4EditIdentity{kind: kind, url: filepath.Clean(absolute)}, nil
	}
	return v4EditIdentity{kind: kind, url: NormalizeGitURL(strings.TrimSpace(entry.URL)), ref: strings.TrimSpace(entry.Ref), path: NormalizeRegistryPath(entry.Path)}, nil
}

func v4EditSourceMatches(source V4Source, identity v4EditIdentity) bool {
	if source.Kind != identity.kind {
		return false
	}
	if source.Kind == V4SourceDirectory {
		return v4EditCanonicalPath(source.CanonicalURL) == v4EditCanonicalPath(identity.url)
	}
	return NormalizeGitURL(source.CanonicalURL) == identity.url && source.Ref == identity.ref && NormalizeRegistryPath(source.Path) == identity.path
}

func v4EditCanonicalPath(value string) string {
	value = filepath.Clean(value)
	if resolved, err := filepath.EvalSymlinks(value); err == nil {
		return filepath.Clean(resolved)
	}
	return value
}

func addV4EditSource(root *yaml.Node, name string, entry RegistryEntry, identity v4EditIdentity, kind V4SourceKind) error {
	sources, err := ensureV4EditMapping(root, "sources")
	if err != nil {
		return err
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if kind == V4SourceDirectory {
		setMappingValue(value, "directory", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: identity.url})
	} else {
		setMappingValue(value, "git", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(entry.URL)})
		setMappingValue(value, "ref", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(entry.Ref)})
		if identity.path != "" {
			setMappingValue(value, "path", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(entry.Path)})
		}
	}
	if strings.TrimSpace(entry.SourceHarness) != "" {
		setMappingValue(value, "source_harness", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(entry.SourceHarness)})
	}
	sources.Content = append(sources.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
	return nil
}

func ensureDocumentMappingFromData(data []byte, configPath string) (*yaml.Node, error) {
	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return nil, err
	}
	return ensureDocumentMapping(doc)
}

func ensureV4EditMapping(root *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind != yaml.ScalarNode || root.Content[i].Value != key {
			continue
		}
		value := root.Content[i+1]
		if value.Kind != yaml.MappingNode {
			return nil, v4err("v4.type", key, value.Line, "must be a mapping")
		}
		return value, nil
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(root, key, value)
	return value, nil
}

func ensureV4EditGroup(resources *yaml.Node, source string) (*yaml.Node, bool, error) {
	for i := 0; i+1 < len(resources.Content); i += 2 {
		if resources.Content[i].Value != source {
			continue
		}
		value := resources.Content[i+1]
		if value.Kind == yaml.MappingNode {
			return value, false, nil
		}
		if value.Kind == yaml.SequenceNode {
			mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"}, value}}
			resources.Content[i+1] = mapping
			return mapping, false, nil
		}
		return nil, false, v4err("v4.type", "resources."+source, value.Line, "must be an include list or mapping")
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	resources.Content = append(resources.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: source}, value)
	return value, true, nil
}

func v4EditGroupSequences(group *yaml.Node) (*yaml.Node, *yaml.Node, error) {
	include := findMappingValue(group, "include")
	if include == nil {
		include = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(group, "include", include)
	} else if include.Kind != yaml.SequenceNode {
		return nil, nil, v4err("v4.type", "resources.include", include.Line, "must be a list")
	}
	exclude := findMappingValue(group, "exclude")
	if exclude == nil {
		exclude = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(group, "exclude", exclude)
	} else if exclude.Kind != yaml.SequenceNode {
		return nil, nil, v4err("v4.type", "resources.exclude", exclude.Line, "must be a list")
	}
	return include, exclude, nil
}

func v4EditSequenceContains(sequence *yaml.Node, locator string) bool {
	for _, item := range sequence.Content {
		if item.Kind == yaml.ScalarNode && item.Value == locator {
			return true
		}
		if item.Kind == yaml.MappingNode && len(item.Content) >= 2 && item.Content[0].Value == locator {
			return true
		}
	}
	return false
}

func v4EditRemoveLocator(sequence *yaml.Node, locator string) bool {
	for i, item := range sequence.Content {
		match := item.Kind == yaml.ScalarNode && item.Value == locator
		if !match && item.Kind == yaml.MappingNode && len(item.Content) >= 2 {
			match = item.Content[0].Value == locator
		}
		if match {
			sequence.Content = append(sequence.Content[:i], sequence.Content[i+1:]...)
			return true
		}
	}
	return false
}

func v4EditSequenceHasWildcard(sequence *yaml.Node, kind ir.ResourceKind, name string) bool {
	locator := v4ResourceKindName(kind) + "/" + name
	for _, item := range sequence.Content {
		if item.Kind == yaml.ScalarNode && item.Value != locator && v4ExclusionMatches(item.Value, locator) {
			return true
		}
	}
	return false
}

func v4EditLoadedSelectionSuppressed(group V4ResourceGroup, locator, declaringPath string, ignoreRootExact bool) bool {
	canonical, _ := canonicalConfigPath(declaringPath)
	suppressed := false
	for _, decision := range group.Decisions {
		if decision.Action == V4SelectionExclude {
			if ignoreRootExact && decision.Provenance.File == canonical && decision.Exclusion == locator {
				continue
			}
			if v4ExclusionMatches(decision.Exclusion, locator) {
				suppressed = true
			}
			continue
		}
		if decision.Action == V4SelectionExplicit && decision.Locator == locator {
			suppressed = false
		}
	}
	return suppressed
}
