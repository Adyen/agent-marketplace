package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func newTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage telemetry consent, controls, and diagnostics",
		Annotations: map[string]string{
			"skip-target-validation": "true",
		},
	}

	cmd.AddCommand(
		newTelemetryEnableCmd(),
		newTelemetryDisableCmd(),
		newTelemetryStatusCmd(),
		newTelemetryPreviewCmd(),
		newTelemetryResetIDCmd(),
		newTelemetryPurgeCmd(),
		newTelemetryReceiverCmd(),
	)

	return cmd
}

func newTelemetryEnableCmd() *cobra.Command {
	var level string
	var endpoint string

	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable telemetry at the user level",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryEnable(cmd.OutOrStdout(), level, endpoint)
		},
	}

	cmd.Flags().StringVar(&level, "level", "", "Telemetry level: basic or detailed (required)")
	_ = cmd.MarkFlagRequired("level")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Optional OTLP/HTTP endpoint for user-managed telemetry")

	return cmd
}

func newTelemetryDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Set user telemetry level to off",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryDisable(cmd.OutOrStdout())
		},
	}
}

func newTelemetryStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show effective telemetry policy and local state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryStatus(cmd.OutOrStdout())
		},
	}
}

func newTelemetryPreviewCmd() *cobra.Command {
	var level string

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Render a representative local payload without sending it",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryPreview(cmd.Context(), cmd.OutOrStdout(), cmd.InOrStdin(), level)
		},
	}

	cmd.Flags().StringVar(&level, "level", "", "Telemetry level: basic, detailed, or diagnostic (required)")
	_ = cmd.MarkFlagRequired("level")

	return cmd
}

func newTelemetryResetIDCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-id",
		Short: "Reset the installation ID and purge pending telemetry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryResetID(cmd.OutOrStdout())
		},
	}
}

func newTelemetryPurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge",
		Short: "Remove queued telemetry records and retained diagnostic payloads",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryPurge(cmd.OutOrStdout())
		},
	}
}

func runTelemetryEnable(out io.Writer, level, endpoint string) error {
	normalized, err := tmcfg.NormalizeLevel(level)
	if err != nil {
		return err
	}
	if normalized == tmcfg.LevelOff {
		return fmt.Errorf("use 'telemetry disable' to turn telemetry off")
	}

	state, err := loadTelemetryState()
	if err != nil {
		return err
	}

	org, err := config.LoadOrganizationConfig()
	if err != nil {
		return fmt.Errorf("load organization config: %w", err)
	}
	orgLevel := tmcfg.LevelOff
	if org != nil && org.Telemetry != nil {
		var normErr error
		orgLevel, normErr = tmcfg.NormalizeLevel(org.Telemetry.Level)
		if normErr != nil {
			return fmt.Errorf("organization telemetry level %q: %w", org.Telemetry.Level, normErr)
		}
	}
	if tmcfg.LevelOrder(normalized) < tmcfg.LevelOrder(orgLevel) {
		return fmt.Errorf("organization policy requires telemetry level %q or higher", orgLevel)
	}
	if state.Managed && endpoint != "" && state.PrimaryEndpointSource == config.Organization.String() {
		return fmt.Errorf("endpoint is locked by organization policy")
	}

	configPath, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		return fmt.Errorf("user config not found at %q; run 'agent-marketplace init' first", configPath)
	}

	if err := config.SetUserTelemetryLevel(configPath, normalized); err != nil {
		return err
	}
	if endpoint != "" {
		if err := config.SetUserTelemetryEndpoint(configPath, endpoint); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(out, "Telemetry enabled at %q", normalized)
	if endpoint != "" {
		_, _ = fmt.Fprintf(out, " with endpoint %q", endpoint)
	}
	_, _ = fmt.Fprintln(out)
	slog.Info("telemetry enabled", "level", normalized, "endpoint", endpoint)
	return nil
}

func runTelemetryDisable(out io.Writer) error {
	if _, err := loadTelemetryState(); err != nil {
		return err
	}

	configPath, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}

	if err := config.DisableTelemetry(configPath); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "Telemetry disabled for user preference.")
	slog.Info("telemetry disabled for user preference")

	// Re-resolve after disabling user telemetry so the org-locked message is
	// based on the effective state, not the level before the user changed it.
	state, err := loadTelemetryState()
	if err != nil {
		return err
	}
	if state.Managed && tmcfg.LevelOrder(state.EffectiveLevel) > 0 {
		_, _ = fmt.Fprintf(out, "Organization policy keeps telemetry active at %q.\n", state.EffectiveLevel)
	}
	return nil
}

