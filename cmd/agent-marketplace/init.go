package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

// isTerminal is swapped in tests to simulate a terminal stdin.
var isTerminal = term.IsTerminal

func newInitCmd() *cobra.Command {
	var harnesses []string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a minimal v3 User Personal config with an explicit target harness set",
		Long: "init creates the ~/.agents/config.yaml file using the v3 schema and an explicit " +
			"harnesses list. It does not install resources, add registries, or configure a marketplace.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.OutOrStdout(), os.Stdin, harnesses)
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "harnesses", nil,
		"Comma-separated target harnesses (non-interactive); required when stdin is not a terminal")

	return cmd
}

func runInit(out io.Writer, in *os.File, harnesses []string) error {
	slog.Info("init start")

	if len(harnesses) > 0 {
		return initWithHarnesses(out, harnesses)
	}

	if !isTerminal(in.Fd()) {
		err := fmt.Errorf("non-interactive init requires --harnesses; supported values: %s",
			strings.Join(config.SupportedHarnessTargets(), ", "))
		slog.Error("init failed", "err", err)
		return err
	}

	detected, err := config.DetectInstalledHarnesses()
	if err != nil {
		slog.Error("init failed to detect installed harnesses", "err", err)
		return err
	}

	reader := bufio.NewReader(in)

	harnesses, err = confirmHarnesses(out, reader, detected)
	if err != nil {
		slog.Error("init failed to confirm harnesses", "err", err)
		return err
	}

	if err := config.ValidateHarnessIDs(harnesses); err != nil {
		slog.Error("init failed to validate harnesses", "err", err)
		return err
	}

	path, created, err := initializeAndReport(out, harnesses)
	if err != nil {
		return err
	}
	if created {
		if err := promptTelemetryConsent(out, reader, path); err != nil {
			slog.Error("init failed to record telemetry consent", "err", err)
			return err
		}
	}
	return nil
}

func initWithHarnesses(out io.Writer, harnesses []string) error {
	if err := config.ValidateHarnessIDs(harnesses); err != nil {
		slog.Error("init failed to validate harnesses", "err", err)
		return err
	}
	_, _, err := initializeAndReport(out, harnesses)
	return err
}

func initializeAndReport(out io.Writer, harnesses []string) (string, bool, error) {
	path, err := config.InitializeUserPersonal(harnesses)
	if errors.Is(err, config.ErrAlreadyInitialized) {
		if _, err := fmt.Fprintln(out, "User personal config already initialized."); err != nil {
			return "", false, err
		}
		slog.Info("init skipped: user personal config already exists")
		return path, false, nil
	}
	if err != nil {
		slog.Error("init failed to create user personal config", "err", err)
		return "", false, err
	}
	if _, err := fmt.Fprintf(out, "Initialized user personal config: %s\n", path); err != nil {
		return "", false, err
	}
	slog.Info("init complete", "path", path, "harnesses", harnesses)
	return path, true, nil
}

func promptTelemetryConsent(out io.Writer, reader *bufio.Reader, configPath string) error {
	org, err := config.LoadOrganizationConfig()
	if err != nil {
		return fmt.Errorf("load organization config: %w", err)
	}
	// Only skip the consent prompt when the organization has explicitly set a
	// telemetry level (on or off). A telemetry block that only declares an
	// endpoint or other policy fields is not an activation decision; the user
	// may still choose a level.
	if org != nil && org.Telemetry != nil && org.Telemetry.Level != "" {
		_, _ = fmt.Fprintln(out, "Telemetry is managed by your organization; no user preference is needed.")
		return nil
	}

	var user *config.Config
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		loaded, loadErr := config.LoadConfigFromPath(configPath)
		if loadErr != nil {
			slog.Warn("failed to load user config for telemetry consent prompt", "path", configPath, "error", loadErr)
		} else {
			user = loaded
		}
	}

	state, err := telemetry.ResolveTelemetryConfig(org, user, telemetry.DefaultProjectEndpoint, telemetry.DefaultProjectRetentionURL)
	projectDestination := "not configured"
	retentionPolicyURL := "not configured"
	if err != nil {
		slog.Warn("failed to resolve telemetry state for consent prompt", "error", err)
	} else {
		if state.PrimaryEndpointHostname != "" {
			projectDestination = state.PrimaryEndpointHostname
		}
		if state.RetentionPolicyURL != "" {
			retentionPolicyURL = state.RetentionPolicyURL
		}
	}

	_, _ = fmt.Fprintln(out, "\nAgent Marketplace can send basic telemetry: CLI version, OS, architecture,")
	_, _ = fmt.Fprintln(out, "command name (not arguments), harness types, outcome, and coarse duration.")
	_, _ = fmt.Fprintln(out, "It does not include resource names, URLs, paths, errors, or prompts.")
	_, _ = fmt.Fprintln(out, "The data is retained according to the published retention policy.")
	_, _ = fmt.Fprintf(out, "Project destination: %s\n", projectDestination)
	_, _ = fmt.Fprintf(out, "Retention policy: %s\n", retentionPolicyURL)
	_, _ = fmt.Fprint(out, "Enable basic telemetry? [Y/n]: ")

	reply, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read telemetry consent: %w", err)
	}
	reply = strings.TrimSpace(strings.ToLower(reply))
	if errors.Is(err, io.EOF) && reply == "" {
		slog.Info("telemetry consent: declined", "path", configPath, "reason", "eof")
		return nil
	}

	if reply == "" || strings.HasPrefix(reply, "y") {
		if err := config.SetUserTelemetryLevel(configPath, tmcfg.LevelBasic); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "Basic telemetry enabled.")
		slog.Info("telemetry consent: basic enabled", "path", configPath)
	} else {
		slog.Info("telemetry consent: declined", "path", configPath)
	}
	return nil
}

func confirmHarnesses(out io.Writer, reader *bufio.Reader, detected []string) ([]string, error) {
	if len(detected) > 0 {
		if _, err := fmt.Fprintf(out, "Detected harnesses: %s\n", strings.Join(detected, ", ")); err != nil {
			return nil, err
		}
		if _, err := fmt.Fprint(out, "Use these? [Y/n/e to edit]: "); err != nil {
			return nil, err
		}

		reply, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read confirmation: %w", err)
		}
		reply = strings.TrimSpace(strings.ToLower(reply))

		switch {
		case reply == "" || strings.HasPrefix(reply, "y"):
			return detected, nil
		case strings.HasPrefix(reply, "e"):
			return promptHarnessList(out, reader, "Edit harnesses")
		case strings.HasPrefix(reply, "n"):
			return promptHarnessList(out, reader, "Choose harnesses")
		default:
			return nil, fmt.Errorf("unknown response %q; use y, n, or e", reply)
		}
	}

	if _, err := fmt.Fprintln(out, "No harnesses detected."); err != nil {
		return nil, err
	}
	return promptHarnessList(out, reader, "Choose harnesses")
}

func promptHarnessList(out io.Writer, reader *bufio.Reader, label string) ([]string, error) {
	if _, err := fmt.Fprintf(out, "%s: %s\n", label, strings.Join(config.SupportedHarnessTargets(), ", ")); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprint(out, "Enter comma-separated harnesses: "); err != nil {
		return nil, err
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read harness list: %w", err)
	}

	harnesses := splitHarnesses(line)
	if len(harnesses) == 0 {
		return nil, fmt.Errorf("at least one harness is required")
	}
	return harnesses, nil
}

func splitHarnesses(line string) []string {
	parts := strings.Split(line, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
