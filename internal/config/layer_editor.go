package config

import (
	"bytes"
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
	loaded           *V4Config
	changed          bool
	addedV4          map[string]v4EditIdentity
	bindings         map[string]SourceBinding
	rendered         *V4Config
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
	if data == nil {
		data = []byte{}
	}
	canonical, err := canonicalConfigPathTolerateMissing(target.DeclaringPath)
	if err != nil {
		return nil, err
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

func (e *LayerEditor) Render() (LayerCandidate, error) {
	candidate := LayerCandidate{Mode: e.mode}
	if !e.changed {
		candidate.Bytes = append([]byte(nil), e.original...)
		return candidate, nil
	}
	data, err := encodeConfigDocument(e.target.DeclaringPath, e.doc)
	if err != nil {
		return LayerCandidate{}, err
	}
	loaded, err := loadV4TreeWithOverlays(e.target.DeclaringPath, true, map[string]bool{}, nil, data, e.organizationPath, e.kind)
	if err != nil {
		return LayerCandidate{}, err
	}
	if err := loaded.validateLayerFields(e.kind); err != nil {
		return LayerCandidate{}, err
	}
	e.rendered = loaded
	candidate.Bytes = append([]byte(nil), data...)
	candidate.Changed = !bytes.Equal(data, e.original)
	return candidate, nil
}

func (e *LayerEditor) RenderedV4() *V4Config {
	if e.rendered != nil {
		return e.rendered
	}
	return e.loaded
}

func (e *LayerEditor) EnsureRegistry(entry RegistryEntry) (SourceBinding, error) {
	return e.ensureRegistry(entry, true)
}

func (e *LayerEditor) LookupRegistry(entry RegistryEntry) (SourceBinding, bool, error) {
	binding, err := e.ensureRegistry(entry, false)
	if err != nil {
		return SourceBinding{}, false, err
	}
	return binding, binding.id != "", nil
}

func registryEntryKind(entry RegistryEntry) V4SourceKind {
	if entry.V4Kind != "" {
		return entry.V4Kind
	}
	if entry.IsLocal() {
		return V4SourceDirectory
	}
	return V4SourceGit
}

func (e *LayerEditor) occupiedSourceNames() map[string]struct{} {
	occupied := make(map[string]struct{}, len(e.loaded.Sources)+len(e.addedV4))
	for name := range e.loaded.Sources {
		occupied[name] = struct{}{}
	}
	for name := range e.addedV4 {
		occupied[name] = struct{}{}
	}
	return occupied
}

func (e *LayerEditor) checkBinding(binding SourceBinding) error {
	if issued, ok := e.bindings[binding.id]; binding.id == "" || !ok || issued.Name != binding.Name {
		return v4err("config.edit.invalid-binding", e.target.DeclaringPath, 0, "source selection was not created for this configuration editor")
	}
	return nil
}

func (e *LayerEditor) normalizeRegistryEntry(entry RegistryEntry, create bool) (RegistryEntry, V4SourceKind, error) {
	if registryEntryKind(entry) == V4SourceGit {
		normalized, err := v4GitOriginForEdit(entry.URL, e.target.DeclaringPath, e.kind)
		if err != nil {
			return RegistryEntry{}, "", err
		}
		entry.URL = normalized
	}
	kind, err := e.validateRegistryEntry(entry, create)
	if err != nil {
		return RegistryEntry{}, "", err
	}
	return entry, kind, nil
}

func (e *LayerEditor) ensureRegistry(entry RegistryEntry, create bool) (SourceBinding, error) {
	entry, kind, err := e.normalizeRegistryEntry(entry, create)
	if err != nil {
		return SourceBinding{}, err
	}
	return e.ensureV4Registry(entry, kind, create)
}

func (e *LayerEditor) validateRegistryEntry(entry RegistryEntry, create bool) (V4SourceKind, error) {
	kind := registryEntryKind(entry)
	if kind != V4SourceGit && kind != V4SourceDirectory {
		return "", v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "layer editor supports only git and directory sources")
	}
	if err := ValidateRegistryPath(entry.Path); err != nil {
		return "", v4err("v4.edit-source", "registry.path", 0, "%v", err)
	}
	if create {
		if err := validateRegistryWrite(e.target.DeclaringPath, entry); err != nil {
			return "", err
		}
	}
	if kind == V4SourceGit {
		if strings.TrimSpace(entry.Ref) == "" {
			return "", v4err("v4.edit-source", "registry.ref", 0, "git sources require an explicit ref")
		}
		if err := giturl.ValidateRef(strings.TrimSpace(entry.Ref)); err != nil {
			return "", v4err("v4.edit-source", "registry.ref", 0, "%v", err)
		}
		if err := validateV4GitOrigin(strings.TrimSpace(entry.URL)); err != nil {
			return "", v4err("v4.edit-source", "registry.url", 0, "%v", err)
		}
	}
	if harness := strings.TrimSpace(entry.SourceHarness); harness != "" {
		if errs := validateSourceHarness("registry.source_harness", harness); len(errs) > 0 {
			return "", v4err("v4.edit-source", "registry.source_harness", 0, "%v", errs[0])
		}
	}
	return kind, nil
}

func v4GitOriginForEdit(raw, declaringPath string, kind layerKind) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if validateV4GitOrigin(trimmed) == nil {
		return trimmed, nil
	}
	local := strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "~")
	if !local {
		return trimmed, nil
	}
	expanded := trimmed
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", v4err("v4.edit-source", "registry.url", 0, "%v", err)
		}
		expanded = filepath.Join(home, expanded[2:])
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(layerDirectoryBase(declaringPath, kind), expanded)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", v4err("v4.edit-source", "registry.url", 0, "%v", err)
	}
	return "file://" + filepath.ToSlash(absolute), nil
}

