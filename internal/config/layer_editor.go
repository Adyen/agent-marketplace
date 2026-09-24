package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/giturl"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

type LayerTarget struct {
	Source        Source
	StagedPath    string
	DeclaringPath string
}

type SourceBinding struct {
	Name  string
	Entry RegistryEntry
	id    string
	root  bool
}

type ResourceLocator struct {
	Kind ir.ResourceKind
	Name string
}

type SelectionOperation uint8

const (
	SelectionInclude SelectionOperation = iota
	SelectionExclude
	SelectionRemoveInclude
	SelectionClearExclude
	SelectionSelectAll
	SelectionExcludeAll
	SelectionAddTag
	SelectionRemoveTag
)

type SelectionChange struct {
	Operation SelectionOperation
	Locators  []ResourceLocator
	Tag       string
}

type LayerCandidate struct {
	Bytes   []byte
	Mode    fs.FileMode
	Changed bool
	Legacy  bool
}

type LayerEditor struct {
	target           LayerTarget
	kind             layerKind
	organizationPath string
	canonical        string
	original         []byte
	mode             fs.FileMode
	doc              *yaml.Node
	root             *yaml.Node
	legacy           bool
	loaded           *V4Config
	changed          bool
	addedV4          map[string]v4EditIdentity
	bindings         map[string]SourceBinding
	legacyNodes      map[string]*yaml.Node
}

