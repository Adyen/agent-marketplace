package tui

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// scope identifies which configuration file the toggle modal is acting on.
type scope int

const (
	scopeGlobal scope = iota
	scopeProject
	scopeLocal
	scopeLocalExclude
	scopeUserExclude
)

// toggleAction describes the direction of a toggle (add vs remove) for a
// given (skill, scope) pair, derived from the row's current state.
type toggleAction struct {
	skillName               string
	kind                    ir.ResourceKind
	scope                   scope
	enable                  bool   // true = add to config; false = remove from config
	registryURL             string // for project/global scopes: which registry entry to target
	registryRef             string // ref of the source registry (for EnsureRegistryInConfig fallback)
	registryPath            string // path subdirectory of the source registry (for EnsureRegistryWithPathsInConfig fallback)
	registryKind            config.V4SourceKind
	sourceHarness           string
	isMarketplace           bool
	allowMarketplaceExclude bool
	marketplaceSelection    marketplaceSelection
	hasMarketplaceSelection bool
}

// scopedToggleAction returns the action (add or remove) for pressing the
// given scope key on a modal. The decision is driven entirely by the row's
// current flags:
//   - scopeGlobal: enable when !EnabledGlobal, disable otherwise.
//   - scopeProject: enable when !EnabledProject, disable otherwise.
//   - scopeLocal: enable when !EnabledLocal, disable otherwise.
//   - scopeLocalExclude: add exclude when !ExcludedLocally, remove otherwise.
func marketplaceSourceForScope(s scope) config.Source {
	switch s {
	case scopeGlobal:
		return config.UserPersonal
	case scopeProject:
		return config.RepoCommitted
	case scopeLocal:
		return config.RepoPersonal
	default:
		return config.SourceUnset
	}
}

func marketplaceSelectionForToggle(r row, s scope) (marketplaceSelection, bool) {
	if s == scopeUserExclude {
		selection, ok := r.MarketplaceSelections[config.Organization]
		return selection, ok
	}
	if selection, ok := r.MarketplaceSelections[marketplaceSourceForScope(s)]; ok {
		return selection, true
	}
	if selection, ok := r.MarketplaceSelections[r.Artifact.Source]; ok && (selection.Pin.Direct || len(selection.Pin.SelectedBy.List()) == 0) {
		return selection, true
	}
	for _, source := range []config.Source{config.RepoPersonal, config.RepoCommitted, config.UserPersonal, config.Organization} {
		selection, ok := r.MarketplaceSelections[source]
		if ok && (selection.Pin.Direct || len(selection.Pin.SelectedBy.List()) == 0) {
			return selection, true
		}
	}
	return marketplaceSelection{}, false
}

func scopedToggleAction(r row, s scope) toggleAction {
	act := toggleAction{
		skillName:               r.Artifact.Name,
		kind:                    r.Artifact.Kind,
		scope:                   s,
		registryURL:             r.RegistryURL,
		registryRef:             r.RegistryRef,
		registryPath:            r.RegistryPath,
		registryKind:            r.Artifact.V4Kind,
		sourceHarness:           r.Artifact.SourceHarness,
		isMarketplace:           r.Artifact.IsMarketplace,
		allowMarketplaceExclude: r.CanOverrideMarketplace(),
	}
	if act.isMarketplace {
		act.marketplaceSelection, act.hasMarketplaceSelection = marketplaceSelectionForToggle(r, s)
	}
	switch s {
	case scopeGlobal:
		act.enable = !r.EnabledGlobal
	case scopeProject:
		act.enable = !r.EnabledProject
	case scopeLocal:
		act.enable = !r.EnabledLocal
	case scopeLocalExclude:
		act.enable = !r.ExcludedLocally
	case scopeUserExclude:
		act.enable = !r.ExcludedByUser
	}
	return act
}

func (a toggleAction) disablesResource() bool {
	if a.scope == scopeLocalExclude || a.scope == scopeUserExclude {
		return a.enable
	}
	return !a.enable
}

