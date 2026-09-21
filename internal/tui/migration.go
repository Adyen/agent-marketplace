package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func pendingMigrationFor(repoRoot, homeDir string) migrationPrompt {
	paths := config.MigratePaths{
		Repo: config.ProjectConfigPath(repoRoot),
	}
	if homeDir != "" {
		paths.User = filepath.Join(homeDir, ".agents", "config.yaml")
	}
	paths.Local = config.LocalConfigPath(config.FindLocalConfigDir())
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

var styleMigrationModal = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"}).
	Background(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"}).
	Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"}).
	Padding(1, 2)

func renderMigrationModal(m Model) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"}).
		Render("Configuration migration required")

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"})

	dim := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString("This configuration can be migrated to agent-marketplace/v4.\n")
	b.WriteString("Existing v2 and v3 configurations remain readable if you decline.\n")
	b.WriteString("The migrated root declares:\n```yaml\napiVersion: agent-marketplace/v4\n```\n\n")
	b.WriteString("Runs the in-process equivalent of:\n")
	b.WriteString("```sh\nagent-marketplace migrate\n```\n\n")
	b.WriteString("The migration preserves source selections and layer policies, but regenerates canonical YAML.\n\n")
	b.WriteString("Detected legacy configuration in:\n")
	for _, cfg := range m.migrationPrompt.configs {
		fmt.Fprintf(&b, "  • %s: %s\n", cfg.label, cfg.path)
	}
	b.WriteString("\n")
	b.WriteString(dim.Render("Migration is transactional; unsupported or lossy inputs are left unchanged."))
	b.WriteString("\n\n")
	b.WriteString("  " + keyStyle.Render("[enter]") + " or " + keyStyle.Render("[m]") + "  migrate to v4\n")
	b.WriteString("  " + keyStyle.Render("[l]") + " or " + keyStyle.Render("[esc]") + "  leave config unchanged")

	return styleMigrationModal.Render(b.String())
}

func (m Model) handleMigrationModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.MigrationRun):
		m.pane = paneList
		return m, m.runMigrationCmd()
	case key.Matches(msg, m.keys.MigrationLeave):
		m.pane = paneList
		m.migrationDismissed = true
		m.toast = "Config left unchanged"
		m.toastExp = time.Now().Add(2 * time.Second)
		return m, tea.Batch(m.beginMarketplaceDiscovery(m.filter, false), toastExpiryCmd(2*time.Second))
	}
	return m, nil
}
