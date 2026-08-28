package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// scope identifies which configuration file the toggle modal is acting on.
type scope int

const (
	scopeGlobal scope = iota
	scopeProject
	scopeLocal
	scopeLocalExclude
)

// toggleAction describes the direction of a toggle (add vs remove) for a
// given (skill, scope) pair, derived from the row's current state.
type toggleAction struct {
	skillName    string
	kind         ir.ResourceKind
	scope        scope
	enable       bool   // true = add to config; false = remove from config
	registryURL  string // for project/global scopes: which registry entry to target
	registryRef  string // ref of the source registry (for EnsureRegistryInConfig fallback)
	registryPath string // path subdirectory of the source registry (for EnsureRegistryWithPathsInConfig fallback)
}

// scopedToggleAction returns the action (add or remove) for pressing the
// given scope key on a modal. The decision is driven entirely by the row's
// current flags:
//   - scopeGlobal: enable when !EnabledGlobal, disable otherwise.
//   - scopeProject: enable when !EnabledProject, disable otherwise.
//   - scopeLocal: enable when !EnabledLocal, disable otherwise.
//   - scopeLocalExclude: add exclude when !ExcludedLocally, remove otherwise.
func scopedToggleAction(r row, s scope) toggleAction {
	act := toggleAction{
		skillName:    r.Artifact.Name,
		kind:         r.Artifact.Kind,
		scope:        s,
		registryURL:  r.RegistryURL,
		registryRef:  r.RegistryRef,
		registryPath: r.RegistryPath,
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
	}
	return act
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
func runScopedToggle(repoRoot string, act toggleAction) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch act.scope {
		case scopeGlobal:
			globalPath, pathErr := config.GlobalConfigPath()
			if pathErr != nil {
				err = pathErr
				break
			}
			if act.enable {
				if err := config.EnsureRegistryWithPathsInConfig(globalPath, act.registryURL, act.registryRef, act.registryPath); err != nil {
					return configWrittenMsg{
						skillName: act.skillName,
						enabled:   false,
						err:       fmt.Errorf("ensure registry in global config: %w", err),
					}
				}
				err = config.AddResourceToRegistryInConfig(globalPath, act.registryURL, act.registryPath, act.kind, act.skillName)
			} else {
				err = config.DisableResourceWithShadowFallback(globalPath, act.registryURL, act.registryPath, act.registryRef, act.kind, act.skillName)
			}
		case scopeProject:
			projectPath := config.ProjectConfigPath(repoRoot)
			if act.enable {
				if err := config.EnsureRegistryWithPathsInConfig(projectPath, act.registryURL, act.registryRef, act.registryPath); err != nil {
					return configWrittenMsg{
						skillName: act.skillName,
						enabled:   false,
						err:       fmt.Errorf("ensure registry in project config: %w", err),
					}
				}
				err = config.AddResourceToRegistryInConfig(projectPath, act.registryURL, act.registryPath, act.kind, act.skillName)
			} else {
				err = config.DisableResourceWithShadowFallback(projectPath, act.registryURL, act.registryPath, act.registryRef, act.kind, act.skillName)
			}
		case scopeLocal:
			localPath := config.LocalConfigPath(repoRoot)
			if act.enable {
				// Write the include scoped to the registry entry (creating a
				// local shadow entry when the registry lives in a lower layer),
				// mirroring the project/global scopes so local selections are
				// registry-scoped too rather than a top-level skills.include.
				if err := config.EnsureRegistryWithPathsInConfig(localPath, act.registryURL, act.registryRef, act.registryPath); err != nil {
					return configWrittenMsg{
						skillName: act.skillName,
						enabled:   false,
						err:       fmt.Errorf("ensure registry in local config: %w", err),
					}
				}
				err = config.AddResourceToRegistryInConfig(localPath, act.registryURL, act.registryPath, act.kind, act.skillName)
			} else {
				err = config.DisableResourceWithShadowFallback(localPath, act.registryURL, act.registryPath, act.registryRef, act.kind, act.skillName)
			}
		case scopeLocalExclude:
			localPath := config.LocalConfigPath(repoRoot)
			if act.enable {
				if strings.TrimSpace(act.registryURL) == "" {
					// No registry context (shouldn't normally happen): fall back
					// to the top-level personal exclude.
					err = config.AddKindExcludeToLocalConfig(repoRoot, act.kind, act.skillName)
					break
				}
				// Suppress locally, registry-scoped. The exclude is written under
				// the providing registry entry (creating a shadow entry if
				// needed) rather than a top-level personal exclude, and any
				// matching local include is removed so the entry never holds a
				// contradictory include+exclude pair. Unlike toggling the local
				// include off ([l]), the exclude is always written so the
				// resource is suppressed even when a lower layer's default
				// re-selects it.
				if e := config.EnsureRegistryWithPathsInConfig(localPath, act.registryURL, act.registryRef, act.registryPath); e != nil {
					return configWrittenMsg{
						skillName: act.skillName,
						enabled:   false,
						err:       fmt.Errorf("ensure registry in local config: %w", e),
					}
				}
				if e := config.RemoveResourceFromRegistryInConfig(localPath, act.registryURL, act.registryPath, act.kind, act.skillName); e != nil {
					err = e
					break
				}
				err = config.AddKindExcludeToRegistryInConfig(localPath, act.registryURL, act.registryPath, act.kind, act.skillName)
			} else {
				// Remove the exclude wherever it lives: the registry entry and
				// any legacy top-level personal exclude.
				if act.registryURL != "" {
					if e := config.RemoveKindExcludeFromRegistryInConfig(localPath, act.registryURL, act.registryPath, act.kind, act.skillName); e != nil {
						err = e
						break
					}
				}
				err = config.RemoveKindExcludeFromLocalConfig(repoRoot, act.kind, act.skillName)
			}
		}
		return configWrittenMsg{
			skillName: act.skillName,
			enabled:   act.enable,
			err:       err,
		}
	}
}