// runScopedToggle returns a Cmd that writes the toggle to the appropriate
// config file and emits a configWrittenMsg on completion. The existing
// configWrittenMsg handler in Update() re-loads the snapshot, which picks
// up the new state.
//
// For project/global scopes, the toggle targets the specific registry entry
// (identified by act.registryURL) whose catalog contains the skill. This
// ensures the skill is added to/removed from the entry's per-registry skills
// list, which the resolver actually processes. Writing to the top-level
// skills list would have no effect when the entry has its own skills list
// (specific wins over default in attribute inheritance).
func marketplaceToggleConfigPath(repoRoot string, s scope) (string, error) {
	switch s {
	case scopeGlobal:
		return config.GlobalConfigPath()
	case scopeProject:
		return config.ProjectConfigPath(repoRoot), nil
	case scopeLocal:
		return config.LocalConfigPath(repoRoot), nil
	default:
		return "", fmt.Errorf("unsupported Marketplace toggle scope")
	}
}

func scopedToggleConfigPath(repoRoot string, s scope) (string, error) {
	if s == scopeLocalExclude {
		return config.LocalConfigPath(repoRoot), nil
	}
	return marketplaceToggleConfigPath(repoRoot, s)
}

func runMarketplaceScopedToggle(ctx context.Context, repoRoot, localConfigDir string, act toggleAction, mapper pipeline.ConfigPathMapper) error {
	if act.scope == scopeUserExclude {
		if !act.allowMarketplaceExclude || !act.hasMarketplaceSelection || act.marketplaceSelection.Pin.RegistrySlug == "" {
			return fmt.Errorf("marketplace resource has no overridable organization pin")
		}
		path, err := config.GlobalConfigPath()
		if err != nil {
			return err
		}
		if mapper != nil {
			path, err = mapper(path)
			if err != nil {
				return err
			}
		}
		selection := act.marketplaceSelection
		return config.SetUserMarketplaceExclusion(path, selection.Entry.URL, selection.Pin.RegistrySlug, act.kind, selection.Pin.Name, act.enable)
	}
	if !act.hasMarketplaceSelection {
		return fmt.Errorf("marketplace %s %q has no direct installation pin", skill.KindLabel(act.kind), act.skillName)
	}
	configPath, err := marketplaceToggleConfigPath(repoRoot, act.scope)
	if err != nil {
		return err
	}
	selection := act.marketplaceSelection
	if !act.enable && !selection.Pin.Direct && len(selection.Pin.SelectedBy.List()) > 0 {
		return fmt.Errorf("marketplace %s %q is selected only through a collection", skill.KindLabel(act.kind), act.skillName)
	}
	var source config.Source
	switch act.scope {
	case scopeGlobal:
		source = config.UserPersonal
	case scopeProject:
		source = config.RepoCommitted
	case scopeLocal:
		source = config.RepoPersonal
	default:
		return fmt.Errorf("unsupported Marketplace toggle scope")
	}
	if act.scope == scopeLocal {
		rel, err := filepath.Rel(repoRoot, localConfigDir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			configPath = config.LocalConfigPath(localConfigDir)
		}
	}
	return pipeline.ToggleMarketplacePin(ctx, repoRoot, localConfigDir, configPath, source, selection.Entry, selection.Pin, act.enable)
}

func runScopedToggle(repoRoot string, act toggleAction) tea.Cmd {
	return runScopedToggleAt(repoRoot, act, nil)
}

