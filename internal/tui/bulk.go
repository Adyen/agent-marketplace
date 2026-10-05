package tui

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// bulkStage is the bulk toggle modal's three-step flow: scope, then
// direction, then explicit confirmation of count and target layer.
type bulkStage int

const (
	bulkStageScope bulkStage = iota
	bulkStageDirection
	bulkStageConfirm
)

// bulkDirection is the explicit enable/disable choice made at stage two.
type bulkDirection int

const (
	bulkDirUnset bulkDirection = iota
	bulkDirEnable
	bulkDirDisable
)

func (d bulkDirection) label() string {
	if d == bulkDirEnable {
		return "enable"
	}
	return "disable"
}

// bulkModalState captures the section the modal opened on plus the
// confirmed-so-far choices. Members and eligibility are re-evaluated at
// submit, never trusted from open time.
type bulkModalState struct {
	sectionPath  string
	sectionTitle string
	stage        bulkStage
	scope        scope
	scopeSet     bool
	direction    bulkDirection

	// confirmed is the immutable copy of the target set displayed at the
	// confirmation stage. Enter re-evaluates the section; a changed set
	// refreshes this copy and requires another affirmative input.
	confirmed        []toggleAction
	confirmedSkipped int
	confirmErr       error
}

// openBulkModal captures the section under the cursor and opens the modal.
// Callers must have already applied the busy/config guards.
func (m *Model) openBulkModal(sectionPath string) {
	title := sectionPath
	if m.cursor >= 0 && m.cursor < len(m.flatItems) {
		if it := m.flatItems[m.cursor]; it.Kind == flatSection && it.SectionPath == sectionPath {
			title = it.SectionTitle
		}
	}
	m.bulkModal = bulkModalState{sectionPath: sectionPath, sectionTitle: title}
	m.pane = paneBulkToggle
}

// sectionMembers returns every artifact row under the given section in the
// current grouping, including descendants hidden by folds. Membership is
// computed over the current filtered rows so an active search narrows the
// target set exactly like the visible list does.
func (m Model) sectionMembers(sectionPath string) []row {
	return m.sectionMembersIn(m.filteredRows, sectionPath)
}

// sectionMembersIn is sectionMembers over an explicit row set, so callers can
// count unfiltered section membership while a search filter is active.
func (m Model) sectionMembersIn(rows []row, sectionPath string) []row {
	items := buildGroupedTree(rows, m.viewSettings, nil)
	var out []row
	depth := -1
	for _, it := range items {
		if it.Kind == flatSection {
			if depth < 0 {
				if it.SectionPath == sectionPath {
					depth = it.SectionDepth
				}
				continue
			}
			if it.SectionDepth <= depth {
				break
			}
			continue
		}
		if depth >= 0 {
			out = append(out, it.Row)
		}
	}
	return out
}

// bulkSkipReason maps the shared per-row denial decision into a skip reason
// and adds the documented batch-only guards: a config-only bulk toggle cannot
// repair a broken row or acquire a cached discovery installation. Busy and
// invalid-config denials are whole-modal guards, not row skip reasons.
func (m Model) bulkSkipReason(r row) string {
	switch m.rowToggleEligibility(r) {
	case toggleMalformed:
		return "malformed"
	case togglePendingVerification:
		return "pending verification"
	case toggleMarketplaceBlocked:
		return "not installable"
	case toggleRequired:
		return "required"
	case toggleCollectionOnly:
		return "collection-selected"
	}
	switch {
	case r.UnavailableReason != "":
		return "unavailable: " + r.UnavailableReason
	case r.MarketplaceDiscovery != nil:
		return "requires acquisition"
	case classifyStatus(r) == statusBroken:
		return "broken"
	}
	return ""
}

// bulkScopeAvailable reports whether the scope key may be selected for this
// workspace: project and local targets need a repository root.
func (m Model) bulkScopeAvailable(s scope) bool {
	switch s {
	case scopeProject, scopeLocal, scopeLocalExclude:
		return m.workspace.RepoRoot() != ""
	}
	return true
}

// bulkScopeForKey maps the scope-stage accelerators to scopes, matching the
// single-row toggle modal's letters.
func bulkScopeForKey(msg tea.KeyPressMsg) (scope, bool) {
	switch msg.Code {
	case 'g':
		return scopeGlobal, true
	case 'p':
		return scopeProject, true
	case 'l':
		return scopeLocal, true
	case 'e':
		return scopeLocalExclude, true
	}
	return 0, false
}

