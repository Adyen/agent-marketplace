package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

type migrationConfig struct {
	label string
	path  string
	scope config.V4MigrationScope
}

type migrationPrompt struct {
	configs []migrationConfig
}

type migrationDoneMsg struct {
	count int
	err   error
}

func (p migrationPrompt) hasConfigs() bool {
	return len(p.configs) > 0
}

// pendingMigrationFor reports legacy configs the startup migration modal may
// offer. Startup migration only migrates the user config; repository and local
// configs are left unchanged until migrated deliberately. The repoRoot and
// localDir parameters keep the validated Workspace roots available at this seam.
func pendingMigrationFor(repoRoot, localDir, homeDir string) migrationPrompt {
	paths := config.MigratePaths{}
	if homeDir != "" {
		paths.User = filepath.Join(homeDir, ".agents", "config.yaml")
	}
	candidates, err := config.FindV4MigrationCandidates(paths)
	if err != nil {
		return migrationPrompt{}
	}
	labels := map[config.V4MigrationScope]string{
		config.V4MigrationUser:  "Global",
		config.V4MigrationRepo:  "Project",
		config.V4MigrationLocal: "Local",
	}
	configs := make([]migrationConfig, 0, len(candidates))
	for _, candidate := range candidates {
		configs = append(configs, migrationConfig{
			label: labels[candidate.Scope],
			path:  candidate.Path,
			scope: candidate.Scope,
		})
	}
	return migrationPrompt{configs: configs}
}

func (m Model) runMigrationCmd() tea.Cmd {
	return func() tea.Msg {
		paths := migrationPathsForPrompt(m.migrationPrompt)
		result, err := config.MigrateConfigFilesToV4WithEvidence(context.Background(), paths, config.V4MigrationAll, false, marketplace.DefaultCache())
		count := 0
		if result != nil {
			if result.UserMigrated {
				count++
			}
			if result.RepoMigrated {
				count++
			}
			if result.LocalMigrated {
				count++
			}
		}
		return migrationDoneMsg{count: count, err: err}
	}
}

func migrationPathsForPrompt(prompt migrationPrompt) config.MigratePaths {
	paths := config.MigratePaths{}
	for _, cfg := range prompt.configs {
		switch cfg.scope {
		case config.V4MigrationUser:
			paths.User = cfg.path
		case config.V4MigrationRepo:
			paths.Repo = cfg.path
		case config.V4MigrationLocal:
			paths.Local = cfg.path
		}
	}
	return paths
}

// renderMigrationModal leads with what was found and the decision, so the
// explanatory detail is what drops first on short terminals.
func renderMigrationModal(m Model) string {
	t := m.currentTheme()
	title := warnTitleStyle(t).Render("Configuration migration required")
	body := []string{"Legacy configuration found:"}
	for _, cfg := range m.migrationPrompt.configs {
		body = append(body, fmt.Sprintf("  • %s: %s", cfg.label, shortenPath(cfg.path, m.homeDir, modalInnerWidth(m)-6-len(cfg.label))))
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
