package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

func newMigrateCmd() *cobra.Command {
	var harnesses []string
	var targetVersion string
	var scope string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Remediate structural issues in config files",
		Args:  cobra.NoArgs,
		Long: `Migrate performs structural remediation on user, repository, and
repository-personal config files. It preserves valid v2 and v3 apiVersion
values, accepts v3 as a compatibility alias, and only changes files when
structural remediation is needed. New or remediated files use canonical v2.

Existing user-personal harnesses are preserved. If a user config needs a
harness set, migration uses the --harnesses flag in non-interactive mode, or
prompts when running in a terminal. Project-level harnesses remain in the
project config.

Migration is transactional: all inputs are validated before any file is
written. Configs that contain legacy identity or team_profiles fields are
refused without modification.

Use --to v4 for explicit lossless source/resource migration. V4 migration
validates all writable layers by default, supports --scope and --dry-run, and
never edits organization configuration or lockfiles.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if targetVersion == "" {
				return runMigrate(cmd.OutOrStdout(), os.Stdin, harnesses)
			}
			if targetVersion != "v4" {
				return fmt.Errorf("unsupported migration target %q; supported target: v4", targetVersion)
			}
			return runMigrateV4(cmd.OutOrStdout(), config.V4MigrationScope(scope), dryRun)
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "harnesses", nil,
		"Target harnesses for the remediated user personal config")
	cmd.Flags().StringVar(&targetVersion, "to", "", "Explicit target configuration version (v4)")
	cmd.Flags().StringVar(&scope, "scope", string(config.V4MigrationAll), "Writable layer to migrate (all, user, repo, local)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview an explicit format migration without writing config files")

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

func runMigrateV4(out io.Writer, scope config.V4MigrationScope, dryRun bool) error {
	repoRoot := config.FindRepoRoot()
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		return fmt.Errorf("resolve user personal config path: %w", err)
	}
	localDir := config.FindLocalConfigDir()
	paths := config.MigratePaths{User: userPath, Repo: config.ProjectConfigPath(repoRoot), Local: config.LocalConfigPath(localDir)}
	result, err := config.MigrateConfigFilesToV4(paths, scope, dryRun)
	if err != nil {
		return err
	}
	verb := "Migrated"
	if dryRun {
		verb = "Would migrate"
	}
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
