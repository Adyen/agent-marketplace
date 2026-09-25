package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (e *LayerEditor) marketplaceSourceNames(canonical string) []string {
	var matches []string
	for name, source := range e.loaded.Sources {
		if source.Kind == V4SourceMarketplace && source.CanonicalURL == canonical {
			matches = append(matches, name)
		}
	}
	for name, added := range e.addedV4 {
		if added.kind == V4SourceMarketplace && added.url == canonical {
			matches = append(matches, name)
		}
	}
	return matches
}

func (e *LayerEditor) marketplaceSourceBinding(name string) SourceBinding {
	rootOwned := true
	if source, ok := e.loaded.Sources[name]; ok {
		rootOwned = source.File == e.canonical
	}
	binding := SourceBinding{Name: name, Entry: RegistryEntry{URL: e.marketplaceBindingURL(name), V4Kind: V4SourceMarketplace}, id: fmt.Sprintf("v4:%p:%s", e, name), root: rootOwned}
	e.bindings[binding.id] = binding
	return binding
}

func (e *LayerEditor) marketplaceBindingURL(name string) string {
	if source, ok := e.loaded.Sources[name]; ok {
		return source.CanonicalURL
	}
	if added, ok := e.addedV4[name]; ok {
		return added.url
	}
	return ""
}

func (e *LayerEditor) LookupMarketplaceSource(marketplaceURL string) (SourceBinding, bool, error) {
	if e.legacy {
		return SourceBinding{}, false, v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "Marketplace endpoints require a v4 configuration layer")
	}
	canonical, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return SourceBinding{}, false, err
	}
	matches := e.marketplaceSourceNames(canonical)
	if len(matches) > 1 {
		sort.Strings(matches)
		return SourceBinding{}, false, v4err("config.edit.ambiguous-source", e.target.DeclaringPath, 0, "Marketplace endpoint matches multiple sources: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 0 {
		return SourceBinding{}, false, nil
	}
	return e.marketplaceSourceBinding(matches[0]), true, nil
}

func (e *LayerEditor) EnsureMarketplaceSource(marketplaceURL string) (SourceBinding, error) {
	if e.legacy {
		return SourceBinding{}, v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "Marketplace endpoints require a v4 configuration layer")
	}
	canonical, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return SourceBinding{}, err
	}
	matches := e.marketplaceSourceNames(canonical)
	if len(matches) > 1 {
		sort.Strings(matches)
		return SourceBinding{}, v4err("config.edit.ambiguous-source", e.target.DeclaringPath, 0, "Marketplace endpoint matches multiple sources: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 0 {
		occupied := make(map[string]v4MigrationSource, len(e.loaded.Sources)+len(e.addedV4))
		for name := range e.loaded.Sources {
			occupied[name] = v4MigrationSource{}
		}
		for name := range e.addedV4 {
			occupied[name] = v4MigrationSource{}
		}
		name := v4MigrationMarketplaceSourceName(occupied, "")
		sources, err := ensureV4EditMapping(e.root, "sources")
		if err != nil {
			return SourceBinding{}, err
		}
		value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(value, "marketplace", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: canonical})
		sources.Content = append(sources.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
		if e.addedV4 == nil {
			e.addedV4 = map[string]v4EditIdentity{}
		}
		e.addedV4[name] = v4EditIdentity{kind: V4SourceMarketplace, url: canonical}
		e.changed = true
		matches = []string{name}
	}
	name := matches[0]
	return e.marketplaceSourceBinding(name), nil
}

type v4MarketplaceRequest struct {
	locator  string
	as       string
	revision string
}

