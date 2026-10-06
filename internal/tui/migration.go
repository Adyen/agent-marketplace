package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/Adyen/agent-marketplace/internal/session"
)

type migrationDoneMsg struct {
	count int
	err   error
}

func (m Model) runMigrationCmd() tea.Cmd {
	return func() tea.Msg {
		count, err := session.RunMigration(context.Background(), m.migrationPrompt)
		return migrationDoneMsg{count: count, err: err}
	}
}

// renderMigrationModal leads with what was found and the decision, so the
// explanatory detail is what drops first on short terminals.
func renderMigrationModal(m Model) string {
	t := m.currentTheme()
	title := warnTitleStyle(t).Render("Configuration migration required")
	body := []string{"Legacy configuration found:"}
	for _, cfg := range m.migrationPrompt.Configs {
		body = append(body, fmt.Sprintf("  • %s: %s", cfg.Label, shortenPath(cfg.Path, m.homeDir, modalInnerWidth(m)-6-len(cfg.Label))))
	}
	body = append(body, "", "Migrate it to agent-marketplace/v4? Existing v2 and v3 configurations remain readable if you leave them unchanged.")
	m.ensureMigrationForm()
	if m.migrationForm != nil {
		body = append(body, "", m.migrationForm.View())
	}
	dim := dimTextStyle(t)
	body = append(body, "",
		dim.Render("Equivalent command:  agent-marketplace migrate --scope user"),
		dim.Render("Migrated root:       apiVersion: agent-marketplace/v4"),
		dim.Render("The migration preserves source selections and layer policies, but regenerates canonical YAML. It is transactional; unsupported or lossy inputs are left unchanged."))
	return modalFrame(m, t.warn, title, body, []footerHint{{"enter/m", "migrate"}, {"l/esc", "leave unchanged"}})
}

func (m Model) handleMigrationModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureMigrationForm()
	if key.Matches(msg, m.keys.MigrationLeave) {
		return m.leaveMigration()
	}
	m.migrationForm = updateModalForm(m.migrationForm, msg)
	if m.migrationForm != nil && m.migrationForm.State == huh.StateCompleted {
		if m.migrationValue != nil && *m.migrationValue {
			m.pane = paneList
			m.migrationForm = nil
			m.migrationValue = nil
			return m, m.runMigrationCmd()
		}
		return m.leaveMigration()
	}
	return m, nil
}

func (m Model) leaveMigration() (tea.Model, tea.Cmd) {
	m.pane = paneList
	m.migrationForm = nil
	m.migrationValue = nil
	m.migrationDismissed = true
	m.pushToast("Config left unchanged", 2*time.Second)
	return m, tea.Batch(m.beginMarketplaceDiscovery(m.filter, false), toastExpiryCmd(2*time.Second))
}
