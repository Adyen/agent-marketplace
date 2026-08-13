package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
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

			return tui.Run(repoRoot, homeDir)
		},
	}
}