// excludeCompetingRegistriesCmd returns a tea.Cmd that adds excludes for
// competing registry URLs that provide the same resource name. This resolves
// name collisions when the user toggles ON a colliding skill.
func excludeCompetingRegistriesCmd(repoRoot string, rows []row, act toggleAction) tea.Cmd {
	competing := competingRegistryURLs(rows, act.kind, act.skillName, act.registryURL)
	if len(competing) == 0 {
		return nil
	}
	// Also collect per-competing-URL metadata (ref, path) so we can ensure
	// the registry entry exists in the target config file before adding the
	// exclude. Without this, the exclude write silently fails when the
	// competing registry only exists in a different scope's config file.
	competingMeta := make(map[string]struct{ ref, path string })
	for _, r := range rows {
		if r.Artifact.Kind != act.kind || r.Artifact.Name != act.skillName || r.RegistryURL == act.registryURL {
			continue
		}
		if _, dup := competingMeta[r.RegistryURL]; dup {
			continue
		}
		competingMeta[r.RegistryURL] = struct{ ref, path string }{ref: r.RegistryRef, path: r.RegistryPath}
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
		for _, url := range competing {
			meta, ok := competingMeta[url]
			if ok {
				// Ensure the competing registry entry exists in the target
				// config file so the exclude write finds a matching entry.
				if err := config.EnsureRegistryWithPathsInConfig(configPath, url, meta.ref, meta.path); err != nil {
					return configWrittenMsg{
						skillName: act.skillName,
						enabled:   false,
						err:       fmt.Errorf("ensure competing registry %q: %w", url, err),
					}
				}
			}
			competingPath := ""
			if ok {
				competingPath = meta.path
			}
			if err := config.AddKindExcludeToRegistryInConfig(configPath, url, competingPath, act.kind, act.skillName); err != nil {
				return configWrittenMsg{
					skillName: act.skillName,
					enabled:   false,
					err:       fmt.Errorf("exclude %s %q in competing registry %q: %w", skill.KindLabel(act.kind), act.skillName, url, err),
				}
			}
		}
		return configWrittenMsg{skillName: act.skillName, enabled: true}
	}
}