func (e *LayerEditor) ensureV4Registry(entry RegistryEntry, kind V4SourceKind, create bool) (SourceBinding, error) {
	identity, err := v4EditSourceIdentity(entry, e.target.DeclaringPath, e.kind, kind)
	if err != nil {
		if kind == V4SourceDirectory {
			return SourceBinding{}, v4err("config.edit.unsafe-directory", e.target.DeclaringPath, 0, "%v", err)
		}
		return SourceBinding{}, err
	}
	var physical, exact []string
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
	matches := physical
	if len(exact) > 0 {
		matches = exact
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return SourceBinding{}, v4err("config.edit.ambiguous-source", e.target.DeclaringPath, 0, "registry identity matches multiple sources: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 0 {
		if !create {
			return SourceBinding{}, nil
		}
		name := registrySourceName(e.occupiedSourceNames(), entry.URL)
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

func (e *LayerEditor) Apply(binding SourceBinding, change SelectionChange) error {
	if err := e.checkBinding(binding); err != nil {
		return err
	}
	op := change.Operation
	locatorOp := op == SelectionInclude || op == SelectionExclude || op == SelectionRemoveInclude || op == SelectionClearExclude
	tagOp := op == SelectionAddTag
	switch {
	case locatorOp && (change.Tag != "" || len(change.Locators) == 0):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation requires one or more locators and no tag")
	case (op == SelectionSelectAll || op == SelectionExcludeAll) && (len(change.Locators) > 0 || change.Tag != ""):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation accepts no locators or tag")
	case tagOp && (len(change.Locators) > 0 || strings.TrimSpace(change.Tag) == "" || strings.TrimSpace(change.Tag) != change.Tag):
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "operation requires one trimmed tag and no locators")
	case op > SelectionAddTag:
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "unsupported selection operation")
	}
	locators, err := e.validateLocators(binding, change.Locators)
	if err != nil {
		return err
	}
	return e.applyV4(binding, op, locators, change.Tag)
}

func (e *LayerEditor) ApplyRootOwned(binding SourceBinding, change SelectionChange) error {
	if err := e.checkBinding(binding); err != nil {
		return err
	}
	if !binding.root {
		return v4err("config.edit.include-owned", e.target.DeclaringPath, 0, "source %q is declared in an included fragment", binding.Name)
	}
	for _, locator := range change.Locators {
		if e.v4IncludeOwned(binding.Name, v4Locator(locator)) {
			return v4err("config.edit.include-owned", e.target.DeclaringPath, 0, "resource %q is requested by an included fragment", v4Locator(locator))
		}
	}
	return e.Apply(binding, change)
}

func (e *LayerEditor) validateLocators(binding SourceBinding, input []ResourceLocator) ([]ResourceLocator, error) {
	seen := map[string]bool{}
	out := make([]ResourceLocator, 0, len(input))
	for _, l := range input {
		key := v4ResourceKindName(l.Kind) + "/" + l.Name
		if _, _, _, err := parseV4Locator(key, binding.Name); err != nil {
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

func (e *LayerEditor) v4IncludeOwned(name, locator string) bool {
	group, ok := e.loaded.Groups[name]
	if !ok {
		return false
	}
	for _, req := range group.Include {
		if req.Locator != locator {
			continue
		}
		for _, prov := range req.Provenance {
			if prov.File != e.canonical {
				return true
			}
		}
	}
	return false
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
		for _, l := range locators {
			if e.v4IncludeOwned(name, v4Locator(l)) {
				return v4err("config.edit.include-owned", e.target.DeclaringPath, 0, "resource %q is requested by an included fragment", v4Locator(l))
			}
		}
		changed := false
		for _, l := range locators {
			if include != nil && v4EditRemoveLocator(include, v4Locator(l)) {
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
		return e.v4ApplyTag(name, group, tag)
	}
	return nil
}

func (e *LayerEditor) v4ApplyInclude(name string, group *yaml.Node, locators []ResourceLocator) error {
	include, exclude := v4GroupSeqs(group)
	clearWildcard := exclude != nil && v4EditSequenceContains(exclude, "*")
	for _, l := range locators {
		locator := v4Locator(l)
		if exclude != nil {
			for _, item := range exclude.Content {
				if item.Kind != yaml.ScalarNode || item.Value == "*" || item.Value == locator {
					continue
				}
				if v4ExclusionMatches(item.Value, locator) {
					return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "wildcard exclusion suppresses %q", locator)
				}
			}
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
	if len(adds) == 0 && len(removals) == 0 && !clearWildcard {
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
	if len(removals) > 0 || clearWildcard {
		if exc := findMappingValue(group, "exclude"); exc != nil {
			if clearWildcard {
				v4EditRemoveLocator(exc, "*")
			}
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
			if v4EditSequenceHasWildcard(exclude, l.Kind, l.Name) || v4EditLoadedSelectionSuppressed(loadedGroup, locator, e.canonical, true) {
				return v4err("v4.edit-exclusion", "resources."+name+".exclude", 0, "an inherited or wildcard exclusion still suppresses %q", locator)
			}
			removals = append(removals, locator)
			continue
		}
		if exclude != nil && v4EditSequenceHasWildcard(exclude, l.Kind, l.Name) || v4EditLoadedSelectionSuppressed(loadedGroup, locator, e.canonical, true) {
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

func (e *LayerEditor) v4ApplyTag(name string, group *yaml.Node, tag string) error {
	if group != nil && group.Kind == yaml.MappingNode {
		if tags := findMappingValue(group, "tags"); tags != nil && v4EditSequenceContains(tags, tag) {
			return nil
		}
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

func (e *LayerEditor) ClearInstallExclude(entry RegistryEntry, locator ResourceLocator) error {
	binding, found, err := e.LookupRegistry(entry)
	if err != nil || !found {
		return err
	}
	return e.Apply(binding, SelectionChange{Operation: SelectionClearExclude, Locators: []ResourceLocator{locator}})
}

func (e *LayerEditor) SetLayerExclude(locator ResourceLocator, entries []RegistryEntry) error {
	key := skill.ConfigKeyForKind(locator.Kind)
	name := strings.TrimSpace(locator.Name)
	if key == "" || name == "" {
		return v4err("v4.locator", "registries", 0, "invalid resource locator %q", v4ResourceKindName(locator.Kind)+"/"+locator.Name)
	}
	if len(entries) == 0 {
		return v4err("config.edit.unsupported-operation", e.target.DeclaringPath, 0, "v4 has no source-free selector for %q", v4ResourceKindName(locator.Kind)+"/"+name)
	}
	// Validate every provider before touching the document: one invalid
	// entry must not leave the earlier ones half-excluded.
	for _, entry := range entries {
		if _, _, err := e.normalizeRegistryEntry(entry, true); err != nil {
			return err
		}
	}
	bound := map[string]bool{}
	for _, entry := range entries {
		binding, err := e.ensureRegistry(entry, true)
		if err != nil {
			return err
		}
		if bound[binding.Name] {
			continue
		}
		bound[binding.Name] = true
		if err := e.Apply(binding, SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{{Kind: locator.Kind, Name: name}}}); err != nil {
			return err
		}
	}
	return nil
}

func (e *LayerEditor) RemoveSource(binding SourceBinding) error {
	if err := e.checkBinding(binding); err != nil {
		return err
	}
	if !binding.root {
		return v4err("config.edit.include-owned", e.target.DeclaringPath, 0, "source %q is declared in an included fragment", binding.Name)
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