func marketplacePinV4Request(pin MarketplaceResourcePin) (v4MarketplaceRequest, bool, error) {
	if err := ValidateMarketplaceResourcePin(pin); err != nil {
		return v4MarketplaceRequest{}, false, err
	}
	if !pin.Direct && len(pin.SelectedBy.List()) > 0 {
		return v4MarketplaceRequest{}, true, nil
	}
	if pin.RegistrySlug == "" {
		return v4MarketplaceRequest{}, false, v4err("v4.registry-slug", "resources", 0, "Marketplace request requires a verified registry slug")
	}
	locator := pin.RegistrySlug + "/" + v4MigrationMarketplaceKind(pin.Kind) + "/" + pin.Name
	if _, _, _, err := parseV4Locator(locator, "resources"); err != nil {
		return v4MarketplaceRequest{}, false, v4err("v4.locator", locator, 0, "%v", err)
	}
	request := v4MarketplaceRequest{locator: locator}
	if pin.InstalledName != "" && pin.InstalledName != pin.Name {
		request.as = pin.InstalledName
	}
	if pin.Revision != "" && pin.Revision != pin.Digest {
		request.revision = pin.Revision
	}
	return request, false, nil
}

func v4MarketplaceIncludeItem(request v4MarketplaceRequest) *yaml.Node {
	if request.as == "" && request.revision == "" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: request.locator}
	}
	options := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if request.as != "" {
		setMappingValue(options, "as", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: request.as})
	}
	if request.revision != "" {
		setMappingValue(options, "revision", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: request.revision})
	}
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(item, request.locator, options)
	return item
}

func v4MarketplaceItemMatches(item *yaml.Node, request v4MarketplaceRequest) (bool, bool) {
	var locator, as, revision string
	switch item.Kind {
	case yaml.ScalarNode:
		locator = item.Value
	case yaml.MappingNode:
		if len(item.Content) != 2 {
			return false, false
		}
		locator = item.Content[0].Value
		options := item.Content[1]
		if options.Kind != yaml.MappingNode {
			return false, false
		}
		if node := findMappingValue(options, "as"); node != nil {
			as = node.Value
		}
		if node := findMappingValue(options, "revision"); node != nil {
			revision = node.Value
		}
	default:
		return false, false
	}
	if locator != request.locator {
		return false, false
	}
	return true, as == request.as && revision == request.revision
}

func (e *LayerEditor) IncludeMarketplacePins(binding SourceBinding, pins []MarketplaceResourcePin) error {
	if e.legacy {
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "Marketplace endpoints require a v4 configuration layer")
	}
	if issued, ok := e.bindings[binding.id]; binding.id == "" || !ok || issued.Name != binding.Name {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source binding was not issued by this editor")
	}
	name := binding.Name
	source, known := e.loaded.Sources[name]
	if added, addedSource := e.addedV4[name]; addedSource {
		if added.kind != V4SourceMarketplace {
			return v4err("v4.source-kind", "sources."+name, 0, "Marketplace request requires a Marketplace source")
		}
	} else if !known || source.Kind != V4SourceMarketplace {
		return v4err("v4.source-kind", "sources."+name, 0, "Marketplace request requires a Marketplace source")
	}
	var requests []v4MarketplaceRequest
	seen := make(map[string]v4MarketplaceRequest)
	for _, pin := range pins {
		request, member, err := marketplacePinV4Request(pin)
		if err != nil {
			return err
		}
		if member {
			continue
		}
		if existing, ok := seen[request.locator]; ok {
			if existing == request {
				continue
			}
			return v4err("v4.duplicate-request", "resources."+name, 0, "Marketplace resource %q is requested with conflicting options", request.locator)
		}
		seen[request.locator] = request
		requests = append(requests, request)
	}
	group, includes, err := e.v4MarketplaceResourceGroup(name)
	if err != nil {
		return err
	}
	_, excludes := v4GroupSeqs(group)
	var adds []v4MarketplaceRequest
	var clearExact []string
	for _, request := range requests {
		if excludes != nil {
			for _, item := range excludes.Content {
				if item.Kind == yaml.ScalarNode && item.Value != request.locator && v4ExclusionMatches(item.Value, request.locator) {
					return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "wildcard exclusion suppresses %q", request.locator)
				}
			}
			if v4EditSequenceContains(excludes, request.locator) {
				clearExact = append(clearExact, request.locator)
			}
		}
		conflict := false
		if includes != nil {
			for _, item := range includes.Content {
				same, identical := v4MarketplaceItemMatches(item, request)
				if !same {
					continue
				}
				if !identical {
					return v4err("v4.duplicate-request", "resources."+name, 0, "Marketplace resource %q is already requested with different options", request.locator)
				}
				conflict = true
			}
		}
		if !conflict {
			adds = append(adds, request)
		}
	}
	if len(adds) == 0 && len(clearExact) == 0 {
		return nil
	}
	for _, locator := range clearExact {
		v4EditRemoveLocator(excludes, locator)
	}
	if len(adds) > 0 && (group == nil || group.Kind == yaml.MappingNode) {
		group, err := e.ensureV4Group(name)
		if err != nil {
			return err
		}
		seq, err := e.ensureV4GroupSeq(group, name, "include")
		if err != nil {
			return err
		}
		includes = seq
	}
	for _, request := range adds {
		includes.Content = append(includes.Content, v4MarketplaceIncludeItem(request))
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) v4MarketplaceResourceGroup(name string) (*yaml.Node, *yaml.Node, error) {
	var group *yaml.Node
	if resources := findMappingValue(e.root, "resources"); resources != nil && resources.Kind == yaml.MappingNode {
		group = findMappingValue(resources, name)
	}
	var includes *yaml.Node
	switch {
	case group == nil:
	case group.Kind == yaml.SequenceNode:
		includes = group
	case group.Kind == yaml.MappingNode:
		includes = findMappingValue(group, "include")
	default:
		return nil, nil, v4err("v4.type", "resources."+name, 0, "resource group must be an include list or mapping")
	}
	return group, includes, nil
}

