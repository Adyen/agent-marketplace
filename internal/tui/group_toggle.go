package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// groupToggle selects or deselects every resource of one source in one
// scope, captured when the key is pressed so a reload cannot change what is
// written. A Git or directory source is changed through one source-level
// selection in the scope's config; a Marketplace source through the
// installation pin of each resource in actions.
type groupToggle struct {
	entry       config.RegistryEntry
	label       string
	scope       scope
	enable      bool
	marketplace []toggleAction
	resources   int
}

// sourceGroupCounts is the per-scope state of a source's non-required
// resources shown by the group toggle modal.
type sourceGroupCounts struct {
	resources, required    int
	global, project, local int
	excluded               int
	orgMarketplace         int
}

func rowEnabledAt(r *row, s scope) bool {
	switch s {
	case scopeGlobal:
		return r.EnabledGlobal
	case scopeProject:
		return r.EnabledProject
	case scopeLocal:
		return r.EnabledLocal
	}
	return false
}

// sourceRowMatches reports whether r is an installed row of the source
// matcher describes, on the same side of the Marketplace boundary as target.
func sourceRowMatches(r *row, target *row, matcher sourceMatcher) bool {
	return !r.IsMalformed && r.MarketplaceDiscovery == nil && r.Artifact.IsMarketplace == target.Artifact.IsMarketplace && matcher.matches(r)
}

func (m Model) sourceGroupCounts(target row) sourceGroupCounts {
	matcher := newSourceMatcher(&target)
	var counts sourceGroupCounts
	for i := range m.rows {
		r := &m.rows[i]
		if !sourceRowMatches(r, &target, matcher) {
			continue
		}
		if r.Artifact.Required {
			counts.required++
			continue
		}
		counts.resources++
		if r.EnabledGlobal {
			counts.global++
		}
		if r.EnabledProject {
			counts.project++
		}
		if r.EnabledLocal {
			counts.local++
		}
		if r.ExcludedLocally || r.SourceExcludedLocally || r.ExcludedByUser {
			counts.excluded++
		}
		if r.Artifact.IsMarketplace && r.canOverrideMarketplace() {
			counts.orgMarketplace++
		}
	}
	return counts
}

// openGroupToggleModal opens the group toggle modal for the source of the
// resource row or registry group under the cursor, or returns a message
// explaining why it cannot.
func (m *Model) openGroupToggleModal() string {
	target, refusal, ok := m.sourceTargetAtCursor()
	if !ok {
		return refusal
	}
	if refusal := m.sourceMutationRefusal(target); refusal != "" {
		return refusal
	}
	if m.sourceGroupCounts(target).resources == 0 {
		return "required sources cannot be toggled"
	}
	m.pane = paneGroupToggleModal
	m.modalSource = target
	return ""
}

func (m Model) sourceMutationRefusal(target row) string {
	if m.configInvalid {
		return "fix the configuration before changing selections"
	}
	matcher := newSourceMatcher(&target)
	for i := range m.rows {
		if sourceRowMatches(&m.rows[i], &target, matcher) && m.rows[i].PendingVerification {
			return "selection is still being verified"
		}
	}
	return ""
}

// groupToggleFor resolves the scoped toggle of the source of target: every
// resource is selected in s unless all already are, then all are deselected.
// Otherwise it returns a message explaining why nothing can change.
func (m Model) groupToggleFor(target row, s scope) (groupToggle, string) {
	if target.Artifact.IsMarketplace {
		return m.marketplaceGroupToggle(target, s)
	}
	toggle := groupToggle{
		label: registryDisplayLabel(target),
		scope: s,
		entry: config.RegistryEntry{
			URL: target.RegistryURL, Ref: target.RegistryRef, Path: target.RegistryPath,
			V4Kind: target.Artifact.V4Kind, SourceHarness: target.Artifact.SourceHarness,
		},
	}
	matcher := newSourceMatcher(&target)
	allEnabled := true
	for i := range m.rows {
		r := &m.rows[i]
		if !sourceRowMatches(r, &target, matcher) {
			continue
		}
		if toggle.entry.Ref == "" {
			toggle.entry.Ref = r.RegistryRef
		}
		if r.Artifact.Required {
			continue
		}
		toggle.resources++
		allEnabled = allEnabled && rowEnabledAt(r, s)
	}
	if toggle.resources == 0 {
		return groupToggle{}, "required sources cannot be toggled"
	}
	toggle.enable = !allEnabled
	if toggle.enable && s == scopeGlobal {
		if err := config.ValidateGlobalRegistryWrite(toggle.entry); err != nil {
			return groupToggle{}, err.Error()
		}
	}
	return toggle, ""
}

