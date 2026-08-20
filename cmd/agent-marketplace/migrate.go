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

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate v2 config files to the v3 schema",
		Args:  cobra.NoArgs,
		Long: `Migrate rewrites user, repository, and repository-personal config files from the
v2 schema to the strict agent-marketplace/v3 schema.

The target harness set is preserved from the user personal config when present.
If it is not present, migration uses the --harnesses flag in non-interactive
mode, or prompts when running in a terminal. Legacy repository-level harness
declarations are moved to user scope and removed from repository/local output.

Migration validates and freezes one plan before writing, locks and compare-and-swaps
its sources, stages each replacement, and rolls back ordinary publish failures.
If rollback itself fails, the error identifies the migration as partial rather than
claiming success. Configs that contain legacy identity or team_profiles fields are
refused without modification.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd.OutOrStdout(), os.Stdin, harnesses)
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "harnesses", nil,
		"Target harnesses for the migrated user personal config")

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

	plan, err := buildMigrationPlan(paths, explicitHarnesses)
	if err == nil {
		result, err := config.ExecuteMigrationPlan(plan)
		if err != nil {
			return err
		}
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
		plan, err = buildMigrationPlan(paths, prompted)
		if err != nil {
			return err
		}
		result, err := config.ExecuteMigrationPlan(plan)
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

func buildMigrationPlan(paths config.MigratePaths, explicitHarnesses []string) (*config.MigrationPlan, error) {
	organization, err := config.LoadOrganizationConfig()
	if err != nil {
		return nil, err
	}
	var organizationHarnesses []string
	if organization != nil {
		organizationHarnesses = organization.Harnesses
	}
	return config.PlanConfigMigration(paths, explicitHarnesses, organizationHarnesses)
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
		if _, err := fmt.Fprintf(out, "Migrated user personal config: %s\n", result.UserPath); err != nil {
			return err
		}
	}
	if result.RepoMigrated {
		if _, err := fmt.Fprintf(out, "Migrated repository config: %s\n", result.RepoPath); err != nil {
			return err
		}
	}
	if result.LocalMigrated {
		if _, err := fmt.Fprintf(out, "Migrated repository personal config: %s\n", result.LocalPath); err != nil {
			return err
		}
	}
	if !result.UserMigrated && !result.RepoMigrated && !result.LocalMigrated {
		if _, err := fmt.Fprintln(out, "No migration needed; config files are already at apiVersion agent-marketplace/v3"); err != nil {
			return err
		}
	}
	return nil
}
