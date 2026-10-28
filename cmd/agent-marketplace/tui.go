package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := config.FindRepoRoot()
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return runProductionTUI(cmd.Context(), repoRoot, homeDir)
		},
	}
}

func runProductionTUI(ctx context.Context, repoRoot, homeDir string) error {
	workspace, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
	if err != nil {
		return err
	}
	if err := pipeline.RecoverDependencyTransactions(ctx); err != nil {
		return err
	}
	options := []tui.Option{tui.WithWorkspace(workspace), tui.WithDependencyLifecycle(tui.DependencyLifecycle(session.RuntimeLifecycle()))}
	if checker := newUpgradeChecker(); checker != nil {
		options = append(options, tui.WithUpgradeCheck(checker))
	}
	return tui.Run(ctx, workspace.RepoRoot(), homeDir, options...)
}
