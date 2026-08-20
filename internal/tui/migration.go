package tui

import (
	"errors"
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
	paths   config.MigratePaths
	plan    *config.MigrationPlan
	planErr error
}

type migrationDoneMsg struct {
	operation operationID
	count     int
	err       error
}

func (p migrationPrompt) hasConfigs() bool {
	return len(p.configs) > 0
}

func pendingMigrationFor(repoRoot, homeDir string, organizationTargets ...[]string) migrationPrompt {
	candidates := []migrationConfig{{label: "Project", path: config.ProjectConfigPath(repoRoot)}}
	if homeDir != "" {
		candidates = append(candidates, migrationConfig{label: "Global", path: filepath.Join(homeDir, ".agents", "config.yaml")})
	}
	localDir := migrationLocalConfigDir(repoRoot)
	if localDir != "" {
		candidates = append(candidates, migrationConfig{label: "Local", path: config.LocalConfigPath(localDir)})
	}

	prompt := migrationPrompt{}
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
		prompt.configs = append(prompt.configs, migrationConfig{label: candidate.label, path: path})
	}
	if !prompt.hasConfigs() {
		return prompt
	}
	prompt.paths = migrationPathsForPrompt(prompt, repoRoot, homeDir)
	var orgTargets []string
	if len(organizationTargets) > 0 {
		orgTargets = organizationTargets[0]
	}
	prompt.plan, prompt.planErr = config.PlanConfigMigration(prompt.paths, nil, orgTargets)
	if errors.Is(prompt.planErr, config.ErrHarnessesRequired) {
		prompt.plan, prompt.planErr = config.PlanConfigMigration(prompt.paths, config.DefaultHarnesses(), orgTargets)
	}
	return prompt
}

// configPathNeedsMigration is retained for compatibility with the migration
// helpers, but schema versions are no longer a reason for the TUI to prompt.
// Structural remediation remains available through the explicit migrate
// command.
func configPathNeedsMigration(path string) bool {
	_ = path
	return false
}

// runMigrationCmd runs the migration plan as one ID-correlated operation.
// External-writer protection stays with the migration engine itself:
// ExecuteMigrationPlan verifies source digests and holds the migration locks
// (see internal/config/migrate.go), so nothing here needs its own locking.
func (m Model) runMigrationCmd(operation operationID) tea.Cmd {
	return func() tea.Msg {
		if m.migrationPrompt.planErr != nil {
			return migrationDoneMsg{operation: operation, err: m.migrationPrompt.planErr}
		}
		plan := m.migrationPrompt.plan
		if plan == nil {
			paths := migrationPathsForPrompt(m.migrationPrompt, m.repoRoot, m.homeDir)
			var err error
			plan, err = config.PlanConfigMigration(paths, config.DefaultHarnesses(), nil)
			if err != nil {
				return migrationDoneMsg{operation: operation, err: err}
			}
		}
		result, err := config.ExecuteMigrationPlan(plan)
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
		return migrationDoneMsg{operation: operation, count: count, err: err}
	}
}

