package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

func newServeCmd() *cobra.Command {
	var cfgPath string
	var role string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Marketplace server worker",
		Long: `serve loads the Marketplace operator configuration, opens the configured
metadata and blob stores, and runs the indexing worker. Only the worker role is
supported in this release.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				return fmt.Errorf("--config is required")
			}
			if role != "worker" {
				return fmt.Errorf("role %q is not supported; use --role worker", role)
			}

			cfg, err := mpconfig.LoadConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load operator config: %w", err)
			}

			ctx := cmd.Context()
			meta, blobs, err := mpconfig.OpenStores(ctx, cfg)
			if err != nil {
				return fmt.Errorf("open stores: %w", err)
			}
			defer func() { _ = meta.Close() }()

			deps := worker.Deps{
				Config:        cfg,
				MetadataStore: meta,
				BlobStore:     blobs,
			}
			if err := worker.EnsureMarketplace(ctx, &deps); err != nil {
				return fmt.Errorf("ensure marketplace: %w", err)
			}

			return worker.Run(ctx, deps, cfg)
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "Path to the Marketplace operator config file")
	cmd.Flags().StringVar(&role, "role", "worker", "Server role (worker)")
	_ = cmd.MarkFlagRequired("config")

	cmd.AddCommand(newServeReindexCmd())
	return cmd
}

func newServeReindexCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "reindex [source-slug-or-id]",
		Short: "Enqueue and process one refresh job for a source",
		Long: `reindex loads the Marketplace operator configuration, opens the stores,
optionally enqueues and waits for the indexing work for a single source. If no
source is provided, all configured sources are reindexed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := mpconfig.LoadConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load operator config: %w", err)
			}

			ctx := cmd.Context()
			meta, blobs, err := mpconfig.OpenStores(ctx, cfg)
			if err != nil {
				return fmt.Errorf("open stores: %w", err)
			}
			defer func() { _ = meta.Close() }()

			deps := worker.Deps{
				Config:        cfg,
				MetadataStore: meta,
				BlobStore:     blobs,
			}

			ctx, cancel := context.WithTimeout(ctx, time.Hour)
			defer cancel()

			switch len(args) {
			case 0:
				for _, src := range cfg.Sources {
					slugOrID := src.Slug
					if slugOrID == "" {
						slugOrID = src.ID
					}
					if err := worker.Reindex(ctx, deps, cfg, slugOrID); err != nil {
						return fmt.Errorf("reindex source %q: %w", src.ID, err)
					}
				}
			case 1:
				if err := worker.Reindex(ctx, deps, cfg, args[0]); err != nil {
					return fmt.Errorf("reindex source %q: %w", args[0], err)
				}
			default:
				return errors.New("accepts at most one source slug or id")
			}

			return meta.ReadyCheck(ctx, blobs)
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "Path to the Marketplace operator config file")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}