func OpenLayerEditor(target LayerTarget) (*LayerEditor, error) {
	if !target.Source.IsWritable() {
		return nil, v4err("config.edit.unsupported-operation", target.DeclaringPath, 0, "configuration source %s is not writable", target.Source)
	}
	if strings.TrimSpace(target.StagedPath) == "" || strings.TrimSpace(target.DeclaringPath) == "" {
		return nil, v4err("config.edit.unsupported-operation", target.DeclaringPath, 0, "staged and declaring paths are required")
	}
	info, err := os.Stat(target.StagedPath)
	if err != nil {
		return nil, fmt.Errorf("stat staged config %q: %w", target.StagedPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("staged config %q is not a regular file", target.StagedPath)
	}
	kind, organizationPath, err := v4LayerForSource(target.Source, target.DeclaringPath)
	if err != nil {
		return nil, err
	}
	data, err := readConfigFile(target.StagedPath)
	if err != nil {
		return nil, fmt.Errorf("read staged config %q: %w", target.StagedPath, err)
	}
	canonical, err := canonicalConfigPath(target.DeclaringPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		absolute, absErr := filepath.Abs(target.DeclaringPath)
		if absErr != nil {
			return nil, absErr
		}
		canonical = filepath.Clean(absolute)
	}
	doc, err := parseConfigDocument(target.StagedPath, data)
	if err != nil {
		return nil, err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	editor := &LayerEditor{
		target:           target,
		kind:             kind,
		organizationPath: organizationPath,
		canonical:        canonical,
		original:         data,
		mode:             info.Mode().Perm(),
		doc:              doc,
		root:             root,
		bindings:         map[string]SourceBinding{},
	}
	if PeekAPIVersion(data) == APIVersionV4 {
		loaded, err := loadV4TreeWithOverlays(target.DeclaringPath, true, map[string]bool{}, nil, data, organizationPath, kind)
		if err != nil {
			return nil, err
		}
		if err := loaded.validateLayerFields(kind); err != nil {
			return nil, err
		}
		editor.loaded = loaded
		return editor, nil
	}
	editor.legacy = true
	if _, err := loadConfigTreeFromRootData(canonical, data, kind); err != nil {
		return nil, err
	}
	return editor, nil
}

func (e *LayerEditor) Render() (LayerCandidate, error) {
	candidate := LayerCandidate{Mode: e.mode, Legacy: e.legacy}
	if !e.changed {
		candidate.Bytes = append([]byte(nil), e.original...)
		return candidate, nil
	}
	data, err := encodeConfigDocument(e.target.DeclaringPath, e.doc)
	if err != nil {
		return LayerCandidate{}, err
	}
	if e.legacy {
		if _, err := loadConfigTreeFromRootData(e.canonical, data, e.kind); err != nil {
			return LayerCandidate{}, err
		}
	} else {
		loaded, err := loadV4TreeWithOverlays(e.target.DeclaringPath, true, map[string]bool{}, nil, data, e.organizationPath, e.kind)
		if err != nil {
			return LayerCandidate{}, err
		}
		if err := loaded.validateLayerFields(e.kind); err != nil {
			return LayerCandidate{}, err
		}
	}
	candidate.Bytes = append([]byte(nil), data...)
	candidate.Changed = true
	return candidate, nil
}

func (e *LayerEditor) EnsureRegistry(entry RegistryEntry) (SourceBinding, error) {
	return e.ensureRegistry(entry, true)
}

func (e *LayerEditor) ensureRegistry(entry RegistryEntry, create bool) (SourceBinding, error) {
	kind, err := e.validateRegistryEntry(entry)
	if err != nil {
		return SourceBinding{}, err
	}
	if e.legacy {
		return e.ensureLegacyRegistry(entry, kind, create)
	}
	return e.ensureV4Registry(entry, kind, create)
}

func (e *LayerEditor) validateRegistryEntry(entry RegistryEntry) (V4SourceKind, error) {
	kind := entry.V4Kind
	if kind == "" {
		if entry.IsLocal() {
			kind = V4SourceDirectory
		} else {
			kind = V4SourceGit
		}
	}
	if kind != V4SourceGit && kind != V4SourceDirectory {
		return "", v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "layer editor supports only git and directory sources")
	}
	if err := ValidateRegistryPath(entry.Path); err != nil {
		return "", v4err("v4.edit-source", "registry.path", 0, "%v", err)
	}
	if err := validateRegistryWrite(e.target.DeclaringPath, entry); err != nil {
		return "", err
	}
	if kind == V4SourceGit {
		if strings.TrimSpace(entry.Ref) == "" {
			return "", v4err("v4.edit-source", "registry.ref", 0, "git sources require an explicit ref")
		}
		if err := giturl.ValidateRef(strings.TrimSpace(entry.Ref)); err != nil {
			return "", v4err("v4.edit-source", "registry.ref", 0, "%v", err)
		}
		if !e.legacy {
			if err := validateV4GitOrigin(strings.TrimSpace(entry.URL)); err != nil {
				return "", v4err("v4.edit-source", "registry.url", 0, "%v", err)
			}
		}
	}
	if harness := strings.TrimSpace(entry.SourceHarness); harness != "" {
		if errs := validateSourceHarness("registry.source_harness", harness); len(errs) > 0 {
			return "", v4err("v4.edit-source", "registry.source_harness", 0, "%v", errs[0])
		}
	}
	return kind, nil
}

func (e *LayerEditor) ensureV4Registry(entry RegistryEntry, kind V4SourceKind, create bool) (SourceBinding, error) {
	identity, err := v4EditSourceIdentity(entry, e.target.DeclaringPath, e.kind, kind)
	if err != nil {
		if kind == V4SourceDirectory {
			return SourceBinding{}, v4err("config.edit.unsafe-directory", e.target.DeclaringPath, 0, "%v", err)
		}
		return SourceBinding{}, err
	}
	var physical, exact, matches []string
	for name, source := range e.loaded.Sources {
		if !v4EditSourcePhysicalMatches(source, identity) {
			continue
		}
		physical = append(physical, name)
		if source.SourceHarness == identity.sourceHarness {
			exact = append(exact, name)
		}
	}
	for name, added := range e.addedV4 {
		if !v4EditIdentityPhysicalEqual(added, identity) {
			continue
		}
		physical = append(physical, name)
		if added.sourceHarness == identity.sourceHarness {
			exact = append(exact, name)
		}
	}
	switch {
	case len(exact) == 1:
		matches = exact
	case len(exact) > 1:
		matches = exact
	case len(physical) == 1:
		matches = physical
	case len(physical) > 1:
		matches = physical
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return SourceBinding{}, v4err("config.edit.ambiguous-source", e.target.DeclaringPath, 0, "registry identity matches multiple sources: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 0 {
		if !create {
			return SourceBinding{}, nil
		}
		occupied := make(map[string]v4MigrationSource, len(e.loaded.Sources)+len(e.addedV4))
		for name := range e.loaded.Sources {
			occupied[name] = v4MigrationSource{}
		}
		for name := range e.addedV4 {
			occupied[name] = v4MigrationSource{}
		}
		name := v4MigrationRegistrySourceName(occupied, entry.URL)
		if err := addV4EditSource(e.root, name, entry, identity, kind); err != nil {
			return SourceBinding{}, err
		}
		if e.addedV4 == nil {
			e.addedV4 = map[string]v4EditIdentity{}
		}
		e.addedV4[name] = identity
		e.changed = true
		matches = []string{name}
	}
	name := matches[0]
	rootOwned := true
	if source, ok := e.loaded.Sources[name]; ok {
		rootOwned = source.File == e.canonical
	}
	binding := SourceBinding{Name: name, Entry: v4BindingEntry(entry, identity, kind), id: fmt.Sprintf("v4:%p:%s", e, name), root: rootOwned}
	e.bindings[binding.id] = binding
	return binding, nil
}

func v4BindingEntry(entry RegistryEntry, identity v4EditIdentity, kind V4SourceKind) RegistryEntry {
	if kind == V4SourceDirectory {
		return RegistryEntry{URL: identity.url, V4Kind: V4SourceDirectory, SourceHarness: strings.TrimSpace(entry.SourceHarness)}
	}
	return RegistryEntry{URL: strings.TrimSpace(entry.URL), Ref: strings.TrimSpace(entry.Ref), Path: NormalizeRegistryPath(entry.Path), SourceHarness: strings.TrimSpace(entry.SourceHarness), V4Kind: V4SourceGit}
}

func (e *LayerEditor) ensureLegacyRegistry(entry RegistryEntry, kind V4SourceKind, create bool) (SourceBinding, error) {
	bound := RegistryEntry{URL: strings.TrimSpace(entry.URL), Ref: strings.TrimSpace(entry.Ref), Path: NormalizeRegistryPath(entry.Path), SourceHarness: strings.TrimSpace(entry.SourceHarness), V4Kind: kind}
	if kind == V4SourceDirectory {
		localPath, err := entry.ResolveLocalPath(legacyLocalBase(e.target.DeclaringPath, e.kind))
		if err != nil {
			return SourceBinding{}, v4err("config.edit.unsafe-directory", e.target.DeclaringPath, 0, "%v", err)
		}
		bound.URL = entry.BundlePath(localPath)
	}
	_, seq := findTopLevelSequence(e.root, "registries")
	if seq != nil && seq.Kind == yaml.SequenceNode {
		var physical, exact []*yaml.Node
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode || !legacyEntryNodePhysicalMatches(item, entry) {
				continue
			}
			physical = append(physical, item)
			if legacyEntryNodeHarness(item) == strings.TrimSpace(entry.SourceHarness) {
				exact = append(exact, item)
			}
		}
		var item *yaml.Node
		switch {
		case len(exact) == 1:
			item = exact[0]
		case len(exact) > 1 || len(physical) > 1:
			return SourceBinding{}, v4err("config.edit.ambiguous-source", e.target.DeclaringPath, 0, "registry identity matches multiple occurrences")
		case len(physical) == 1:
			item = physical[0]
		}
		if item != nil {
			return e.issueLegacyBinding(item, bound), nil
		}
	}
	if !create {
		return SourceBinding{}, nil
	}
	registries, err := ensureTopLevelSequence(e.root, "registries")
	if err != nil {
		return SourceBinding{}, fmt.Errorf("update config %q: %w", e.target.DeclaringPath, err)
	}
	item := legacySourceEntryNode(entry)
	registries.Content = append(registries.Content, item)
	e.changed = true
	return e.issueLegacyBinding(item, bound), nil
}

func (e *LayerEditor) issueLegacyBinding(item *yaml.Node, bound RegistryEntry) SourceBinding {
	id := fmt.Sprintf("legacy:%p", item)
	binding := SourceBinding{Entry: bound, id: id, root: true}
	e.bindings[id] = binding
	if e.legacyNodes == nil {
		e.legacyNodes = map[string]*yaml.Node{}
	}
	e.legacyNodes[id] = item
	return binding
}

func legacyEntryNodePhysicalMatches(item *yaml.Node, entry RegistryEntry) bool {
	urlNode := findRegistryURLNode(item)
	if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(strings.TrimSpace(entry.URL)) {
		return false
	}
	if strings.TrimSpace(entry.Path) != "" && NormalizeRegistryPath(yamlEntryPath(item)) != NormalizeRegistryPath(entry.Path) {
		return false
	}
	if strings.TrimSpace(entry.Ref) != "" {
		refNode := findMappingValue(item, "ref")
		if refNode == nil || strings.TrimSpace(refNode.Value) != strings.TrimSpace(entry.Ref) {
			return false
		}
	}
	return true
}

func legacyEntryNodeHarness(item *yaml.Node) string {
	if harnessNode := findMappingValue(item, "source_harness"); harnessNode != nil {
		return strings.TrimSpace(harnessNode.Value)
	}
	return ""
}

func legacySourceEntryNode(entry RegistryEntry) *yaml.Node {
	str := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	}
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	item.Content = append(item.Content, str("url"), str(strings.TrimSpace(entry.URL)))
	if ref := strings.TrimSpace(entry.Ref); ref != "" {
		item.Content = append(item.Content, str("ref"), str(ref))
	}
	if p := strings.TrimSpace(entry.Path); p != "" {
		item.Content = append(item.Content, str("path"), str(p))
	}
	if h := strings.TrimSpace(entry.SourceHarness); h != "" {
		item.Content = append(item.Content, str("source_harness"), str(h))
	}
	if len(entry.Harnesses) > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, h := range entry.Harnesses {
			seq.Content = append(seq.Content, str(h))
		}
		item.Content = append(item.Content, str("harnesses"), seq)
	}
	return item
}