func runTelemetryStatus(out io.Writer) error {
	info, err := telemetry.GetStatusInfo(context.Background())
	if err != nil {
		return err
	}

	state := info.State

	primary := "none"
	if state.PrimaryEndpointHostname != "" {
		primary = fmt.Sprintf("%s (%s)", state.PrimaryEndpointHostname, strings.ToLower(state.PrimaryEndpointSource))
	}

	community := fmt.Sprintf("%v", state.CommunityReportingEnabled)
	if state.CommunityReportingSource != "" {
		community += fmt.Sprintf(" (%s)", strings.ToLower(state.CommunityReportingSource))
		if state.CommunityReportingFoldedIntoPrimary {
			community += ", folded into primary"
		}
	}

	installID := info.InstallationID
	if installID == "" {
		installID = "not set"
	}

	oldest := "none"
	if info.QueueOldestAgeOK {
		oldest = info.QueueOldestAge.Round(time.Second).String()
	}

	lastFailure := "none"
	if info.LastFailure.Error != "" {
		lastFailure = fmt.Sprintf("%s: %s", info.LastFailure.Time.Format(time.RFC3339), info.LastFailure.Error)
	}

	_, _ = fmt.Fprintf(out, "Telemetry status\n")
	_, _ = fmt.Fprintf(out, "  Effective level:       %s (%s)\n", state.EffectiveLevel, strings.ToLower(state.EffectiveLevelSource))
	_, _ = fmt.Fprintf(out, "  Primary endpoint:      %s\n", primary)
	_, _ = fmt.Fprintf(out, "  Primary headers:       %s\n", formatPrimaryHeaders(state.PrimaryHeaders))
	_, _ = fmt.Fprintf(out, "  Retention policy:      %s\n", emptyOr(state.RetentionPolicyURL, "none"))
	_, _ = fmt.Fprintf(out, "  Community reporting:   %s\n", community)
	_, _ = fmt.Fprintf(out, "  Managed installation:  %v\n", state.Managed)
	_, _ = fmt.Fprintf(out, "  Installation ID:       %s\n", installID)
	_, _ = fmt.Fprintf(out, "  Pending records:       %d (oldest %s, %s)\n", info.QueueCount, oldest, formatBytes(info.QueueSize))
	_, _ = fmt.Fprintf(out, "  Diagnostic payloads:   %d\n", info.DiagnosticCount)
	_, _ = fmt.Fprintf(out, "  Last delivery failure: %s\n", lastFailure)

	if state.Managed {
		locked := lockedFields(state)
		if len(locked) > 0 {
			_, _ = fmt.Fprintf(out, "  Locked by organization: %s\n", strings.Join(locked, ", "))
		}
	}

	return nil
}

func runTelemetryPreview(ctx context.Context, out io.Writer, in io.Reader, level string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	if level != tmcfg.LevelBasic && level != tmcfg.LevelDetailed && level != tmcfg.LevelDiagnostic {
		return fmt.Errorf("preview level must be one of %s, %s, %s", tmcfg.LevelBasic, tmcfg.LevelDetailed, tmcfg.LevelDiagnostic)
	}

	if level == tmcfg.LevelDiagnostic && !diagnosticAcceptSensitive {
		if isTerminalReader(in) {
			reader := bufio.NewReader(in)
			_, _ = fmt.Fprint(out, "Diagnostic payloads may contain sensitive data. Continue? [y/N]: ")
			reply, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read confirmation: %w", err)
			}
			reply = strings.TrimSpace(strings.ToLower(reply))
			if !strings.HasPrefix(reply, "y") {
				return fmt.Errorf("diagnostic preview cancelled")
			}
		} else {
			return fmt.Errorf("diagnostic preview requires --diagnostic-accept-sensitive-data in non-interactive mode")
		}
	}

	state, err := loadTelemetryState()
	if err != nil {
		return err
	}

	path, err := telemetry.Preview(ctx, level, state)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Preview payload written to %s\n", path)
	_, _ = fmt.Fprintf(out, "  level:         %s\n", level)
	_, _ = fmt.Fprintf(out, "  endpoint host: %s\n", emptyOr(state.PrimaryEndpointHostname, "none"))
	_, _ = fmt.Fprintf(out, "  retention:     %s\n", emptyOr(state.RetentionPolicyURL, "none"))
	_, _ = fmt.Fprintf(out, "  headers:       %s\n", formatPrimaryHeaders(state.PrimaryHeaders))
	if level == tmcfg.LevelDiagnostic {
		_, _ = fmt.Fprintln(out, "  warning:       Diagnostic payloads may contain sensitive data and require review before sending.")
	}
	slog.Info("telemetry preview rendered", "level", level, "path", path)
	return nil
}

func runTelemetryResetID(out io.Writer) error {
	state, err := loadTelemetryState()
	if err != nil {
		return err
	}

	if err := telemetry.ResetInstallationID(state.InstallationIDPath); err != nil {
		return err
	}

	newID, err := telemetry.LoadInstallationID(state.InstallationIDPath)
	if err != nil {
		return fmt.Errorf("generate new installation id: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Installation ID reset. New ID: %s\n", newID)
	slog.Info("installation id reset", "path", state.InstallationIDPath)
	return nil
}

func runTelemetryPurge(out io.Writer) error {
	state, err := loadTelemetryState()
	if err != nil {
		return err
	}

	queueCount, diagCount, err := telemetry.Purge(state.InstallationIDPath)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Purged %d queued record(s) and %d diagnostic payload(s).\n", queueCount, diagCount)
	slog.Info("telemetry purged", "queue_count", queueCount, "diagnostic_count", diagCount)
	return nil
}

func loadTelemetryState() (telemetry.TelemetryState, error) {
	return telemetry.LoadAndResolve(context.Background())
}

func formatPrimaryHeaders(headers map[string]tmcfg.HeaderSource) string {
	if len(headers) == 0 {
		return "none"
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		source, ref := headers[name].Source()
		if source == "" {
			parts = append(parts, fmt.Sprintf("%s (unset)", name))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s: %s)", name, source, ref))
		}
	}
	return strings.Join(parts, ", ")
}

func lockedFields(state telemetry.TelemetryState) []string {
	orgSource := config.Organization.String()
	var locked []string
	if state.EffectiveLevelSource == orgSource {
		locked = append(locked, "level")
	}
	if state.PrimaryEndpointSource == orgSource {
		locked = append(locked, "endpoint")
	}
	if state.CommunityReportingSource == orgSource {
		locked = append(locked, "community_reporting")
	}
	return locked
}

func emptyOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}