func (e *LayerEditor) RemoveMarketplacePins(binding SourceBinding, pins []MarketplaceResourcePin) error {
	if e.legacy {
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "Marketplace endpoints require a v4 configuration layer")
	}
	if issued, ok := e.bindings[binding.id]; binding.id == "" || !ok || issued.Name != binding.Name {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source binding was not issued by this editor")
	}
	name := binding.Name
	source, known := e.loaded.Sources[name]
	if added, addedSource := e.addedV4[name]; addedSource {
		if added.kind != V4SourceMarketplace {
			return v4err("v4.source-kind", "sources."+name, 0, "Marketplace request requires a Marketplace source")
		}
	} else if !known || source.Kind != V4SourceMarketplace {
		return v4err("v4.source-kind", "sources."+name, 0, "Marketplace request requires a Marketplace source")
	}
	var requests []v4MarketplaceRequest
	seen := make(map[string]bool)
	for _, pin := range pins {
		request, member, err := marketplacePinV4Request(pin)
		if err != nil {
			return err
		}
		if member || seen[request.locator] {
			continue
		}
		seen[request.locator] = true
		requests = append(requests, request)
	}
	group, includes, err := e.v4MarketplaceResourceGroup(name)
	if err != nil {
		return err
	}
	matched := make(map[*yaml.Node]bool)
	for _, request := range requests {
		if e.v4IncludeOwned(name, request.locator) {
			return v4err("config.edit.include-owned", "resources."+name, 0, "Marketplace request %q is declared by an included file", request.locator)
		}
		found := false
		if includes != nil {
			for _, item := range includes.Content {
				if same, _ := v4MarketplaceItemMatches(item, request); same {
					matched[item] = true
					found = true
				}
			}
		}
		if found {
			continue
		}
		return v4err("config.edit.request-missing", "resources."+name, 0, "Marketplace request %q is not present", request.locator)
	}
	if len(matched) == 0 {
		return nil
	}
	retained := make([]*yaml.Node, 0, len(includes.Content))
	for _, item := range includes.Content {
		if !matched[item] {
			retained = append(retained, item)
		}
	}
	includes.Content = retained
	if len(includes.Content) == 0 {
		resources := findMappingValue(e.root, "resources")
		if group.Kind == yaml.SequenceNode {
			removeMappingKeyByName(resources, name)
		} else {
			removeMappingKeyByName(group, "include")
			if len(group.Content) == 0 {
				removeMappingKeyByName(resources, name)
			}
		}
	}
	if resources := findMappingValue(e.root, "resources"); resources != nil && resources.Kind == yaml.MappingNode && len(resources.Content) == 0 {
		removeMappingKeyByName(e.root, "resources")
	}
	e.changed = true
	return nil
}