func (e *LayerEditor) Apply(binding SourceBinding, change SelectionChange) error {
	if issued, ok := e.bindings[binding.id]; binding.id == "" || !ok || issued.Name != binding.Name {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source binding was not issued by this editor")
	}
	op := change.Operation
	locatorOp := op == SelectionInclude || op == SelectionExclude || op == SelectionRemoveInclude || op == SelectionClearExclude
	tagOp := op == SelectionAddTag || op == SelectionRemoveTag
	switch {
	case locatorOp && (change.Tag != "" || len(change.Locators) == 0):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation requires one or more locators and no tag")
	case (op == SelectionSelectAll || op == SelectionExcludeAll) && (len(change.Locators) > 0 || change.Tag != ""):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation accepts no locators or tag")
	case tagOp && (len(change.Locators) > 0 || strings.TrimSpace(change.Tag) == "" || strings.TrimSpace(change.Tag) != change.Tag):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation requires one trimmed tag and no locators")
	case op > SelectionRemoveTag:
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "unsupported selection operation")
	}
	locators, err := e.validateLocators(binding, change.Locators)
	if err != nil {
		return err
	}
	if e.legacy {
		return e.applyLegacy(binding, op, locators, change.Tag)
	}
	return e.applyV4(binding, op, locators, change.Tag)
}

