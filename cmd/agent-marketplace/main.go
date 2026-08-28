package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/logging"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/internal/tui"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

const defaultVersion = "dev"

var version = defaultVersion
var diagnosticCapture bool
var diagnosticAcceptSensitive bool
var rootLogState struct {
	startedAt time.Time
	close     func() error
	closed    bool
}

func main() {
	cmd := newRootCmd()
	execCmd, execErr := cmd.ExecuteC()
	finishTelemetry(execCmd, execErr)
	if execErr != nil {
		finishRootLogging(execErr)
		// Cobra already prints the error (SilenceErrors is false) to the
		// command's stderr. Do NOT print it again here, or every error shows
		// up twice. Just exit non-zero.
		os.Exit(1)
	}
	finishRootLogging(nil)
}

func newRootCmd() *cobra.Command {
	var showVersion bool
	var noTUI bool
	var debug bool

	rootCmd := &cobra.Command{
		Use:           "agent-marketplace",
		Short:         "Sync shared AI agent skills from configured registries",
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			initRootLogging(cmd, debug)
			initTelemetry(cmd, start)
			if shouldSkipTargetValidation(cmd) {
				return nil
			}
			if err := validateStartupTargetRegistrations(); err != nil {
				slog.Error("validate harness target registrations", "error", err)
				return fmt.Errorf("validate harness target registrations: %w", err)
			}
			if err := harness.TargetInfoError(); err != nil {
				slog.Error("initialize harness targets", "error", err)
				return fmt.Errorf("initialize harness targets: %w", err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
				return err
			}

			// Launch TUI when bare invocation on a TTY (unless opted out)
			if !noTUI && os.Getenv("AGENT_MARKETPLACE_NO_TUI") == "" {
				if term.IsTerminal(os.Stdin.Fd()) {
					repoRoot := config.FindRepoRoot()
					homeDir, err := os.UserHomeDir()
					if err != nil {
						return err
					}
					return tui.Run(cmd.Context(), repoRoot, homeDir)
				}
			}

			return cmd.Help()
		},
	}

	rootCmd.Flags().BoolVar(&showVersion, "version", false, "Print the agent-marketplace version")
	rootCmd.Flags().BoolVar(&noTUI, "no-tui", false, "Skip the interactive TUI and print help instead")
	_ = rootCmd.Flags().MarkHidden("no-tui")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging and mirror logs to stderr")
	rootCmd.PersistentFlags().BoolVar(&diagnosticCapture, "diagnostic", false, "Capture a reviewed diagnostic telemetry payload for this invocation")
	rootCmd.PersistentFlags().BoolVar(&diagnosticAcceptSensitive, "diagnostic-accept-sensitive-data", false, "Acknowledge a diagnostic payload may contain sensitive data")

	rootCmd.AddCommand(
		newInitCmd(),
		newSyncCmd(),
		newListCmd(),
		newSearchCmd(),
		newInstallCmd(),
		newRemoveCmd(),
		newMigrateCmd(),
		newConvertCmd(),
		newDoctorCmd(),
		newGCCmd(),
		newUpdateCmd(),
		newStatusCmd(),
		newTUICmd(),
		newBackupCmd(),
		newDeinitCmd(),
		newServeCmd(),
		newTelemetryCmd(),
	)

	return rootCmd
}

func validateStartupTargetRegistrations() error {
	return hcc.ValidateTargetRegistrations()
}

func initRootLogging(cmd *cobra.Command, debug bool) {
	rootLogState.startedAt = time.Now()
	rootLogState.closed = false

	opts := logging.Options{
		Debug:   debug,
		Command: cmd.CommandPath(),
		Version: version,
	}
	logCfg, cfgErr := config.LoadUserLoggingConfig()
	applyLoggingConfig(&opts, logCfg)

	_, closeLog, initErr := logging.Init(opts)
	rootLogState.close = closeLog
	if cfgErr != nil {
		slog.Warn("user logging config ignored", "error", cfgErr)
	}
	if initErr != nil {
		slog.Warn("file logging unavailable; using stderr fallback", "error", initErr)
	}

	repoRoot := config.FindRepoRoot()
	globalPath, _ := config.GlobalConfigPath()
	slog.Info("command start",
		"repo_root", repoRoot,
		"org_config", config.OrganizationConfigPath(),
		"user_config", globalPath,
		"repo_config", config.ProjectConfigPath(repoRoot),
		"local_config", config.LocalConfigPath(repoRoot),
	)
}

