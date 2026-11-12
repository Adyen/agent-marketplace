package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudedelivery"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

func newServeClaudeDeliveryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-delivery",
		Short: "Inspect and reconcile Claude Enterprise delivery",
	}
	cmd.AddCommand(newServeClaudeDeliveryStatusCmd(), newServeClaudeDeliverySyncCmd())
	return cmd
}

func newServeClaudeDeliveryStatusCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Claude Enterprise delivery state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := mpconfig.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load Marketplace configuration: %w", err)
			}
			meta, _, err := mpconfig.OpenStores(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("open Marketplace stores: %w", err)
			}
			defer func() { _ = meta.Close() }()
			state, ok := meta.(store.ClaudeDeliveryStateStore)
			if !ok {
				return store.ErrTopology
			}
			rows, err := state.ListClaudeDeliveryPlugins(cmd.Context())
			if err != nil {
				return err
			}
			wrote := false
			for _, row := range rows {
				if row.Coordinate.Marketplace != store.MarketplaceID(cfg.Instance.ID) {
					continue
				}
				wrote = true
				status := string(row.State)
				message := claudedelivery.StatusMessage(row)
				if message == "awaiting its content scan" {
					status += " (awaiting its content scan)"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(),
					"Resource: %s/%s/%s\nPlugin: %s (%s)\nState: %s\nPublished version: %s\nServed version: %s\nScan status: %s\nReach: %s\nInstallation preference: %s\nLast failure code: %s\n",
					row.Coordinate.Registry, row.Coordinate.Kind, row.Coordinate.Name,
					row.PluginName, row.PluginID, status, row.PublishedVersionID,
					row.ServedVersionID, row.ContentScanStatus, row.Reach,
					row.InstallationPreference, row.LastErrorCode); err != nil {
					return err
				}
				if message != "" && message != "awaiting its content scan" {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), message); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			if !wrote {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No Claude Enterprise delivery state.")
			}
			return err
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the Marketplace operator config file")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newServeClaudeDeliverySyncCmd() *cobra.Command {
	var configPath string
	var once bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Run one Claude Enterprise delivery reconcile pass",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !once {
				return errors.New("serve claude-delivery sync requires --once")
			}
			cfg, err := mpconfig.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load Marketplace configuration: %w", err)
			}
			meta, blobs, err := mpconfig.OpenStores(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("open Marketplace stores: %w", err)
			}
			defer func() { _ = meta.Close() }()
			result, enabled, err := worker.RunClaudeDeliveryOnce(cmd.Context(), worker.Deps{
				Config: cfg, MetadataStore: meta, BlobStore: blobs,
			})
			if err != nil {
				return fmt.Errorf("claude enterprise delivery sync failed: %w", err)
			}
			if !enabled {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Claude Enterprise delivery is disabled; no request was sent.")
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"Claude Enterprise delivery pass completed: published=%d retired=%d skipped=%d conflicts=%d failed=%d.\n",
				result.Published, result.Retired, result.Skipped, result.Conflicts, result.Failed)
			return err
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the Marketplace operator config file")
	cmd.Flags().BoolVar(&once, "once", false, "Run exactly one reconcile pass and exit")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}