func (e *LayerEditor) validateLocators(binding SourceBinding, input []ResourceLocator) ([]ResourceLocator, error) {
	seen := map[string]bool{}
	out := make([]ResourceLocator, 0, len(input))
	for _, l := range input {
		key := v4ResourceKindName(l.Kind) + "/" + l.Name
		if e.legacy {
			if skill.ConfigKeyForKind(l.Kind) == "" || strings.TrimSpace(l.Name) == "" {
				return nil, v4err("v4.locator", "registries", 0, "invalid resource locator %q", key)
			}
		} else if _, _, _, err := parseV4Locator(key, binding.Name); err != nil {
			return nil, v4err("v4.locator", "resources."+binding.Name, 0, "%v", err)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out, nil
}

func v4Locator(l ResourceLocator) string {
	return v4ResourceKindName(l.Kind) + "/" + l.Name
}

func v4GroupSeqs(group *yaml.Node) (include, exclude *yaml.Node) {
	if group == nil {
		return nil, nil
	}
	if group.Kind == yaml.SequenceNode {
		return group, nil
	}
	if group.Kind == yaml.MappingNode {
		return findMappingValue(group, "include"), findMappingValue(group, "exclude")
	}
	return nil, nil
}

func (e *LayerEditor) ensureV4Group(name string) (*yaml.Node, error) {
	resources, err := ensureV4EditMapping(e.root, "resources")
	if err != nil {
		return nil, err
	}
	group, _, err := ensureV4EditGroup(resources, name)
	return group, err
}

func (e *LayerEditor) ensureV4GroupSeq(group *yaml.Node, name, key string) (*yaml.Node, error) {
	seq := findMappingValue(group, key)
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(group, key, seq)
		return seq, nil
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, v4err("v4.type", "resources."+name+"."+key, seq.Line, "must be a list")
	}
	return seq, nil
}

func (e *LayerEditor) v4InheritedSuppression(name, locator string) bool {
	group, ok := e.loaded.Groups[name]
	if !ok {
		return false
	}
	for _, d := range group.Decisions {
		if d.Action != V4SelectionExclude || d.Exclusion == locator || d.Provenance.File == e.canonical {
			continue
		}
		if v4ExclusionMatches(d.Exclusion, locator) {
			return true
		}
	}
	return false
}

func v4ScalarAppend(seq *yaml.Node, value string) {
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func (e *LayerEditor) applyV4(binding SourceBinding, op SelectionOperation, locators []ResourceLocator, tag string) error {
	name := binding.Name
	var group *yaml.Node
	if resources := findMappingValue(e.root, "resources"); resources != nil && resources.Kind == yaml.MappingNode {
		group = findMappingValue(resources, name)
	}
	switch op {
	case SelectionInclude:
		return e.v4ApplyInclude(name, group, locators)
	case SelectionExclude:
		return e.v4ApplyExclude(name, group, locators)
	case SelectionRemoveInclude:
		include, _ := v4GroupSeqs(group)
		if include == nil {
			return nil
		}
		changed := false
		for _, l := range locators {
			if v4EditRemoveLocator(include, v4Locator(l)) {
				changed = true
			}
		}
		if changed {
			e.changed = true
		}
		return nil
	case SelectionClearExclude:
		return e.v4ApplyClearExclude(name, group, locators)
	case SelectionSelectAll:
		return e.v4ApplySelectAll(name, group)
	case SelectionExcludeAll:
		return e.v4ApplyExcludeAll(name, group)
	case SelectionAddTag:
		return e.v4ApplyTag(name, group, tag, true)
	case SelectionRemoveTag:
		return e.v4ApplyTag(name, group, tag, false)
	}
	return nil
}

func (e *LayerEditor) v4ApplyInclude(name string, group *yaml.Node, locators []ResourceLocator) error {
	include, exclude := v4GroupSeqs(group)
	for _, l := range locators {
		locator := v4Locator(l)
		if exclude != nil && v4EditSequenceHasWildcard(exclude, l.Kind, l.Name) {
			return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "wildcard exclusion suppresses %q", locator)
		}
		if e.v4InheritedSuppression(name, locator) {
			return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "an inherited exclusion suppresses %q", locator)
		}
	}
	var adds, removals []string
	for _, l := range locators {
		locator := v4Locator(l)
		if include == nil || !v4EditSequenceContains(include, locator) {
			adds = append(adds, locator)
		}
		if exclude != nil && v4EditSequenceContains(exclude, locator) {
			removals = append(removals, locator)
		}
	}
	if len(adds) == 0 && len(removals) == 0 {
		return nil
	}
	if group != nil && group.Kind == yaml.SequenceNode && len(removals) == 0 {
		for _, locator := range adds {
			v4ScalarAppend(group, locator)
		}
		e.changed = true
		return nil
	}
	group, err := e.ensureV4Group(name)
	if err != nil {
		return err
	}
	if len(removals) > 0 {
		if exc := findMappingValue(group, "exclude"); exc != nil {
			for _, locator := range removals {
				v4EditRemoveLocator(exc, locator)
			}
		}
	}
	if len(adds) > 0 {
		inc, err := e.ensureV4GroupSeq(group, name, "include")
		if err != nil {
			return err
		}
		for _, locator := range adds {
			v4ScalarAppend(inc, locator)
		}
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) v4ApplyExclude(name string, group *yaml.Node, locators []ResourceLocator) error {
	include, exclude := v4GroupSeqs(group)
	var removals, adds []string
	for _, l := range locators {
		locator := v4Locator(l)
		if include != nil && v4EditSequenceContains(include, locator) {
			removals = append(removals, locator)
		}
		if exclude == nil || !v4EditSequenceContains(exclude, locator) {
			adds = append(adds, locator)
		}
	}
	if len(removals) == 0 && len(adds) == 0 {
		return nil
	}
	group, err := e.ensureV4Group(name)
	if err != nil {
		return err
	}
	if len(removals) > 0 {
		if inc := findMappingValue(group, "include"); inc != nil {
			for _, locator := range removals {
				v4EditRemoveLocator(inc, locator)
			}
		}
	}
	if len(adds) > 0 {
		exc, err := e.ensureV4GroupSeq(group, name, "exclude")
		if err != nil {
			return err
		}
		for _, locator := range adds {
			v4ScalarAppend(exc, locator)
		}
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) v4ApplyClearExclude(name string, group *yaml.Node, locators []ResourceLocator) error {
	_, exclude := v4GroupSeqs(group)
	loadedGroup := e.loaded.Groups[name]
	var removals []string
	for _, l := range locators {
		locator := v4Locator(l)
		if exclude != nil && v4EditSequenceContains(exclude, locator) {
			if v4EditSequenceHasWildcard(exclude, l.Kind, l.Name) || v4EditLoadedSelectionSuppressed(loadedGroup, locator, e.target.DeclaringPath, true) {
				return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "an inherited or wildcard exclusion still suppresses %q", locator)
			}
			removals = append(removals, locator)
			continue
		}
		if v4EditLoadedSelectionSuppressed(loadedGroup, locator, e.target.DeclaringPath, false) {
			return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "an inherited or wildcard exclusion still suppresses %q", locator)
		}
	}
	for _, locator := range removals {
		v4EditRemoveLocator(exclude, locator)
	}
	if len(removals) > 0 {
		e.changed = true
	}
	return nil
}