func runScopedToggleAt(repoRoot string, act toggleAction, mapper pipeline.ConfigPathMapper) tea.Cmd {
	return func() tea.Msg {
		mapPath := func(path string) (string, error) {
			if mapper == nil {
				return path, nil
			}
			return mapper(path)
		}
		if act.isMarketplace {
			localConfigDir := repoRoot
			candidate := config.FindLocalConfigDir()
			if rel, err := filepath.Rel(repoRoot, candidate); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				localConfigDir = candidate
			}
			err := runMarketplaceScopedToggle(context.Background(), repoRoot, localConfigDir, act, mapper)
			if err != nil {
				slog.Error("TUI Marketplace toggle failed", "kind", act.kind.String(), "name", act.skillName, "scope", scopeName(act.scope), "enable", act.enable, "error", err)
			} else {
				slog.Info("TUI Marketplace toggle completed", "kind", act.kind.String(), "name", act.skillName, "scope", scopeName(act.scope), "enable", act.enable)
			}
			return configWrittenMsg{skillName: act.skillName, enabled: act.enable, err: err}
		}
		originalPath, pathErr := scopedToggleConfigPath(repoRoot, act.scope)
		if pathErr != nil {
			return configWrittenMsg{skillName: act.skillName, enabled: act.enable, selectNext: act.disablesResource(), err: pathErr}
		}
		mappedPath, pathErr := mapPath(originalPath)
		if pathErr != nil {
			return configWrittenMsg{skillName: act.skillName, enabled: act.enable, selectNext: act.disablesResource(), err: pathErr}
		}
		source := config.RepoPersonal
		switch act.scope {
		case scopeGlobal:
			source = config.UserPersonal
		case scopeProject:
			source = config.RepoCommitted
		}
		entry := config.RegistryEntry{URL: act.registryURL, Ref: act.registryRef, Path: act.registryPath, V4Kind: act.registryKind, SourceHarness: act.sourceHarness}
		locator := config.ResourceLocator{Kind: act.kind, Name: act.skillName}
		target := config.LayerTarget{Source: source, StagedPath: mappedPath, DeclaringPath: originalPath}
		var err error
		if act.scope == scopeLocalExclude {
			err = pipeline.EditLayerAtPath(target, func(m *pipeline.LayerMutation) error {
				if strings.TrimSpace(act.registryURL) == "" {
					if act.enable {
						return m.SetUnboundExclude(locator)
					}
					return m.ClearUnboundExclude(locator)
				}
				if act.enable {
					binding, err := m.EnsureRegistry(entry)
					if err != nil {
						return err
					}
					return m.Apply(binding, config.SelectionChange{Operation: config.SelectionExclude, Locators: []config.ResourceLocator{locator}})
				}
				binding, found, err := m.LookupRegistry(entry)
				if err != nil {
					return err
				}
				if found {
					if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionClearExclude, Locators: []config.ResourceLocator{locator}}); err != nil {
						return err
					}
				}
				return m.ClearUnboundExclude(locator)
			})
		} else {
			err = session.EditRegistryResourceSelection(target, entry, locator, act.enable, false)
		}
		if err != nil {
			slog.Error("TUI toggle failed", "kind", act.kind.String(), "name", act.skillName, "scope", scopeName(act.scope), "enable", act.enable, "error", err)
		} else {
			slog.Info("TUI toggle completed", "kind", act.kind.String(), "name", act.skillName, "scope", scopeName(act.scope), "enable", act.enable)
		}
		return configWrittenMsg{
			skillName:  act.skillName,
			enabled:    act.enable,
			selectNext: act.disablesResource(),
			err:        err,
		}
	}
}

// applyToggleAction applies one resolved selection change inside an open
// layer mutation. It is shared by the single-row toggle and the atomic
// section bulk toggle so both write identical semantics.
func applyToggleAction(m *pipeline.LayerMutation, act toggleAction) error {
	if act.isMarketplace {
		return applyMarketplacePinAction(m, act)
	}
	entry := config.RegistryEntry{URL: act.registryURL, Ref: act.registryRef, Path: act.registryPath, V4Kind: act.registryKind, SourceHarness: act.sourceHarness}
	locator := config.ResourceLocator{Kind: act.kind, Name: act.skillName}
	if act.scope == scopeLocalExclude {
		if strings.TrimSpace(act.registryURL) == "" {
			if act.enable {
				return m.SetUnboundExclude(locator)
			}
			return m.ClearUnboundExclude(locator)
		}
		if act.enable {
			binding, err := m.EnsureRegistry(entry)
			if err != nil {
				return err
			}
			return m.Apply(binding, config.SelectionChange{Operation: config.SelectionExclude, Locators: []config.ResourceLocator{locator}})
		}
		binding, found, err := m.LookupRegistry(entry)
		if err != nil {
			return err
		}
		if found {
			if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionClearExclude, Locators: []config.ResourceLocator{locator}}); err != nil {
				return err
			}
		}
		return m.ClearUnboundExclude(locator)
	}
	var binding config.SourceBinding
	var found bool
	var err error
	if !act.enable {
		binding, found, err = m.LookupRegistry(entry)
		if err != nil {
			return err
		}
	}
	if !found {
		binding, err = m.EnsureRegistry(entry)
		if err != nil {
			return err
		}
	}
	operation := config.SelectionExclude
	if act.enable {
		operation = config.SelectionInclude
	}
	return m.Apply(binding, config.SelectionChange{Operation: operation, Locators: []config.ResourceLocator{locator}})
}