// bulkScopeForRow resolves the exclude accelerator per row: Marketplace rows
// the user can override take the user exclusion, everything else the local
// exclusion — the same split the single-row modal applies for 'e'.
func bulkScopeForRow(r row, s scope) (scope, string) {
	if s != scopeLocalExclude {
		if r.CanOverrideMarketplace() {
			return s, "organization Marketplace items can only be disabled through the user override"
		}
		return s, ""
	}
	switch {
	case r.CanOverrideMarketplace():
		return scopeUserExclude, ""
	case r.Artifact.IsMarketplace:
		return s, "Marketplace items must be toggled by installation scope"
	case r.Artifact.Source.IsMachineScope():
		return s, "org/user resources cannot be excluded locally"
	}
	return s, ""
}

// evaluateBulkActions re-evaluates the section's members against the
// confirmed scope and direction and returns the resolved per-resource
// actions plus a count per skip reason. Rows already in the target state are
// counted as scope/direction-aware no-ops. A section whose actions resolve to
// more than one destination layer refuses the batch: the bulk toggle writes
// exactly one chosen layer atomically. The confirmation view reuses these
// same reason decisions instead of re-deriving policy.
func (m Model) evaluateBulkActions(bm bulkModalState) (acts []toggleAction, skipReasons map[string]int, err error) {
	skipReasons = map[string]int{}
	skip := func(reason string) { skipReasons[reason]++ }
	writeRoot := m.workspace.RepoRoot()
	if bm.scope == scopeLocal || bm.scope == scopeLocalExclude {
		writeRoot = m.workspace.LocalConfigDir()
	}
	targetPaths := map[string]bool{}
	for _, r := range m.sectionMembers(bm.sectionPath) {
		if reason := m.bulkSkipReason(r); reason != "" {
			skip(shortBulkReason(reason))
			continue
		}
		s, reason := bulkScopeForRow(r, bm.scope)
		if reason != "" {
			skip(shortBulkReason(reason))
			continue
		}
		act := scopedToggleAction(r, s)
		// act.enable reports the flip the row would perform, so the row's
		// current disabled state is the inverse of act.disablesResource().
		// Rows already in the requested state are skipped.
		wantDisable := bm.direction == bulkDirDisable
		if !act.disablesResource() == wantDisable {
			skip(bulkNoopReason(s, wantDisable))
			continue
		}
		switch s {
		case scopeLocalExclude, scopeUserExclude:
			act.enable = wantDisable
		default:
			act.enable = !wantDisable
		}
		// Enabling a colliding resource in bulk cannot run the single-row
		// competing-exclusion follow-up, so refuse before publication rather
		// than silently installing competing resources.
		if act.enable && !act.isMarketplace && (act.scope == scopeGlobal || act.scope == scopeProject) && hasCollisionError(m.errs, act.kind, act.skillName) {
			return nil, skipReasons, fmt.Errorf("enabling %s %q collides with another configured source; toggle it individually so competing exclusions stay intact", skill.KindLabel(act.kind), act.skillName)
		}
		if err := validateToggleRegistryWrite(m.rows, m.errs, act); err != nil {
			return nil, skipReasons, err
		}
		if act.isMarketplace && act.scope != scopeUserExclude {
			if !act.hasMarketplaceSelection {
				skip("no direct installation pin")
				continue
			}
			if !act.enable && !act.marketplaceSelection.Pin.Direct && len(act.marketplaceSelection.Pin.SelectedBy.List()) > 0 {
				skip("collection-selected")
				continue
			}
		}
		path, pathErr := bulkTargetPath(writeRoot, act.scope)
		if pathErr != nil {
			return nil, skipReasons, pathErr
		}
		targetPaths[path] = true
		acts = append(acts, act)
	}
	if len(targetPaths) > 1 {
		return nil, skipReasons, fmt.Errorf("section targets span multiple config layers; choose a narrower section")
	}
	return acts, skipReasons, nil
}

// bulkNoopReason describes a row skipped because it is already in the
// requested state on the resolved scope, so the confirmation never calls a
// no-op eligible.
func bulkNoopReason(s scope, wantDisable bool) string {
	state := "enabled"
	if wantDisable {
		state = "disabled"
	}
	if s == scopeLocalExclude || s == scopeUserExclude {
		if wantDisable {
			return "already excluded (" + scopeName(s) + ")"
		}
		return "not excluded (" + scopeName(s) + ")"
	}
	return "already " + state + " (" + scopeName(s) + ")"
}