func (e *LayerEditor) v4ApplySelectAll(name string, group *yaml.Node) error {
	_, exclude := v4GroupSeqs(group)
	star := exclude != nil && v4EditSequenceContains(exclude, "*")
	if group != nil && group.Kind == yaml.MappingNode && boolMappingValue(group, "all") && !star {
		return nil
	}
	group, err := e.ensureV4Group(name)
	if err != nil {
		return err
	}
	setMappingValue(group, "all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	if exc := findMappingValue(group, "exclude"); exc != nil {
		v4EditRemoveLocator(exc, "*")
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) v4ApplyExcludeAll(name string, group *yaml.Node) error {
	_, exclude := v4GroupSeqs(group)
	star := exclude != nil && v4EditSequenceContains(exclude, "*")
	hasSelection := group != nil && (group.Kind == yaml.SequenceNode || findMappingValue(group, "all") != nil || findMappingValue(group, "tags") != nil || findMappingValue(group, "include") != nil)
	if !hasSelection && star {
		return nil
	}
	resources, err := ensureV4EditMapping(e.root, "resources")
	if err != nil {
		return err
	}
	if group != nil && group.Kind == yaml.SequenceNode {
		replacement := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(replacement, "exclude", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "*"}}})
		for i := 0; i+1 < len(resources.Content); i += 2 {
			if resources.Content[i].Value == name {
				resources.Content[i+1] = replacement
				break
			}
		}
		e.changed = true
		return nil
	}
	if group == nil {
		group, _, err = ensureV4EditGroup(resources, name)
		if err != nil {
			return err
		}
	}
	removeMappingKeyByName(group, "all")
	removeMappingKeyByName(group, "tags")
	removeMappingKeyByName(group, "include")
	if !star {
		exc, err := e.ensureV4GroupSeq(group, name, "exclude")
		if err != nil {
			return err
		}
		v4ScalarAppend(exc, "*")
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) v4ApplyTag(name string, group *yaml.Node, tag string, add bool) error {
	var tags *yaml.Node
	if group != nil && group.Kind == yaml.MappingNode {
		tags = findMappingValue(group, "tags")
	}
	if add {
		if tags != nil && v4EditSequenceContains(tags, tag) {
			return nil
		}
		group, err := e.ensureV4Group(name)
		if err != nil {
			return err
		}
		seq, err := e.ensureV4GroupSeq(group, name, "tags")
		if err != nil {
			return err
		}
		v4ScalarAppend(seq, tag)
		e.changed = true
		return nil
	}
	if tags == nil || !v4EditRemoveLocator(tags, tag) {
		return nil
	}
	if len(tags.Content) == 0 {
		removeMappingKeyByName(group, "tags")
	}
	e.changed = true
	return nil
}