// styleToggleModal is the bordered floating-box style for paneToggleModal.
// Uses a rounded border + padding + opaque background so it reads clearly
// on top of the main two-pane layout.
var styleToggleModal = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"}).
	Background(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"}).
	Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"}).
	Padding(1, 2)

// renderToggleModal renders the toggle modal overlay for the currently
// selected skill. Shows per-scope state plus the available key actions.
func renderToggleModal(m Model) string {
	r := m.modalSkill
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"}).
		Render(fmt.Sprintf(`"%s"`, r.Artifact.Name))

	check := func(on bool) string {
		if on {
			return lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#5C8A1E", Dark: "#A6E22E"}).
				Render("✓")
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"}).
			Render("✗")
	}
	yesno := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}

	var b strings.Builder
	b.WriteString(title + "\n\n")

	// Check for name collision errors affecting this resource.
	if hasCollisionError(m.errs, r.Artifact.Kind, r.Artifact.Name) {
		warnStyle := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#CC7A00", Dark: "#FD971F"}).
			Bold(true)
		b.WriteString("  " + warnStyle.Render("⚠ Name collision — toggle may not take effect until you exclude the other source") + "\n\n")
	}

	fmt.Fprintf(&b, "  Global:        %s\n", check(r.EnabledGlobal))
	fmt.Fprintf(&b, "  Project:       %s\n", check(r.EnabledProject))
	fmt.Fprintf(&b, "  Local:         %s\n", check(r.EnabledLocal))
	fmt.Fprintf(&b, "  Local exclude: %s  (%s)\n\n", yesno(r.ExcludedLocally), skill.KindLabel(r.Artifact.Kind))

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"}).Bold(true)
	disabledKeyStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#777777"})
	disabledLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"})

	kindLabel := skill.KindLabel(r.Artifact.Kind)
	actions := []struct{ key, label string }{
		{"[g]", "toggle global"},
		{"[p]", "toggle project"},
		{"[l]", "toggle local"},
		{"[e]", fmt.Sprintf("toggle %s exclude", kindLabel)},
		{"[esc]", "cancel"},
	}
	for i, a := range actions {
		if i > 0 {
			b.WriteString("\n")
		}
		if a.key == "[e]" && r.Artifact.Source.IsMachineScope() {
			b.WriteString("  " + disabledKeyStyle.Render(a.key) + "  " + disabledLabelStyle.Render(a.label+" (use [g] to toggle at user level)"))
			continue
		}
		b.WriteString("  " + keyStyle.Render(a.key) + "  " + a.label)
	}

	return styleToggleModal.Render(b.String())
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

func (m Model) startToggleOperation(act toggleAction) (tea.Model, tea.Cmd) {
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	recordToggleActionSpan(m.ctx, act, m.modalSkill.Artifact.Kind)
	if act.enable && hasCollisionError(m.errs, m.modalSkill.Artifact.Kind, m.modalSkill.Artifact.Name) {
		m.pendingExcludeAction = &act
	}
	return m, correlateOperation(runScopedToggle(m.repoRoot, act), operation, false)
}

// handleToggleModalKey routes modal key presses. Returns (newModel, cmd).
func (m Model) handleToggleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeGlobal))
	case key.Matches(msg, m.keys.ToggleProject):
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeProject))
	case key.Matches(msg, m.keys.ToggleLocal):
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeLocal))
	case key.Matches(msg, m.keys.ToggleExclude):
		if m.modalSkill.Artifact.Source.IsMachineScope() {
			return m, func() tea.Msg {
				return toastMsg{text: "Cannot exclude org/user skill at local level — use [g] instead", duration: 2 * time.Second}
			}
		}
		return m.startToggleOperation(scopedToggleAction(m.modalSkill, scopeLocalExclude))
	}
	return m, nil
}