// shortBulkReason strips a detail suffix (e.g. "unavailable: gone") so
// confirmation counts group by reason category.
func shortBulkReason(reason string) string {
	if i := strings.IndexByte(reason, ':'); i >= 0 {
		return reason[:i]
	}
	return reason
}

// bulkActions is the count-shaped wrapper over evaluateBulkActions used by
// the submit paths that only need action and skip totals.
func (m Model) bulkActions(bm bulkModalState) (acts []toggleAction, targeted, skipped int, err error) {
	acts, reasons, err := m.evaluateBulkActions(bm)
	for _, n := range reasons {
		skipped += n
	}
	return acts, len(acts), skipped, err
}

// resolvedBulkScope is the scope actually written by the evaluated actions:
// an exclude choice may resolve per row to the user exclusion layer.
func resolvedBulkScope(bm bulkModalState, acts []toggleAction) scope {
	if len(acts) > 0 {
		return acts[0].scope
	}
	return bm.scope
}

// bulkTargetPath resolves the authored config path a bulk action writes to,
// mirroring scopedToggleConfigPath plus the user-exclusion global file.
func bulkTargetPath(writeRoot string, s scope) (string, error) {
	if s == scopeUserExclude {
		return config.GlobalConfigPath()
	}
	return scopedToggleConfigPath(writeRoot, s)
}

func bulkLayerSource(s scope) config.Source {
	switch s {
	case scopeGlobal, scopeUserExclude:
		return config.UserPersonal
	case scopeProject:
		return config.RepoCommitted
	default:
		return config.RepoPersonal
	}
}

// runBulkDependencyToggle applies all resolved actions to their single target
// layer inside one dependency mutation transaction, so a failing action or a
// failed publish leaves every byte unchanged. Without the serialized mutation
// seam the batch fails closed; it never falls back to sequential publishes.
func runBulkDependencyToggle(ctx context.Context, workspace pipeline.Workspace, acts []toggleAction, mutate DependencyMutationFunc) tea.Cmd {
	return func() tea.Msg {
		// enabled describes the resource outcome of the batch, not the raw
		// config write direction: an exclude action disables the resource.
		enabled := false
		label := fmt.Sprintf("%d resources", len(acts))
		if len(acts) > 0 {
			enabled = !acts[0].disablesResource()
		}
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return configWrittenMsg{skillName: label, enabled: enabled, err: err}
		}
		writeRoot := ws.RepoRoot()
		if len(acts) > 0 && (acts[0].scope == scopeLocal || acts[0].scope == scopeLocalExclude) {
			writeRoot = ws.LocalConfigDir()
		}
		for _, act := range acts {
			if !act.isMarketplace && act.scope != scopeLocalExclude && strings.TrimSpace(act.registryURL) == "" {
				return configWrittenMsg{skillName: label, enabled: enabled, err: fmt.Errorf("resource %q has no source registry to toggle", act.skillName)}
			}
		}
		if mutate == nil {
			return configWrittenMsg{skillName: label, enabled: enabled, err: fmt.Errorf("section bulk toggle requires the serialized config mutation seam")}
		}
		destPath := ""
		var destSource config.Source
		userExcludes := false
		for _, act := range acts {
			path, err := bulkTargetPath(writeRoot, act.scope)
			if err != nil {
				return configWrittenMsg{skillName: label, enabled: enabled, err: err}
			}
			source := bulkLayerSource(act.scope)
			if destPath == "" {
				destPath, destSource = path, source
				userExcludes = act.scope == scopeUserExclude
				continue
			}
			if path != destPath || source != destSource {
				return configWrittenMsg{skillName: label, enabled: enabled, err: fmt.Errorf("section targets span multiple config layers; choose a narrower section")}
			}
		}
		err = mutate(ctx, ws, func(mapper pipeline.ConfigPathMapper) error {
			mapped, err := mapper(destPath)
			if err != nil {
				return err
			}
			if userExcludes {
				for _, act := range acts {
					selection := act.marketplaceSelection
					if err := config.SetUserMarketplaceExclusion(mapped, selection.Entry.URL, selection.Pin.RegistrySlug, act.kind, selection.Pin.Name, act.enable); err != nil {
						return err
					}
				}
				return nil
			}
			return pipeline.EditLayerAtPath(config.LayerTarget{Source: destSource, StagedPath: mapped, DeclaringPath: destPath}, func(m *pipeline.LayerMutation) error {
				for _, act := range acts {
					if err := applyToggleAction(m, act); err != nil {
						return err
					}
				}
				return nil
			})
		})
		return configWrittenMsg{skillName: label, enabled: enabled, err: err}
	}
}

