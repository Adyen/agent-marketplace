package config

import (
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

type v4EditIdentity struct {
	kind          V4SourceKind
	url           string
	ref           string
	path          string
	sourceHarness string
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
		return v4EditIdentity{kind: kind, url: filepath.Clean(absolute), sourceHarness: strings.TrimSpace(entry.SourceHarness)}, nil
	}
	return v4EditIdentity{kind: kind, url: NormalizeGitURL(strings.TrimSpace(entry.URL)), ref: strings.TrimSpace(entry.Ref), path: NormalizeRegistryPath(entry.Path), sourceHarness: strings.TrimSpace(entry.SourceHarness)}, nil
}

func v4EditSourcePhysicalMatches(source V4Source, identity v4EditIdentity) bool {
	if source.Kind != identity.kind {
		return false
	}
	if source.Kind == V4SourceDirectory {
		return filepath.Clean(source.CanonicalURL) == filepath.Clean(identity.url)
	}
	return NormalizeGitURL(source.CanonicalURL) == identity.url && source.Ref == identity.ref && NormalizeRegistryPath(source.Path) == identity.path
}

func v4EditIdentityPhysicalEqual(a, b v4EditIdentity) bool {
	return a.kind == b.kind && a.url == b.url && a.ref == b.ref && a.path == b.path
}

func v4DirectorySourceValue(entry RegistryEntry, identity v4EditIdentity) string {
	raw := strings.TrimSpace(entry.URL)
	lower := strings.ToLower(raw)
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "~") || strings.HasPrefix(lower, "file://") {
		return identity.url
	}
	value := filepath.ToSlash(filepath.Clean(filepath.Join(raw, strings.TrimSpace(entry.Path))))
	if strings.HasPrefix(filepath.ToSlash(raw), "./") && value != "." && !strings.HasPrefix(value, "./") {
		value = "./" + value
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
		setMappingValue(value, "directory", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v4DirectorySourceValue(entry, identity)})
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

func v4EditLoadedSelectionSuppressed(group V4ResourceGroup, locator, canonical string, ignoreRootExact bool) bool {
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