// marketplaceGroupToggle adds or removes the scope's installation pin of
// every resource of target's Marketplace source that [g]/[p]/[l] can change
// on its own. Organization resources are overridden through [e] instead.
func (m Model) marketplaceGroupToggle(target row, s scope) (groupToggle, string) {
	matcher := newSourceMatcher(&target)
	var candidates []*row
	orgOnly, collectionOnly := 0, 0
	for i := range m.rows {
		r := &m.rows[i]
		if !sourceRowMatches(r, &target, matcher) || r.Artifact.Required {
			continue
		}
		switch {
		case r.canOverrideMarketplace():
			orgOnly++
		case r.MarketplaceCollectionOnly:
			collectionOnly++
		default:
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		switch {
		case orgOnly > 0:
			return groupToggle{}, "organization Marketplace items can be disabled with [e] user override"
		case collectionOnly > 0:
			return groupToggle{}, "collection-selected Marketplace items must be toggled through their collection"
		}
		return groupToggle{}, "required sources cannot be toggled"
	}
	toggle := groupToggle{label: registryDisplayLabel(target), scope: s}
	for _, r := range candidates {
		toggle.enable = toggle.enable || !rowEnabledAt(r, s)
	}
	for _, r := range candidates {
		if rowEnabledAt(r, s) == toggle.enable {
			continue
		}
		act := scopedToggleAction(*r, s)
		if !act.hasMarketplaceSelection {
			continue
		}
		if !act.enable && !act.marketplaceSelection.Pin.Direct && len(act.marketplaceSelection.Pin.SelectedBy.List()) > 0 {
			continue
		}
		toggle.marketplace = append(toggle.marketplace, act)
	}
	if len(toggle.marketplace) == 0 {
		return groupToggle{}, fmt.Sprintf("no %s Marketplace installations of this source to change", scopeName(s))
	}
	toggle.resources = len(toggle.marketplace)
	return toggle, ""
}

func (m Model) startGroupToggle(toggle groupToggle) (tea.Model, tea.Cmd) {
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	recordActionSpan(m.ctx, "group_toggle", map[string]string{
		"enable":         strconv.FormatBool(toggle.enable),
		"scope":          scopeName(toggle.scope),
		"marketplace":    strconv.FormatBool(len(toggle.marketplace) > 0),
		"resource.count": strconv.Itoa(toggle.resources),
	})
	return m, correlateOperation(runDependencyGroupToggle(ctxOrBackground(m.ctx), m.workspace, toggle, m.dependencyLifecycle.Mutate), operation, false)
}

func runGroupToggleInWorkspace(ctx context.Context, repoRoot, localConfigDir string, toggle groupToggle, mapper pipeline.ConfigPathMapper) (bool, error) {
	if len(toggle.marketplace) > 0 {
		for i, act := range toggle.marketplace {
			if err := runMarketplaceScopedToggle(ctx, repoRoot, localConfigDir, act, mapper); err != nil {
				return i > 0, err
			}
		}
		return true, nil
	}
	originalPath, err := scopedToggleConfigPath(repoRoot, toggle.scope)
	if err != nil {
		return false, err
	}
	if toggle.scope == scopeLocal {
		originalPath = config.LocalConfigPath(localConfigDir)
	}
	path := originalPath
	if mapper != nil {
		if path, err = mapper(originalPath); err != nil {
			return false, err
		}
	}
	return config.EditSourceSelection(path, originalPath, toggle.entry, toggle.enable)
}

func runDependencyGroupToggle(ctx context.Context, workspace pipeline.Workspace, toggle groupToggle, mutate DependencyMutationFunc) tea.Cmd {
	return func() tea.Msg {
		var changed bool
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return configWrittenMsg{skillName: toggle.label, err: err}
		}
		if mutate == nil {
			changed, err = runGroupToggleInWorkspace(ctx, ws.RepoRoot(), ws.LocalConfigDir(), toggle, nil)
		} else if len(toggle.marketplace) > 0 {
			// Marketplace pin edits own their config, acquisition lock, and
			// sync transaction. Do not nest them in a staged config mutation.
			changed, err = runGroupToggleInWorkspace(ctx, ws.RepoRoot(), ws.LocalConfigDir(), toggle, nil)
		} else {
			err = mutate(ctx, ws, func(mapper pipeline.ConfigPathMapper) error {
				var runErr error
				changed, runErr = runGroupToggleInWorkspace(ctx, ws.RepoRoot(), ws.LocalConfigDir(), toggle, mapper)
				return runErr
			})
		}
		label := sanitizeTerminalData(toggle.label, terminalSingleLine)
		where := scopeName(toggle.scope)
		var summary string
		switch {
		case !changed:
			summary = fmt.Sprintf("No %s selections of %s to change", where, label)
		case toggle.enable:
			summary = fmt.Sprintf("Enabled source %s in %s config (%d %s)", label, where, toggle.resources, pluralResources(toggle.resources))
		default:
			summary = fmt.Sprintf("Disabled source %s in %s config (%d %s)", label, where, toggle.resources, pluralResources(toggle.resources))
		}
		if err != nil {
			slog.Error("TUI group toggle failed", "enable", toggle.enable, "scope", where, "resources", toggle.resources, "error", err)
		} else {
			slog.Info("TUI group toggle completed", "enable", toggle.enable, "scope", where, "resources", toggle.resources, "changed", changed)
		}
		return configWrittenMsg{skillName: label, enabled: toggle.enable, summary: summary, err: err}
	}
}