// startBulkOperation re-evaluates targets and guards at submit time, emits the
// privacy-safe action span, and serializes the batch through the shared
// operation lifecycle.
func (m Model) startBulkOperation() (tea.Model, tea.Cmd) {
	bm := m.bulkModal
	if m.loading || m.syncing {
		return m, operationBlockedCmd()
	}
	if m.configInvalid {
		return m, invalidConfigToastCmd()
	}
	acts, targeted, skipped, err := m.bulkActions(bm)
	if err != nil {
		return m, func() tea.Msg {
			return toastMsg{text: err.Error(), duration: 5e9}
		}
	}
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	recordActionSpan(m.ctx, "bulk-toggle", map[string]string{
		"grouping.primary":   string(m.viewSettings.normalized().GroupBy),
		"grouping.secondary": string(m.viewSettings.normalized().ThenBy),
		"scope":              scopeName(resolvedBulkScope(bm, acts)),
		"direction":          bm.direction.label(),
		"count.targeted":     strconv.Itoa(targeted),
		"count.skipped":      strconv.Itoa(skipped),
	})
	m.bulkModal = bulkModalState{}
	if len(acts) == 0 {
		return m, correlateOperation(func() tea.Msg {
			return configWrittenMsg{skillName: "section", enabled: bm.direction == bulkDirEnable, err: fmt.Errorf("no eligible resources in section")}
		}, operation, false)
	}
	return m, correlateOperation(runBulkDependencyToggle(ctxOrBackground(m.ctx), m.workspace, acts, m.dependencyLifecycle.Mutate), operation, false)
}

// handleBulkToggleKey owns keys while the bulk modal is open: g/p/l/e pick a
// scope, + and - pick a direction, enter confirms, esc cancels at any stage.
func (m Model) handleBulkToggleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Clear) {
		m.pane = paneList
		m.bulkModal = bulkModalState{}
		return m, nil
	}
	switch m.bulkModal.stage {
	case bulkStageScope:
		s, ok := bulkScopeForKey(msg)
		if !ok {
			return m, nil
		}
		if !m.bulkScopeAvailable(s) {
			return m, func() tea.Msg {
				return toastMsg{text: "that scope is unavailable here", duration: 2e9}
			}
		}
		m.bulkModal.scope = s
		m.bulkModal.scopeSet = true
		m.bulkModal.stage = bulkStageDirection
		return m, nil
	case bulkStageDirection:
		switch msg.Code {
		case '+', '=':
			m.bulkModal.direction = bulkDirEnable
		case '-', '_':
			m.bulkModal.direction = bulkDirDisable
		default:
			return m, nil
		}
		m.bulkModal.stage = bulkStageConfirm
		m.refreshBulkConfirmation()
		return m, nil
	case bulkStageConfirm:
		if key.Matches(msg, m.keys.SearchConfirm) || msg.Code == 'y' {
			// Re-evaluate the displayed target set; a membership or
			// eligibility change refreshes the confirmation instead of
			// committing a stale set.
			acts, _, skipped, err := m.bulkActions(m.bulkModal)
			if err != nil || skipped != m.bulkModal.confirmedSkipped || !sameToggleActions(acts, m.bulkModal.confirmed) {
				m.refreshBulkConfirmation()
				return m, nil
			}
			return m.startBulkOperation()
		}
		return m, nil
	}
	return m, nil
}

// refreshBulkConfirmation captures the current resolved target set as the one
// displayed at the confirmation stage.
func (m *Model) refreshBulkConfirmation() {
	acts, _, skipped, err := m.bulkActions(m.bulkModal)
	m.bulkModal.confirmed = append([]toggleAction(nil), acts...)
	m.bulkModal.confirmedSkipped = skipped
	m.bulkModal.confirmErr = err
}