// applyMarketplacePinAction applies a Marketplace pin add/remove inside an
// open v4 layer mutation, enforcing the same direct-pin rule the standalone
// toggle applies. scopeUserExclude is handled by the caller because it owns
// a different file-level edit.
func applyMarketplacePinAction(m *pipeline.LayerMutation, act toggleAction) error {
	if !act.hasMarketplaceSelection {
		return fmt.Errorf("marketplace %s %q has no direct installation pin", skill.KindLabel(act.kind), act.skillName)
	}
	selection := act.marketplaceSelection
	if !act.enable && !selection.Pin.Direct && len(selection.Pin.SelectedBy.List()) > 0 {
		return fmt.Errorf("marketplace %s %q is selected only through a collection", skill.KindLabel(act.kind), act.skillName)
	}
	if act.enable {
		binding, err := m.EnsureMarketplaceSource(selection.Entry.URL)
		if err != nil {
			return err
		}
		pin := selection.Pin
		pin.Direct = true
		pin.SelectedBy = ""
		return m.IncludeMarketplacePins(binding, []config.MarketplaceResourcePin{pin})
	}
	binding, found, err := m.LookupMarketplaceSource(selection.Entry.URL)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("marketplace endpoint %q is not declared in this layer", selection.Entry.URL)
	}
	return m.RemoveMarketplacePins(binding, []config.MarketplaceResourcePin{selection.Pin})
}

func runDependencyScopedToggle(ctx context.Context, workspace pipeline.Workspace, act toggleAction, mutate DependencyMutationFunc) tea.Cmd {
	return func() tea.Msg {
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return configWrittenMsg{skillName: act.skillName, enabled: act.enable, selectNext: act.disablesResource(), err: err}
		}
		writeRoot := ws.RepoRoot()
		if act.scope == scopeLocal || act.scope == scopeLocalExclude {
			writeRoot = ws.LocalConfigDir()
		}
		if mutate == nil {
			return runScopedToggle(writeRoot, act)()
		}
		if act.isMarketplace && act.scope != scopeUserExclude {
			err = runMarketplaceScopedToggle(ctx, ws.RepoRoot(), ws.LocalConfigDir(), act, nil)
			return configWrittenMsg{skillName: act.skillName, enabled: act.enable, selectNext: act.disablesResource(), err: err}
		}
		err = mutate(ctx, ws, func(mapper pipeline.ConfigPathMapper) error {
			msg, ok := runScopedToggleAt(writeRoot, act, mapper)().(configWrittenMsg)
			if !ok {
				return fmt.Errorf("toggle did not produce a config result")
			}
			return msg.err
		})
		return configWrittenMsg{skillName: act.skillName, enabled: act.enable, selectNext: act.disablesResource(), err: err}
	}
}

func competingRowsInScope(rows []row, targetScope scope) []row {
	var source config.Source
	switch targetScope {
	case scopeGlobal:
		source = config.UserPersonal
	case scopeProject:
		source = config.RepoCommitted
	default:
		return nil
	}
	var eligible []row
	for _, r := range rows {
		if r.Artifact.Source == source && !r.Artifact.Required && !r.Artifact.IsMarketplace {
			eligible = append(eligible, r)
		}
	}
	return eligible
}

