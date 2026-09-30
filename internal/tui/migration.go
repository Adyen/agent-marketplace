package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

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

func renderMigrationModal(m Model) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.currentTheme().warn).
		Render("Configuration migration required")

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.currentTheme().mark)

	dim := lipgloss.NewStyle().
		Foreground(m.currentTheme().muted)

	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString("This configuration can be migrated to agent-marketplace/v4.\n")
	b.WriteString("Existing v2 and v3 configurations remain readable if you decline.\n")
	b.WriteString("The migrated root declares:\n```yaml\napiVersion: agent-marketplace/v4\n```\n\n")
	b.WriteString("Runs the in-process equivalent of:\n")
	b.WriteString("```sh\nagent-marketplace migrate --scope user\n```\n\n")
	b.WriteString("The migration preserves source selections and layer policies, but regenerates canonical YAML.\n\n")
	b.WriteString("Detected legacy configuration in:\n")
	for _, cfg := range m.migrationPrompt.configs {
		fmt.Fprintf(&b, "  • %s: %s\n", cfg.label, cfg.path)
	}
	b.WriteString("\n")
	b.WriteString(dim.Render("Migration is transactional; unsupported or lossy inputs are left unchanged."))
	b.WriteString("\n\n")
	m.ensureMigrationForm()
	if m.migrationForm != nil {
		b.WriteString(m.migrationForm.View())
		b.WriteString("\n\n")
	}
	b.WriteString("  " + keyStyle.Render("[enter]") + " or " + keyStyle.Render("[m]") + "  migrate to v4\n")
	b.WriteString("  " + keyStyle.Render("[l]") + " or " + keyStyle.Render("[esc]") + "  leave config unchanged")

	return modalBoxStyle(m.currentTheme(), lipgloss.DoubleBorder(), m.currentTheme().warn).Render(b.String())
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
			return m, m.runMigrationCmd()
		}
		return m.leaveMigration()
	}
	return m, nil
}

func (m Model) leaveMigration() (tea.Model, tea.Cmd) {
	m.pane = paneList
	m.migrationDismissed = true
	m.pushToast("Config left unchanged", 2*time.Second)
	return m, tea.Batch(m.beginMarketplaceDiscovery(m.filter, false), toastExpiryCmd(2*time.Second))
}
