package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Adyen/agent-marketplace/internal/config"
)

type migrationConfig struct {
	label string
	path  string
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
	candidates := []migrationConfig{
		{label: "Project", path: config.ProjectConfigPath(repoRoot)},
	}
	if homeDir != "" {
		candidates = append(candidates, migrationConfig{
			label: "Global",
			path:  filepath.Join(homeDir, ".agents", "config.yaml"),
		})
	}
	localDir := config.FindLocalConfigDir()
	if localDir != "" && localDir != repoRoot {
		candidates = append(candidates, migrationConfig{
			label: "Local",
			path:  config.LocalConfigPath(localDir),
		})
	}

	var configs []migrationConfig
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		path := filepath.Clean(candidate.path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if !configPathNeedsMigration(path) {
			continue
		}
		configs = append(configs, migrationConfig{
			label: candidate.label,
			path:  path,
		})
	}
	return migrationPrompt{configs: configs}
}

// configPathNeedsMigration is retained for compatibility with the migration
// helpers, but schema versions are no longer a reason for the TUI to prompt.
// Structural remediation remains available through the explicit migrate
// command.
func configPathNeedsMigration(path string) bool {
	_ = path
	return false
}

func (m Model) runMigrationCmd() tea.Cmd {
	return func() tea.Msg {
		paths := migrationPathsForPrompt(m.migrationPrompt, m.repoRoot, m.homeDir)
		result, err := config.MigrateConfigFiles(paths, config.DefaultHarnesses())
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

func migrationPathsForPrompt(prompt migrationPrompt, repoRoot, homeDir string) config.MigratePaths {
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := config.ProjectConfigPath(repoRoot)
	localPath := config.LocalConfigPath(config.FindLocalConfigDir())

	paths := config.MigratePaths{}
	for _, cfg := range prompt.configs {
		switch cfg.path {
		case userPath:
			paths.User = cfg.path
		case repoPath:
			paths.Repo = cfg.path
		case localPath:
			paths.Local = cfg.path
		}
	}

	// The migrate command may need to create a new user config (for example,
	// when a repo v2 config supplies the target harness set). Only include the
	// user path when it is missing, v2, or v3 so migration does not choke on a
	// v1/empty/unknown file that was not shown in the modal.
	if paths.User == "" && homeDir != "" && migrationUserPathSafe(userPath) {
		paths.User = userPath
	}

	return paths
}

func migrationUserPathSafe(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return os.IsNotExist(err)
	}
	v := config.PeekAPIVersion(data)
	return v == config.APIVersionV2 || v == config.APIVersionV3
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
		Render("Config remediation required")

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"})

	dim := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	defaultHarnesses := config.DefaultHarnesses()
	harnessesFlag := strings.Join(defaultHarnesses, ",")
	harnessesYAML := ""
	for _, h := range defaultHarnesses {
		harnessesYAML += fmt.Sprintf("  - %s\n", h)
	}

	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString("This config has structural issues that must be fixed.\n")
	b.WriteString("Valid agent-marketplace/v3 files are accepted as a compatibility alias.\n\n")
	b.WriteString("Runs the in-process equivalent of:\n")
	fmt.Fprintf(&b, "```sh\nagent-marketplace migrate --harnesses %s\n```\n\n", harnessesFlag)
	b.WriteString("Adds or updates these top-level lines before your existing config:\n")
	fmt.Fprintf(&b, "```yaml\napiVersion: agent-marketplace/v2\nharnesses:\n%s```\n\n", harnessesYAML)
	b.WriteString("Existing registries, selected skills, excludes, and comments are preserved.\n\n")
	b.WriteString("Detected pending remediation in:\n")
	for _, cfg := range m.migrationPrompt.configs {
		fmt.Fprintf(&b, "  • %s: %s\n", cfg.label, cfg.path)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s", dim.Render(fmt.Sprintf("Running remediation uses the same default as `agent-marketplace migrate`: harnesses: %v.", defaultHarnesses)))
	b.WriteString("\n\n")
	b.WriteString("  " + keyStyle.Render("[enter]") + " or " + keyStyle.Render("[m]") + "  run remediation\n")
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
		m.toast = "Config left unchanged"
		m.toastExp = time.Now().Add(2 * time.Second)
		return m, nil
	}
	return m, nil
}