// excludeCompetingRegistriesCmd returns a tea.Cmd that adds excludes for
// competing registry URLs that provide the same resource name. This resolves
// name collisions when the user toggles ON a colliding skill.
func excludeCompetingRegistriesCmd(repoRoot string, rows []row, act toggleAction) tea.Cmd {
	rows = competingRowsInScope(rows, act.scope)
	competing := competingRegistryURLs(rows, act.kind, act.skillName, act.registryURL)
	if len(competing) == 0 {
		return nil
	}
	// Also collect per-competing-URL metadata (ref, path) so the competing
	// source binding resolves to the exact entry declared in the target
	// config file rather than a same-URL sibling.
	type competingRegistryMeta struct {
		ref, path, harness string
		kind               config.V4SourceKind
	}
	competingMeta := make(map[string]competingRegistryMeta)
	for _, r := range rows {
		if r.Artifact.Kind != act.kind || r.Artifact.Name != act.skillName || r.RegistryURL == act.registryURL {
			continue
		}
		if _, dup := competingMeta[r.RegistryURL]; dup {
			continue
		}
		competingMeta[r.RegistryURL] = competingRegistryMeta{ref: r.RegistryRef, path: r.RegistryPath, kind: r.Artifact.V4Kind, harness: r.Artifact.SourceHarness}
	}

	return func() tea.Msg {
		var configPath string
		switch act.scope {
		case scopeGlobal:
			p, err := config.GlobalConfigPath()
			if err != nil {
				return configWrittenMsg{skillName: act.skillName, err: err}
			}
			configPath = p
		case scopeProject:
			configPath = config.ProjectConfigPath(repoRoot)
		default:
			return nil
		}
		if act.scope == scopeGlobal {
			entries := make([]config.RegistryEntry, 0, len(competing))
			for _, url := range competing {
				entries = append(entries, config.RegistryEntry{URL: url})
			}
			// Competing repo-relative local registries cannot be shadowed into
			// the user (global) config; reject them to avoid leaking a
			// project-local path machine-wide.
			if err := config.ValidateGlobalRegistryWrite(entries...); err != nil {
				return configWrittenMsg{skillName: act.skillName, enabled: false, err: err}
			}
		}
		source := config.UserPersonal
		if act.scope == scopeProject {
			source = config.RepoCommitted
		}
		locator := config.ResourceLocator{Kind: act.kind, Name: act.skillName}
		err := pipeline.EditLayerAtPath(config.LayerTarget{Source: source, StagedPath: configPath, DeclaringPath: configPath}, func(m *pipeline.LayerMutation) error {
			for _, url := range competing {
				meta := competingMeta[url]
				entry := config.RegistryEntry{URL: url, Ref: meta.ref, Path: meta.path, V4Kind: meta.kind, SourceHarness: meta.harness}
				binding, found, err := m.LookupRegistry(entry)
				if err != nil {
					return fmt.Errorf("exclude competing registry %q: %w", url, err)
				}
				if !found {
					return fmt.Errorf("exclude competing registry %q: source is not declared in %s", url, configPath)
				}
				if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionExclude, Locators: []config.ResourceLocator{locator}}); err != nil {
					return fmt.Errorf("exclude %s %q in competing registry %q: %w", skill.KindLabel(act.kind), act.skillName, url, err)
				}
			}
			return nil
		})
		if err != nil {
			return configWrittenMsg{skillName: act.skillName, enabled: false, err: err}
		}
		return configWrittenMsg{skillName: act.skillName, enabled: true}
	}
}