func finishRootLogging(execErr error) {
	if rootLogState.close == nil || rootLogState.closed {
		return
	}
	duration := time.Since(rootLogState.startedAt)
	if execErr != nil {
		slog.Error("command end", "duration_ms", duration.Milliseconds(), "success", false, "error", execErr)
	} else {
		slog.Info("command end", "duration_ms", duration.Milliseconds(), "success", true)
	}
	if err := rootLogState.close(); err != nil && execErr == nil {
		fmt.Fprintf(os.Stderr, "warning: failed to close log file: %v\n", err)
	}
	rootLogState.closed = true
}

func initTelemetry(cmd *cobra.Command, start time.Time) {
	if cmd == nil {
		return
	}
	path := cmd.CommandPath()
	if shouldSkipTelemetryInit(path) {
		return
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	state, err := telemetry.LoadAndResolve(ctx)
	if err != nil {
		slog.Warn("telemetry config resolution failed; continuing without telemetry", "error", err)
		return
	}
	if state.EffectiveLevel != tmcfg.LevelBasic && state.EffectiveLevel != tmcfg.LevelDetailed {
		return
	}

	t, err := telemetry.New(ctx, state, slog.Default(), telemetry.WithDiagnostic(diagnosticCapture))
	if err != nil {
		slog.Warn("telemetry initialization failed; continuing without telemetry", "error", err)
		return
	}

	rootSpan := telemetry.Span{}
	if state.EffectiveLevel == tmcfg.LevelDetailed {
		ctx, rootSpan = t.StartSpan(ctx, "agent_marketplace.command", map[string]string{
			string(tmcfg.ResourceAttrCommandName): path,
			string(tmcfg.ResourceAttrHarnesses):   state.HarnessesString(),
			string(tmcfg.ResourceAttrManaged):     strconv.FormatBool(state.Managed),
		})
		if t.DiagnosticActive() && rootSpan != (telemetry.Span{}) {
			t.RecordDiagnostic("diagnostic.capture.started", "diagnostic capture enabled for this invocation")
			rootSpan.AddEvent("diagnostic.capture.started", map[string]string{
				"message": "diagnostic capture enabled for this invocation",
			})
		}
	}

	cmd.SetContext(telemetry.NewContext(ctx, t, path, start, state.HarnessesString(), state.Managed, rootSpan, t.DiagnosticActive()))
}

func finishTelemetry(cmd *cobra.Command, execErr error) {
	if cmd == nil {
		return
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	t := telemetry.TelemetryFromContext(ctx)
	if t == nil {
		return
	}

	path := telemetry.CommandPathFromContext(ctx)
	if path == "" {
		path = cmd.CommandPath()
	}
	start := telemetry.CommandStartFromContext(ctx)
	harnesses := telemetry.HarnessesFromContext(ctx)
	managed := telemetry.ManagedFromContext(ctx)

	outcome := "success"
	if execErr != nil {
		outcome = "error"
	}
	attrs := map[string]string{
		string(tmcfg.ResourceAttrCommandName): path,
		string(tmcfg.ResourceAttrHarnesses):   harnesses,
		string(tmcfg.ResourceAttrManaged):     strconv.FormatBool(managed),
		string(tmcfg.ResourceAttrOutcomeCode): outcome,
	}

	rootSpan := telemetry.RootSpanFromContext(ctx)
	if rootSpan != (telemetry.Span{}) {
		if t.DiagnosticActive() {
			for _, ev := range t.DiagnosticEvents() {
				rootSpan.AddEvent(ev.Name, map[string]string{"message": ev.Message})
			}
			rootSpan.AddEvent("diagnostic.command.end", map[string]string{
				"outcome": outcome,
				"message": "command finished; diagnostic payload ready for review",
			})
		}
		rootSpan.SetAttributes(attrs)
		rootSpan.End()
	}

	t.CommandCounter().Add(ctx, 1, attrs)
	t.CommandDuration().Record(ctx, time.Since(start).Milliseconds(), attrs)

	if t.DiagnosticActive() {
		if err := t.FinishDiagnostic(ctx, cmd.OutOrStdout(), cmd.InOrStdin(), isTerminalReader(cmd.InOrStdin()), diagnosticAcceptSensitive); err != nil {
			slog.Warn("diagnostic export failed or declined", "error", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := t.Shutdown(shutdownCtx); err != nil {
		slog.Warn("telemetry shutdown failed", "error", err)
	}
}

// isTerminalReader reports whether r is backed by a terminal file descriptor.
func isTerminalReader(r io.Reader) bool {
	type fdReader interface {
		Fd() uintptr
	}
	fr, ok := r.(fdReader)
	if !ok {
		return false
	}
	return term.IsTerminal(fr.Fd())
}

func shouldSkipTelemetryInit(path string) bool {
	return path == "agent-marketplace init" || strings.HasPrefix(path, "agent-marketplace telemetry")
}

func applyLoggingConfig(opts *logging.Options, cfg *config.LoggingConfig) {
	if cfg == nil {
		return
	}
	opts.Enabled = cfg.Enabled
	if cfg.Level != nil {
		opts.Level = *cfg.Level
	}
	if cfg.File != nil {
		opts.FilePath = *cfg.File
	}
	opts.MaxSizeMB = cfg.MaxSizeMB
	opts.MaxBackups = cfg.MaxBackups
	opts.MaxAgeDays = cfg.MaxAgeDays
	opts.Compress = cfg.Compress
}

func newRemoveCmd() *cobra.Command {
	var scopeStr string
	var kindFilter string
	var fromURL string
	var fromRef string
	var fromPath string

	cmd := &cobra.Command{
		Use:   "remove <resource-name>",
		Short: "Remove a resource from config and sync it away",
		Args: func(cmd *cobra.Command, args []string) error {
			fromURL = strings.TrimSpace(fromURL)
			fromRef = strings.TrimSpace(fromRef)
			fromPath = strings.TrimSpace(fromPath)

			switch {
			case fromRef != "" && fromURL == "":
				return fmt.Errorf("--ref requires --from")
			case fromPath != "" && fromURL == "":
				return fmt.Errorf("--path requires --from")
			case fromURL != "" && len(args) == 0:
				// --from without name = remove entire registry entry
				return nil
			case fromURL != "" && len(args) == 1:
				// --from with name = remove one resource from that entry
				return nil
			case len(args) != 1:
				return fmt.Errorf("accepts exactly one resource name, or --from to remove an ad-hoc registry entry")
			default:
				return nil
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scopeSpecified := cmd.Flags().Changed("scope")
			scope, err := pipeline.ParseScope(scopeStr)
			if err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runRemove(cmd.Context(), cmd.OutOrStdout(), name, scope, scopeSpecified, kindFilter, fromURL, fromRef, fromPath)
		},
	}

	cmd.Flags().StringVar(&scopeStr, "scope", "", "Config layer to write to: repo (default), user, or local")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "Resource kind for name disambiguation: skill, command, subagent, hook, mcp, or collection")
	cmd.Flags().StringVar(&fromURL, "from", "", "Remove from an ad-hoc registry (git URL or GitLab/GitHub web URL)")
	cmd.Flags().StringVar(&fromRef, "ref", "", "Git ref for --from registry; requires --from")
	cmd.Flags().StringVar(&fromPath, "path", "", "Subdirectory within the --from registry; requires --from")
	return cmd
}

func runRemove(ctx context.Context, out io.Writer, skillName string, scope pipeline.Scope, scopeSpecified bool, kindFilter, fromURL, fromRef, fromPath string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}

	repoRoot := config.FindRepoRoot()
	result, err := pipeline.Remove(ctx, pipeline.RemoveOptions{
		RepoRoot:       repoRoot,
		ResourceName:   skillName,
		Kind:           kind,
		HasKind:        hasKind,
		Scope:          scope,
		ScopeSpecified: scopeSpecified,
		FromURL:        fromURL,
		FromRef:        fromRef,
		FromPath:       fromPath,
	})
	if err != nil {
		return err
	}

	if result.IsCollection {
		_, err = fmt.Fprintf(out, "Removed collection %s\n", result.RemovedName)
		return err
	}
	if result.CollectionVia != "" {
		if _, err = fmt.Fprintf(out, "Excluded %s (provided by %s)\n", result.RemovedName, result.CollectionVia); err != nil {
			return err
		}
	} else if result.ExcludedRegistry {
		_, err = fmt.Fprintf(out, "Excluded all resources from %s (%s scope)\n", result.ExcludedURL, result.ExcludedScope)
		return err
	} else if result.RemovedRegistry {
		_, err = fmt.Fprintf(out, "Removed registry entry %s\n", fromURL)
		return err
	} else {
		if _, err = fmt.Fprintf(out, "Removed %s\n", result.RemovedName); err != nil {
			return err
		}
	}
	if result.InferredUserScope {
		globalPath, pathErr := config.GlobalConfigPath()
		if pathErr != nil {
			globalPath = "~/.agents/config.yaml"
		}
		_, err = fmt.Fprintf(out, "  (excluded in %s — applies across all repos)\n", globalPath)
	}
	return err
}

func shouldSkipTargetValidation(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil && c.Annotations["skip-target-validation"] == "true" {
			return true
		}
	}
	return false
}