func migrationPathsForPrompt(prompt migrationPrompt, repoRoot, homeDir string) config.MigratePaths {
	if prompt.paths.User != "" || prompt.paths.Repo != "" || prompt.paths.Local != "" {
		return prompt.paths
	}
	userPath := ""
	if homeDir != "" {
		userPath = filepath.Join(homeDir, ".agents", "config.yaml")
	}
	repoPath := config.ProjectConfigPath(repoRoot)
	localDir := migrationLocalConfigDir(repoRoot)
	localPath := config.LocalConfigPath(localDir)

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

func migrationLocalConfigDir(repoRoot string) string {
	localDir := config.FindLocalConfigDir()
	if localDir == "" || repoRoot == "" {
		return repoRoot
	}
	rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(localDir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return repoRoot
	}
	return localDir
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

	plan := m.migrationPrompt.plan
	if compactTerminalLayout(m) || m.height < 30 {
		var b strings.Builder
		b.WriteString(title + "\n")
		if m.migrationPrompt.planErr != nil {
			fmt.Fprintf(&b, "Plan error: %s\n", sanitizeTerminalData(m.migrationPrompt.planErr.Error(), terminalSingleLine))
		} else if plan != nil {
			fmt.Fprintf(&b, "Targets: %s (%s)\n", sanitizeTerminalData(strings.Join(plan.TargetHarnesses, ","), terminalSingleLine), plan.TargetSource)
			b.WriteString("Writes:\n")
			for _, file := range plan.Files {
				fmt.Fprintf(&b, "  %s %s\n", migrationPlanAction(file), migrationLayerLabel(file.Layer))
			}
		}
		b.WriteString(keyStyle.Render("[m]") + "/" + keyStyle.Render("[enter]") + " migrate\n")
		b.WriteString(keyStyle.Render("[l]") + "/" + keyStyle.Render("[esc]") + " leave unchanged")
		return compactModalStyle(styleMigrationModal, m).Render(b.String())
	}

	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString("This configuration needs structural remediation.\n")
	b.WriteString("agent-marketplace/v2 is canonical; v3 remains a compatibility alias.\n\n")
	if m.migrationPrompt.planErr != nil {
		fmt.Fprintf(&b, "Remediation plan could not be built:\n  %s\n\n", sanitizeTerminalData(m.migrationPrompt.planErr.Error(), terminalSingleLine))
	} else if plan != nil {
		fmt.Fprintf(&b, "Exact target set: %s\n", sanitizeTerminalData(strings.Join(plan.TargetHarnesses, ", "), terminalSingleLine))
		fmt.Fprintf(&b, "Target source: %s\n\n", plan.TargetSource)
		b.WriteString("Exact file writes:\n")
		for _, file := range plan.Files {
			fmt.Fprintf(&b, "  • %s %s: %s\n", migrationPlanAction(file), migrationLayerLabel(file.Layer), sanitizeTerminalData(file.Path, terminalSingleLine))
		}
		b.WriteString("\n")
		b.WriteString("Existing comments and valid unrelated data are preserved; unsupported policy blocks the plan before writing.\n")
		b.WriteString(dim.Render("Execution verifies every source-file digest, then locks, stages, publishes, and rolls back on commit failure."))
		b.WriteString("\n\n")
	} else {
		b.WriteString("Detected pending remediation in:\n")
		for _, pending := range m.migrationPrompt.configs {
			fmt.Fprintf(&b, "  • %s: %s\n", sanitizeTerminalData(pending.label, terminalSingleLine), sanitizeTerminalData(pending.path, terminalSingleLine))
		}
		b.WriteString("\n")
	}
	b.WriteString("  " + keyStyle.Render("[enter]") + " or " + keyStyle.Render("[m]") + "  run this plan\n")
	b.WriteString("  " + keyStyle.Render("[l]") + " or " + keyStyle.Render("[esc]") + "  leave config unchanged")

	return styleMigrationModal.Render(b.String())
}

func migrationPlanAction(file config.MigrationPlannedFile) string {
	if file.Create {
		return "create"
	}
	return "migrate"
}

func migrationLayerLabel(layer string) string {
	switch layer {
	case "user":
		return "Global"
	case "repo":
		return "Project"
	case "local":
		return "Local"
	default:
		return layer
	}
}

func (m Model) handleMigrationModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.MigrationRun):
		id, ok := m.beginOperation(operationMigration, nil)
		if !ok {
			return m, operationInProgressCmd()
		}
		m.pane = paneList
		return m, m.runMigrationCmd(id)
	case key.Matches(msg, m.keys.MigrationLeave):
		m.pane = paneList
		m.toast = "Config left unchanged"
		m.toastExp = time.Now().Add(2 * time.Second)
		return m, nil
	}
	return m, nil
}