// renderToggleModal renders the toggle modal overlay for the currently
// selected skill. Shows per-scope state plus the available key actions.
func renderToggleModal(m Model) string {
	r := m.modalSkill
	title := titleStyle(m.currentTheme()).
		Render(fmt.Sprintf(`"%s"`, sanitizeTerminalData(r.Artifact.Name, terminalSingleLine)))

	check := func(on bool) string {
		if on {
			return okStyle(m.currentTheme()).Render("✓")
		}
		return dimTextStyle(m.currentTheme()).Render("✗")
	}
	yesno := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}

	t := m.currentTheme()
	collision := hasCollisionError(m.errs, r.Artifact.Kind, r.Artifact.Name)
	if compactTerminalLayout(m) {
		keyStyle := markKeyStyle(t)
		var body []string
		if collision {
			body = append(body, warnBoldStyle(t).Render("Name collision: choose one source."))
		}
		body = append(body,
			fmt.Sprintf("Global %s  Project %s  Local %s", check(r.EnabledGlobal), check(r.EnabledProject), check(r.EnabledLocal)),
			fmt.Sprintf("Excluded %s", yesno(r.ExcludedLocally || r.ExcludedByUser)))
		switch {
		case r.CanOverrideMarketplace():
			body = append(body, keyStyle.Render("[e]")+" user override")
		case r.Artifact.Source.IsMachineScope():
			body = append(body, keyStyle.Render("[g]")+" global  "+keyStyle.Render("[p]")+" project  "+keyStyle.Render("[l]")+" local",
				styleDim.Render("[e] unavailable"))
		default:
			body = append(body, keyStyle.Render("[g]")+" global  "+keyStyle.Render("[p]")+" project  "+keyStyle.Render("[l]")+" local",
				keyStyle.Render("[e]")+" exclude")
		}
		return modalFrame(m, t.accent, title, body, []footerHint{{"esc", "cancel"}})
	}

	var body []string
	if collision {
		body = append(body, warnBoldStyle(t).Render("⚠ Name collision — toggle may not take effect until you exclude the other source"), "")
	}
	body = append(body, fmt.Sprintf("Global %s   Project %s   Local %s", check(r.EnabledGlobal), check(r.EnabledProject), check(r.EnabledLocal)))
	if r.CanOverrideMarketplace() {
		body = append(body, fmt.Sprintf("User override: %s  (%s)", yesno(r.ExcludedByUser), skill.KindLabel(r.Artifact.Kind)))
	} else {
		body = append(body, fmt.Sprintf("Local exclude: %s  (%s)", yesno(r.ExcludedLocally), skill.KindLabel(r.Artifact.Kind)))
	}
	m.ensureToggleForm()
	if m.toggleForm != nil {
		body = append(body, "", m.toggleForm.View())
	}
	return modalFrame(m, t.accent, title, body, []footerHint{{"↑↓", "move"}, {"enter", "apply"}, {"esc", "cancel"}})
}

// hasCollisionError returns true if any error in errs is a name collision
// affecting the given resource kind and name.
func hasCollisionError(errs []error, kind ir.ResourceKind, name string) bool {
	for _, err := range errs {
		if err == nil {
			continue
		}
		// Pattern: <kind> "<name>" is provided by multiple registries
		s := err.Error()
		if strings.Contains(s, "is provided by multiple registries") &&
			strings.Contains(s, fmt.Sprintf("%q", name)) &&
			strings.Contains(s, skill.KindLabel(kind)) {
			return true
		}
	}
	return false
}

// competingRegistryURLs returns the registry URLs of other rows that have
// the same resource kind and name but a different RegistryURL than the given one.
func competingRegistryURLs(rows []row, kind ir.ResourceKind, name, ownURL string) []string {
	var urls []string
	seen := make(map[string]struct{})
	for _, r := range rows {
		if r.Artifact.Kind != kind || r.Artifact.Name != name || r.RegistryURL == ownURL || r.RegistryURL == "" {
			continue
		}
		if _, dup := seen[r.RegistryURL]; dup {
			continue
		}
		seen[r.RegistryURL] = struct{}{}
		urls = append(urls, r.RegistryURL)
	}
	return urls
}

func validateToggleRegistryWrite(rows []row, errs []error, act toggleAction) error {
	if act.scope != scopeGlobal || !act.enable {
		return nil
	}
	entries := []config.RegistryEntry{{URL: act.registryURL}}
	if hasCollisionError(errs, act.kind, act.skillName) {
		for _, url := range competingRegistryURLs(competingRowsInScope(rows, act.scope), act.kind, act.skillName, act.registryURL) {
			entries = append(entries, config.RegistryEntry{URL: url})
		}
	}
	return config.ValidateGlobalRegistryWrite(entries...)
}

