package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func newMigrateCmd() *cobra.Command {
	var harnesses []string
	var targetVersion string
	var scope string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate legacy configs to v4",
		Args:  cobra.NoArgs,
		Long: `Migrate converts writable user, repository, and repository-personal
configuration files to the canonical agent-marketplace/v4 source/resource
schema. The conversion is semantic and selection preserving for representable
standalone legacy files; it does not promise authored comments or formatting.

Migration is transactional: all selected inputs are validated before any file
is written. Include trees and Marketplace entries lacking complete verified
cache provenance are refused without modification; a proven Marketplace entry
migrates its exact pins into a v2 lock record derived offline from verified
local cache evidence. Existing v2 and v3 configs remain readable when
migration is declined or refused.

The --to v4 spelling remains accepted for compatibility (v4 is the default).
Use --to v2 only for the legacy structural-remediation flow. V4 migration
supports --scope and --dry-run; --dry-run writes neither config nor lock.
Organization configuration is never edited. Git/directory-only conversion
leaves locks unchanged; provenance-proven Marketplace conversion creates or
upgrades the owning layer lock to agent-marketplace-lock/v2 transactionally,
published together with the migrated config in one journaled transaction.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch targetVersion {
			case "", "v4":
				return runMigrateV4(cmd.Context(), cmd.OutOrStdout(), config.V4MigrationScope(scope), dryRun)
			case "v2":
				return runMigrate(cmd.OutOrStdout(), os.Stdin, harnesses)
			default:
				return fmt.Errorf("unsupported migration target %q; supported targets: v2, v4", targetVersion)
			}
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "harnesses", nil,
		"Target harnesses for the remediated user personal config")
	cmd.Flags().StringVar(&targetVersion, "to", "", "Target configuration version (v4 by default; v2 for legacy structural remediation)")
	cmd.Flags().StringVar(&scope, "scope", string(config.V4MigrationAll), "Writable layer to migrate (all, user, repo, local)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the v4 format migration without writing config files")

	return cmd
}

func runMigrate(out io.Writer, in io.Reader, explicitHarnesses []string) error {
	slog.Info("migrate start")

	repoRoot := config.FindRepoRoot()
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		return fmt.Errorf("resolve user personal config path: %w", err)
	}

	localDir := config.FindLocalConfigDir()
	paths := config.MigratePaths{
		User:  userPath,
		Repo:  config.ProjectConfigPath(repoRoot),
		Local: config.LocalConfigPath(localDir),
	}

	result, err := config.MigrateConfigFiles(paths, explicitHarnesses)
	if err == nil {
		if err := reportMigrateResult(out, result); err != nil {
			return err
		}
		slog.Info("migrate complete", "user", result.UserMigrated, "repo", result.RepoMigrated, "local", result.LocalMigrated)
		return nil
	}
	if !errors.Is(err, config.ErrHarnessesRequired) {
		return err
	}

	if stdin, ok := in.(*os.File); ok && isTerminal(stdin.Fd()) {
		prompted, perr := promptMigrateHarnesses(out, in)
		if perr != nil {
			return perr
		}
		result, err = config.MigrateConfigFiles(paths, prompted)
		if err != nil {
			return err
		}
		if err := reportMigrateResult(out, result); err != nil {
			return err
		}
		slog.Info("migrate complete", "user", result.UserMigrated, "repo", result.RepoMigrated, "local", result.LocalMigrated)
		return nil
	}

	return fmt.Errorf("non-interactive migration requires --harnesses; supported values: %s",
		strings.Join(config.SupportedHarnessTargets(), ", "))
}

func currentMigratePaths() (config.MigratePaths, error) {
	repoRoot := config.FindRepoRoot()
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		return config.MigratePaths{}, fmt.Errorf("resolve user personal config path: %w", err)
	}
	localDir := config.FindLocalConfigDir()
	return config.MigratePaths{
		User:  userPath,
		Repo:  config.ProjectConfigPath(repoRoot),
		Local: config.LocalConfigPath(localDir),
	}, nil
}

func runMigrateV4(ctx context.Context, out io.Writer, scope config.V4MigrationScope, dryRun bool) error {
	paths, err := currentMigratePaths()
	if err != nil {
		return err
	}
	result, err := config.MigrateConfigFilesToV4WithEvidence(ctx, paths, scope, dryRun, marketplace.DefaultCache())
	if err != nil {
		return err
	}
	verb := "Migrated"
	if dryRun {
		verb = "Would migrate"
	}
	return reportV4MigrationResult(out, result, verb)
}

// runV4MigrationPrompt asks for consent before changing legacy configuration.
// A non-interactive invocation never mutates files and logs the explicit
// migration command for the next interactive run.
func runV4MigrationPrompt(ctx context.Context, out io.Writer, in io.Reader, terminal bool, paths config.MigratePaths) error {
	if !terminal {
		slog.Warn("legacy configuration detected; migration consent unavailable", "next_command", "agent-marketplace migrate")
		return nil
	}
	if in == nil {
		return errors.New("cannot ask for v4 migration consent without input")
	}
	if _, err := fmt.Fprintln(out, "Legacy configuration detected in the writable config layers:"); err != nil {
		return err
	}
	for _, path := range []string{paths.User, paths.Repo, paths.Local} {
		if path != "" {
			if _, err := fmt.Fprintf(out, "  %s\n", path); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprint(out, "Migrate these configuration files to v4 now? [Y/n]: "); err != nil {
		return err
	}
	reply, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read v4 migration consent: %w", err)
	}
	reply = strings.TrimSpace(strings.ToLower(reply))
	if strings.HasPrefix(reply, "n") || (errors.Is(err, io.EOF) && reply == "") {
		_, writeErr := fmt.Fprintln(out, "Configuration migration skipped; continuing with v2/v3 compatibility reads.")
		return writeErr
	}
	if reply != "" && !strings.HasPrefix(reply, "y") {
		return fmt.Errorf("unknown response %q; use y or n", reply)
	}
	result, err := config.MigrateConfigFilesToV4WithEvidence(ctx, paths, config.V4MigrationAll, false, marketplace.DefaultCache())
	if err != nil {
		slog.Warn("automatic v4 migration refused; continuing with legacy configuration", "error", err)
		_, writeErr := fmt.Fprintf(out, "warning: v4 migration was not completed: %v\nContinuing with v2/v3 compatibility reads.\n", err)
		return writeErr
	}
	return reportV4MigrationResult(out, result, "Migrated")
}

func reportV4MigrationResult(out io.Writer, result *config.V4MigrationResult, verb string) error {
	if result.UserMigrated {
		if _, err := fmt.Fprintf(out, "%s user personal config: %s\n", verb, result.UserPath); err != nil {
			return err
		}
	}
	if result.RepoMigrated {
		if _, err := fmt.Fprintf(out, "%s repository config: %s\n", verb, result.RepoPath); err != nil {
			return err
		}
	}
	if result.LocalMigrated {
		if _, err := fmt.Fprintf(out, "%s repository personal config: %s\n", verb, result.LocalPath); err != nil {
			return err
		}
	}
	if !result.UserMigrated && !result.RepoMigrated && !result.LocalMigrated {
		_, err := fmt.Fprintln(out, "No legacy config files require v4 migration")
		return err
	}
	return nil
}

func promptMigrateHarnesses(out io.Writer, in io.Reader) ([]string, error) {
	reader := bufio.NewReader(in)
	detected, err := harness.DetectInstalledInCurrentHome()
	if err != nil {
		return nil, err
	}
	return confirmHarnesses(out, reader, detected)
}

func reportMigrateResult(out io.Writer, result *config.MigrateResult) error {
	if result.UserMigrated {
		if _, err := fmt.Fprintf(out, "Remediated user personal config: %s\n", result.UserPath); err != nil {
			return err
		}
	}
	if result.RepoMigrated {
		if _, err := fmt.Fprintf(out, "Remediated repository config: %s\n", result.RepoPath); err != nil {
			return err
		}
	}
	if result.LocalMigrated {
		if _, err := fmt.Fprintf(out, "Remediated repository personal config: %s\n", result.LocalPath); err != nil {
			return err
		}
	}
	if !result.UserMigrated && !result.RepoMigrated && !result.LocalMigrated {
		if _, err := fmt.Fprintln(out, "No structural remediation needed; config files are already valid"); err != nil {
			return err
		}
	}
	return nil
}