// handleGroupToggleModalKey routes key presses of the group toggle modal.
func (m Model) handleGroupToggleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	target := m.modalSource
	if !key.Matches(msg, m.keys.ToggleCancel) {
		if m.loading || m.syncing {
			return m, nil
		}
		if refusal := m.sourceMutationRefusal(target); refusal != "" {
			return m, groupRefusalCmd(refusal)
		}
	}
	var s scope
	switch {
	case key.Matches(msg, m.keys.ToggleCancel):
		m.pane = paneList
		recordActionSpan(m.ctx, "modal_outcome", map[string]string{"outcome": "cancelled", "modal": "group_toggle"})
		return m, nil
	case key.Matches(msg, m.keys.ToggleExclude):
		toggle, refusal := m.sourceToggleFor(target)
		if len(toggle.edits) == 0 && len(toggle.marketplace) == 0 {
			return m, groupRefusalCmd(refusal)
		}
		m.pane = paneList
		return m.startSourceToggle(toggle)
	case key.Matches(msg, m.keys.ToggleGlobal):
		s = scopeGlobal
	case key.Matches(msg, m.keys.ToggleProject):
		s = scopeProject
	case key.Matches(msg, m.keys.ToggleLocal):
		s = scopeLocal
	default:
		return m, nil
	}
	toggle, refusal := m.groupToggleFor(target, s)
	if refusal != "" {
		return m, groupRefusalCmd(refusal)
	}
	return m.startGroupToggle(toggle)
}

func groupRefusalCmd(refusal string) tea.Cmd {
	if refusal == "" {
		return nil
	}
	return func() tea.Msg {
		return toastMsg{text: refusal, duration: 3 * time.Second}
	}
}

// renderGroupToggleModal renders the group toggle modal overlay: how many of
// the source's resources each scope selects, and the available keys.
func renderGroupToggleModal(m Model) string {
	target := m.modalSource
	counts := m.sourceGroupCounts(target)
	label := sanitizeTerminalData(registryDisplayLabel(target), terminalSingleLine)
	t := m.currentTheme()
	title := titleStyle(t).Render(fmt.Sprintf("Source %q", label))
	keyStyle := markKeyStyle(t)
	of := func(n int) string { return fmt.Sprintf("%d/%d", n, counts.resources) }
	orgOnly := counts.orgMarketplace == counts.resources
	excludeLabel := "toggle whole-source exclusion"
	if target.Artifact.IsMarketplace {
		excludeLabel = "toggle user override of the source"
	}

	if compactTerminalLayout(m) {
		body := []string{
			fmt.Sprintf("Global %s  Project %s", of(counts.global), of(counts.project)),
			fmt.Sprintf("Local %s  Excluded %s", of(counts.local), of(counts.excluded)),
		}
		if !orgOnly {
			body = append(body, keyStyle.Render("[g]")+" global  "+keyStyle.Render("[p]")+" project")
			body = append(body, keyStyle.Render("[l]")+" local   "+keyStyle.Render("[e]")+" exclude")
		} else {
			body = append(body, keyStyle.Render("[e]")+" exclude")
		}
		return modalFrame(m, t.accent, title, body, []footerHint{{"esc", "cancel"}})
	}

	summary := fmt.Sprintf("%d %s", counts.resources, pluralResources(counts.resources))
	if counts.required > 0 {
		summary += fmt.Sprintf(", %d required and unchanged", counts.required)
	}
	body := []string{
		summary,
		"",
		fmt.Sprintf("Global:   %s", of(counts.global)),
		fmt.Sprintf("Project:  %s", of(counts.project)),
		fmt.Sprintf("Local:    %s", of(counts.local)),
		fmt.Sprintf("Excluded: %s", of(counts.excluded)),
		"",
	}
	actions := []struct{ key, label string }{
		{"[g]", "toggle global for every resource"},
		{"[p]", "toggle project for every resource"},
		{"[l]", "toggle local for every resource"},
		{"[e]", excludeLabel},
		{"[esc]", "cancel"},
	}
	if orgOnly {
		actions = actions[3:]
	}
	for _, a := range actions {
		body = append(body, keyStyle.Render(a.key)+"  "+a.label)
	}
	return modalFrame(m, t.accent, title, body, nil)
}