func (m Model) startToggleOperation(act toggleAction) (tea.Model, tea.Cmd) {
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	recordToggleActionSpan(m.ctx, act, m.modalSkill.Artifact.Kind)
	if err := validateToggleRegistryWrite(m.rows, m.errs, act); err != nil {
		return m, correlateOperation(func() tea.Msg {
			return configWrittenMsg{skillName: act.skillName, enabled: false, err: err}
		}, operation, false)
	}
	if act.enable && !act.isMarketplace && (act.scope == scopeGlobal || act.scope == scopeProject) && hasCollisionError(m.errs, m.modalSkill.Artifact.Kind, m.modalSkill.Artifact.Name) {
		m.pendingExcludeAction = &act
	}
	return m, correlateOperation(runDependencyScopedToggle(ctxOrBackground(m.ctx), m.workspace, act, m.dependencyLifecycle.Mutate), operation, false)
}

// handleToggleModalKey routes modal key presses. Letter accelerators
// (g/p/l/e/esc) dispatch directly; remaining keys drive the modal's Huh
// action-select form, whose completion dispatches the same action.
func (m Model) handleToggleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureToggleForm()
	switch {
	case key.Matches(msg, m.keys.ToggleCancel):
		m.pane = paneList
		recordActionSpan(m.ctx, "modal_outcome", map[string]string{
			"outcome":       "cancelled",
			"modal":         "toggle",
			"resource.kind": m.modalSkill.Artifact.Kind.String(),
		})
		return m, nil
	case key.Matches(msg, m.keys.ToggleGlobal):
		return m.runToggleModalAction("g")
	case key.Matches(msg, m.keys.ToggleProject):
		return m.runToggleModalAction("p")
	case key.Matches(msg, m.keys.ToggleLocal):
		return m.runToggleModalAction("l")
	case key.Matches(msg, m.keys.ToggleExclude):
		return m.runToggleModalAction("e")
	default:
		m.toggleForm = updateModalForm(m.toggleForm, msg)
		if m.toggleForm != nil && m.toggleForm.State == huh.StateCompleted && m.toggleChoice != nil && *m.toggleChoice != "" {
			choice := *m.toggleChoice
			m.toggleForm = nil
			return m.runToggleModalAction(choice)
		}
		return m, nil
	}
}

// runToggleModalAction performs one per-scope toggle action identified by
// its accelerator letter (g/p/l/e).
func (m Model) runToggleModalAction(letter string) (tea.Model, tea.Cmd) {
	if m.modalSkill.CanOverrideMarketplace() && (letter == "g" || letter == "p" || letter == "l") {
		return m, func() tea.Msg {
			return toastMsg{text: "organization Marketplace items can be disabled with [e] user override", duration: 2 * time.Second}
		}
	}
	switch letter {
	case "g":
		if m.modalSkill.MarketplaceDiscovery != nil {
			return m.startDiscoveryInstall(m.modalSkill, scopeGlobal)
		}
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeGlobal))
	case "p":
		if m.modalSkill.MarketplaceDiscovery != nil {
			return m.startDiscoveryInstall(m.modalSkill, scopeProject)
		}
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeProject))
	case "l":
		if m.modalSkill.MarketplaceDiscovery != nil {
			return m.startDiscoveryInstall(m.modalSkill, scopeLocal)
		}
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeLocal))
	case "e":
		if m.modalSkill.Artifact.IsMarketplace && !m.modalSkill.CanOverrideMarketplace() {
			return m, func() tea.Msg {
				return toastMsg{text: "Marketplace items must be toggled by installation scope", duration: 2 * time.Second}
			}
		}
		if m.modalSkill.Artifact.Source.IsMachineScope() && !m.modalSkill.CanOverrideMarketplace() {
			return m, func() tea.Msg {
				return toastMsg{text: "Cannot exclude org/user skill at local level — use [g] instead", duration: 2 * time.Second}
			}
		}
		if m.modalSkill.CanOverrideMarketplace() {
			return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeUserExclude))
		}
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeLocalExclude))
	}
	return m, nil
}
