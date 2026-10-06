package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// sourceToggle is one whole-source change, captured when the key is pressed
// so a reload cannot change what is written. A Git or directory source is
// changed in the local config through entry and edits; an organization
// Marketplace source through the per-resource user overrides in marketplace.
type sourceToggle struct {
	entry       config.RegistryEntry
	label       string
	exclude     bool
	edits       []config.SourceExclusionEdit
	marketplace []toggleAction
	resources   int
}

// localExcludeAllEntries returns the local config's registry entries that
// exclude every resource of their source.
func localExcludeAllEntries(cfg *config.Config) []*config.RegistryEntry {
	if cfg == nil {
		return nil
	}
	var out []*config.RegistryEntry
	for i := range cfg.Registries {
		entry := &cfg.Registries[i]
		if entry.Source == config.RepoPersonal && !entry.Required && entryExcludesAllKinds(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func entryExcludesAllKinds(entry *config.RegistryEntry) bool {
	if entry.ExcludeAll {
		return true
	}
	for _, kind := range config.AllKinds {
		if !entry.SelectorForKind(kind).ExcludeAll {
			return false
		}
	}
	return true
}

// sourceExcludedBy reports whether a local exclude-all entry hides the
// artifact. Required and Marketplace resources ignore it, and local config
// cannot hide resources of organization or user sources.
func sourceExcludedBy(s resolver.ResolvedArtifact, entries []*config.RegistryEntry) bool {
	if s.Required || s.IsMarketplace || s.Source.IsMachineScope() {
		return false
	}
	for _, entry := range entries {
		if resolver.WholeSourceExclusionMatches(*entry, s) {
			return true
		}
	}
	return false
}

// sourceMatcher recognizes the rows of one Git or directory source. An empty
// ref on either side matches, because rows attributed to a ref-less local
// entry still belong to the configured source.
type sourceMatcher struct {
	rawURL, url, path, ref string
	sourceHarness          string
}

func newSourceMatcher(r *row) sourceMatcher {
	return sourceMatcher{
		rawURL:        r.RegistryURL,
		url:           config.NormalizeGitURL(strings.TrimSpace(r.RegistryURL)),
		path:          config.NormalizeRegistryPath(r.RegistryPath),
		ref:           strings.TrimSpace(r.RegistryRef),
		sourceHarness: strings.TrimSpace(r.Artifact.SourceHarness),
	}
}

func (s sourceMatcher) matches(r *row) bool {
	if r.RegistryURL != s.rawURL && config.NormalizeGitURL(strings.TrimSpace(r.RegistryURL)) != s.url {
		return false
	}
	if config.NormalizeRegistryPath(r.RegistryPath) != s.path {
		return false
	}
	if strings.TrimSpace(r.Artifact.SourceHarness) != s.sourceHarness {
		return false
	}
	ref := strings.TrimSpace(r.RegistryRef)
	return s.ref == "" || ref == "" || s.ref == ref
}

// sourceTargetAtCursor returns a row of the source of the selected resource
// row or registry group. Otherwise it returns a message explaining why no
// source applies; ok is false and the message empty when the cursor is on
// nothing.
func (m Model) sourceTargetAtCursor() (target row, refusal string, ok bool) {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return row{}, "", false
	}
	item := m.flatItems[m.cursor]
	switch {
	case item.Kind == flatArtifact:
		target = item.Row
	case len(item.SectionRows) > 0:
		if target, refusal = sectionSourceRow(item); refusal != "" {
			return row{}, refusal, false
		}
	default:
		return row{}, "select a resource or registry group to toggle its source", false
	}
	if refusal = sourceTargetRefusal(target); refusal != "" {
		return row{}, refusal, false
	}
	return target, "", true
}

// sectionSourceRow returns a row of the one source a registry group section
// holds, or a message when it holds several.
func sectionSourceRow(item flatItem) (row, string) {
	target := item.SectionRows[0]
	matcher := newSourceMatcher(&target)
	for i := 1; i < len(item.SectionRows); i++ {
		if !matcher.matches(&item.SectionRows[i]) {
			return row{}, "this group holds several sources; select a resource to toggle its source"
		}
	}
	return target, ""
}

// sourceTargetRefusal explains why target has no source to toggle, or
// returns "" when it has one.
func sourceTargetRefusal(target row) string {
	switch {
	case target.IsMalformed:
		return "malformed resources have no source to toggle"
	case target.MarketplaceDiscovery != nil:
		return "Marketplace search results are not installed; install them from the toggle menu"
	case !target.Artifact.IsMarketplace && strings.TrimSpace(target.RegistryURL) == "":
		return "this resource has no source to toggle"
	}
	return ""
}

// sourceToggleFor resolves the whole-source exclusion toggle for the source
// of target, which sourceTargetRefusal accepts.
func (m Model) sourceToggleFor(target row) (sourceToggle, string) {
	if target.Artifact.IsMarketplace {
		return m.marketplaceSourceToggle(target)
	}
	toggle := sourceToggle{
		label: registryDisplayLabel(target),
		entry: config.RegistryEntry{
			URL: target.RegistryURL, Ref: target.RegistryRef, Path: target.RegistryPath,
			V4Kind: target.Artifact.V4Kind, SourceHarness: target.Artifact.SourceHarness,
		},
	}
	matcher := newSourceMatcher(&target)
	excludeAll, allExcluded, machineScope := false, true, false
	for i := range m.rows {
		r := &m.rows[i]
		if r.IsMalformed || r.Artifact.IsMarketplace || r.MarketplaceDiscovery != nil || !matcher.matches(r) {
			continue
		}
		if toggle.entry.Ref == "" {
			toggle.entry.Ref = r.RegistryRef
		}
		if r.Artifact.Required {
			continue
		}
		toggle.resources++
		excludeAll = excludeAll || r.SourceExcludedLocally
		allExcluded = allExcluded && r.ExcludedLocally
		machineScope = machineScope || r.Artifact.Source.IsMachineScope()
	}
	if toggle.resources == 0 {
		return sourceToggle{}, "required sources cannot be excluded"
	}
	if excludeAll || allExcluded {
		if excludeAll {
			toggle.edits = append(toggle.edits, config.SourceClearExcludeAll)
		}
		if allExcluded {
			toggle.edits = append(toggle.edits, config.SourceClearResourceExclusions)
		}
		return toggle, ""
	}
	if machineScope {
		return sourceToggle{}, "local config cannot exclude a source from global or org config"
	}
	toggle.exclude = true
	toggle.edits = []config.SourceExclusionEdit{config.SourceExcludeAll}
	return toggle, ""
}

// marketplaceSourceToggle sets or clears the user override of every
// non-required organization resource of target's Marketplace source, as [e]
// does for one. Resources installed by user or repository config have no
// override and keep their installation.
func (m Model) marketplaceSourceToggle(target row) (sourceToggle, string) {
	matcher := newSourceMatcher(&target)
	var included, excluded []toggleAction
	for i := range m.rows {
		r := &m.rows[i]
		if !r.Artifact.IsMarketplace || r.MarketplaceDiscovery != nil || !r.canOverrideMarketplace() || !matcher.matches(r) {
			continue
		}
		act := scopedToggleAction(*r, scopeUserExclude)
		if !act.hasMarketplaceSelection || act.marketplaceSelection.Pin.RegistrySlug == "" {
			continue
		}
		if r.ExcludedByUser {
			excluded = append(excluded, act)
		} else {
			included = append(included, act)
		}
	}
	toggle := sourceToggle{label: registryDisplayLabel(target), exclude: len(included) > 0, marketplace: included}
	if !toggle.exclude {
		toggle.marketplace = excluded
	}
	if len(toggle.marketplace) == 0 {
		return sourceToggle{}, "only organization Marketplace resources can be excluded as a whole; use the toggle menu for the others"
	}
	toggle.resources = len(toggle.marketplace)
	return toggle, ""
}

func (m Model) startSourceToggle(toggle sourceToggle) (tea.Model, tea.Cmd) {
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	recordActionSpan(m.ctx, "source_toggle", map[string]string{
		"exclude":        strconv.FormatBool(toggle.exclude),
		"scope":          toggle.scopeName(),
		"resource.count": strconv.Itoa(toggle.resources),
	})
	return m, correlateOperation(runDependencySourceToggle(ctxOrBackground(m.ctx), m.workspace, toggle, m.dependencyLifecycle.Mutate), operation, false)
}

func (t sourceToggle) scopeName() string {
	if len(t.marketplace) > 0 {
		return "user"
	}
	return "local"
}

func runSourceToggleInWorkspace(ctx context.Context, repoRoot, localConfigDir string, toggle sourceToggle, mapper pipeline.ConfigPathMapper) (bool, error) {
	if len(toggle.marketplace) > 0 {
		for i, act := range toggle.marketplace {
			if err := runMarketplaceScopedToggle(ctx, repoRoot, localConfigDir, act, mapper); err != nil {
				return i > 0, err
			}
		}
		return true, nil
	}
	originalPath := config.LocalConfigPath(localConfigDir)
	path := originalPath
	if mapper != nil {
		var err error
		if path, err = mapper(originalPath); err != nil {
			return false, err
		}
	}
	changed := false
	for _, edit := range toggle.edits {
		editChanged, err := config.EditSourceExclusion(path, originalPath, toggle.entry, edit)
		if err != nil {
			return changed, err
		}
		changed = changed || editChanged
	}
	return changed, nil
}

func runDependencySourceToggle(ctx context.Context, workspace pipeline.Workspace, toggle sourceToggle, mutate DependencyMutationFunc) tea.Cmd {
	return func() tea.Msg {
		var changed bool
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return configWrittenMsg{skillName: toggle.label, err: err}
		}
		if mutate == nil {
			changed, err = runSourceToggleInWorkspace(ctx, ws.RepoRoot(), ws.LocalConfigDir(), toggle, nil)
		} else {
			err = mutate(ctx, ws, func(mapper pipeline.ConfigPathMapper) error {
				var runErr error
				changed, runErr = runSourceToggleInWorkspace(ctx, ws.RepoRoot(), ws.LocalConfigDir(), toggle, mapper)
				return runErr
			})
		}
		label := sanitizeTerminalData(toggle.label, terminalSingleLine)
		var summary string
		switch {
		case len(toggle.marketplace) > 0 && toggle.exclude:
			summary = fmt.Sprintf("Excluded source %s for your user (%d %s); press x, e to undo", label, toggle.resources, pluralResources(toggle.resources))
		case len(toggle.marketplace) > 0:
			summary = fmt.Sprintf("Removed user overrides of source %s (%d %s)", label, toggle.resources, pluralResources(toggle.resources))
		case !changed:
			summary = fmt.Sprintf("No local exclusions of %s to change", label)
		case toggle.exclude:
			summary = fmt.Sprintf("Excluded source %s locally (%d %s); press x, e to undo", label, toggle.resources, pluralResources(toggle.resources))
		default:
			summary = fmt.Sprintf("Removed local exclusions of source %s", label)
		}
		if err != nil {
			slog.Error("TUI source toggle failed", "exclude", toggle.exclude, "scope", toggle.scopeName(), "resources", toggle.resources, "error", err)
		} else {
			slog.Info("TUI source toggle completed", "exclude", toggle.exclude, "scope", toggle.scopeName(), "resources", toggle.resources, "changed", changed)
		}
		return configWrittenMsg{skillName: label, enabled: !toggle.exclude, summary: summary, err: err}
	}
}

func pluralResources(n int) string {
	if n == 1 {
		return "resource"
	}
	return "resources"
}

func registryDisplayLabel(r row) string {
	base := r.RegistryName
	if base == "" {
		base = registryDisplayName(r.RegistryURL)
	}
	if base == "" {
		base = "(local)"
	}
	if p := strings.Trim(r.RegistryPath, "/"); p != "" {
		base += " / " + p
	}
	return base
}