// sameToggleActions compares two resolved action sets for identity equality.
func sameToggleActions(a, b []toggleAction) bool {
	if len(a) != len(b) {
		return false
	}
	return len(a) == 0 || reflect.DeepEqual(a, b)
}

// renderBulkToggleModal shows the section, counts, skips, filter, and the
// current stage's choices inside the shared bounded frame.
func renderBulkToggleModal(m Model) string {
	t := m.currentTheme()
	bm := m.bulkModal
	title := titleStyle(t).Render("Toggle all in " + sanitizeTerminalData(bm.sectionTitle, terminalSingleLine))

	members := m.sectionMembers(bm.sectionPath)
	var skipReasons map[string]int
	var acts []toggleAction
	eligible := 0
	if bm.stage == bulkStageConfirm {
		// Reuse the submitted evaluation so confirmation skip reasons and
		// no-ops are the same decisions the write path makes.
		acts, skipReasons, _ = m.evaluateBulkActions(bm)
		eligible = len(acts)
	} else {
		// Before a direction is chosen there is no direction-specific
		// no-op claim: report base and per-scope eligibility only.
		skipReasons = map[string]int{}
		for _, r := range members {
			reason := m.bulkSkipReason(r)
			if reason == "" && bm.scopeSet {
				_, reason = bulkScopeForRow(r, bm.scope)
			}
			if reason != "" {
				skipReasons[shortBulkReason(reason)]++
				continue
			}
			eligible++
		}
	}
	skipped := 0
	for _, n := range skipReasons {
		skipped += n
	}

	var body []string
	if m.filter != "" {
		total := len(m.sectionMembersIn(m.rows, bm.sectionPath))
		body = append(body, fmt.Sprintf("%d matching of %d in section · %d eligible · %d skipped", len(members), total, eligible, skipped))
	} else {
		body = append(body, fmt.Sprintf("%d matching · %d eligible · %d skipped", len(members), eligible, skipped))
	}
	onGlobal, onProject, onLocal, excluded := 0, 0, 0, 0
	for _, r := range members {
		if r.EnabledGlobal {
			onGlobal++
		}
		if r.EnabledProject {
			onProject++
		}
		if r.EnabledLocal {
			onLocal++
		}
		if r.ExcludedLocally || r.ExcludedByUser {
			excluded++
		}
	}
	off := len(members)
	body = append(body, fmt.Sprintf("on/off: g%d/%d p%d/%d l%d/%d · excluded %d · current rows only",
		onGlobal, off-onGlobal, onProject, off-onProject, onLocal, off-onLocal, excluded))
	if m.filter != "" {
		body = append(body, "filter: "+sanitizeTerminalData(m.filter, terminalSingleLine))
	}
	if len(skipReasons) > 0 {
		reasons := make([]string, 0, len(skipReasons))
		for reason := range skipReasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		var parts []string
		for _, reason := range reasons {
			parts = append(parts, fmt.Sprintf("%d %s", skipReasons[reason], reason))
		}
		body = append(body, dimTextStyle(t).Render("skipped: "+strings.Join(parts, ", ")))
	}

	var hints []footerHint
	switch bm.stage {
	case bulkStageScope:
		body = append(body, "", markKeyStyle(t).Render("[g]")+" global  "+markKeyStyle(t).Render("[p]")+" project  "+markKeyStyle(t).Render("[l]")+" local  "+markKeyStyle(t).Render("[e]")+" exclude")
		hints = []footerHint{{"esc", "cancel"}}
	case bulkStageDirection:
		body = append(body, "", fmt.Sprintf("scope: %s", scopeName(bm.scope)),
			markKeyStyle(t).Render("[+]")+" enable  "+markKeyStyle(t).Render("[-]")+" disable")
		hints = []footerHint{{"esc", "cancel"}}
	case bulkStageConfirm:
		if bm.confirmErr != nil {
			body = append(body, "", sanitizeTerminalData(bm.confirmErr.Error(), terminalSingleLine))
		} else {
			body = append(body, "", fmt.Sprintf("%s %d resources in the %s layer (%d skipped)",
				bm.direction.label(), len(bm.confirmed), scopeName(resolvedBulkScope(bm, bm.confirmed)), bm.confirmedSkipped))
		}
		hints = []footerHint{{"enter", "confirm"}, {"esc", "cancel"}}
	}
	return modalFrame(m, t.accent, title, body, hints)
}