func (e *LayerEditor) applyLegacy(binding SourceBinding, op SelectionOperation, locators []ResourceLocator, tag string) error {
	entryNode := e.legacyNodes[binding.id]
	if entryNode == nil {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source binding was not issued by this editor")
	}
	changed := false
	for _, l := range locators {
		key := skill.ConfigKeyForKind(l.Kind)
		switch op {
		case SelectionInclude:
			if kindMap := findMappingValue(entryNode, key); kindMap != nil && kindMap.Kind == yaml.MappingNode {
				if _, exc := findEntrySequence(kindMap, "exclude"); exc != nil {
					changed = legacySeqRemove(exc, l.Name) || changed
				}
			}
			if entryNodeAutoSelectsKind(entryNode, l.Kind) {
				continue
			}
			kindMap := ensureEntryMapping(entryNode, key)
			if kindMap == nil {
				return fmt.Errorf("update config %q: registry has invalid %q selector type", e.target.DeclaringPath, key)
			}
			inc, err := ensureMappingSequence(kindMap, "include")
			if err != nil {
				return fmt.Errorf("update config %q: %w", e.target.DeclaringPath, err)
			}
			changed = legacySeqAppend(inc, l.Name) || changed
			if boolMappingValue(entryNode, "exclude_all") || boolMappingValue(kindMap, "exclude_all") {
				clearEntryExcludeAll(entryNode, key)
				changed = true
			}
		case SelectionExclude:
			kindMap := ensureEntryMapping(entryNode, key)
			if kindMap == nil {
				return fmt.Errorf("update config %q: registry has invalid %q selector type", e.target.DeclaringPath, key)
			}
			if _, inc := findEntrySequence(kindMap, "include"); inc != nil {
				changed = legacySeqRemove(inc, l.Name) || changed
			}
			exc, err := ensureMappingSequence(kindMap, "exclude")
			if err != nil {
				return fmt.Errorf("update config %q: %w", e.target.DeclaringPath, err)
			}
			changed = legacySeqAppend(exc, l.Name) || changed
		case SelectionRemoveInclude:
			changed = legacyKindSeqRemove(entryNode, key, "include", l.Name) || changed
		case SelectionClearExclude:
			changed = legacyKindSeqRemove(entryNode, key, "exclude", l.Name) || changed
		}
	}
	switch op {
	case SelectionSelectAll:
		if !boolMappingValue(entryNode, "default") {
			setMappingValue(entryNode, "default", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			changed = true
		}
		if findMappingValue(entryNode, "exclude_all") != nil {
			removeMappingKeyByName(entryNode, "exclude_all")
			changed = true
		}
	case SelectionExcludeAll:
		changed = clearEntrySelectionsForExcludeAll(entryNode) || changed
		changed = removeMappingKeyByName(entryNode, "tags") || changed
		if !boolMappingValue(entryNode, "exclude_all") {
			setMappingValue(entryNode, "exclude_all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			changed = true
		}
	case SelectionAddTag:
		seq := findMappingValue(entryNode, "tags")
		if seq == nil {
			seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setMappingValue(entryNode, "tags", seq)
		}
		if seq.Kind != yaml.SequenceNode {
			return fmt.Errorf("update config %q: expected %q to be a sequence", e.target.DeclaringPath, "tags")
		}
		changed = legacySeqAppend(seq, tag) || changed
	case SelectionRemoveTag:
		if seq := findMappingValue(entryNode, "tags"); seq != nil && seq.Kind == yaml.SequenceNode {
			if legacySeqRemove(seq, tag) {
				changed = true
				if len(seq.Content) == 0 {
					removeMappingKeyByName(entryNode, "tags")
				}
			}
		}
	}
	if changed {
		e.changed = true
	}
	return nil
}

func legacyKindSeqRemove(entryNode *yaml.Node, kindKey, subKey, value string) bool {
	kindMap := findMappingValue(entryNode, kindKey)
	if kindMap == nil {
		return false
	}
	if kindMap.Kind == yaml.SequenceNode {
		if subKey == "include" {
			return legacySeqRemove(kindMap, value)
		}
		return false
	}
	if kindMap.Kind != yaml.MappingNode {
		return false
	}
	_, seq := findEntrySequence(kindMap, subKey)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	return legacySeqRemove(seq, value)
}

func legacySeqRemove(seq *yaml.Node, value string) bool {
	for i, item := range seq.Content {
		if item.Kind == yaml.ScalarNode && strings.TrimSpace(item.Value) == value {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return true
		}
	}
	return false
}

func legacySeqAppend(seq *yaml.Node, value string) bool {
	for _, item := range seq.Content {
		if item.Kind == yaml.ScalarNode && strings.TrimSpace(item.Value) == value {
			return false
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	return true
}

func (e *LayerEditor) RemoveSource(binding SourceBinding) error {
	if issued, ok := e.bindings[binding.id]; binding.id == "" || !ok || issued.Name != binding.Name {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source binding was not issued by this editor")
	}
	if !binding.root {
		return v4err("config.edit.include-owned", e.target.DeclaringPath, 0, "source %q is declared in an included fragment", binding.Name)
	}
	if e.legacy {
		keyIndex, seq := findTopLevelSequence(e.root, "registries")
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return nil
		}
		target := e.legacyNodes[binding.id]
		for i, item := range seq.Content {
			if item != target {
				continue
			}
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			e.changed = true
			break
		}
		if e.changed && len(seq.Content) == 0 && keyIndex >= 0 {
			removeTopLevelKey(e.root, keyIndex)
		}
		return nil
	}
	changed := false
	if sources := findMappingValue(e.root, "sources"); sources != nil && sources.Kind == yaml.MappingNode {
		changed = removeMappingKeyByName(sources, binding.Name) || changed
	}
	if resources := findMappingValue(e.root, "resources"); resources != nil && resources.Kind == yaml.MappingNode {
		changed = removeMappingKeyByName(resources, binding.Name) || changed
	}
	delete(e.addedV4, binding.Name)
	if changed {
		e.changed = true
	}
	return nil
}
